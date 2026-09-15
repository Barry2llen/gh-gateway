package graphqlapi

import (
	"encoding/json"
	"testing"
)

const repositoryInfoQuery = `query RepositoryInfo($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    nameWithOwner,parent{id,name,owner{id,login}}
  }
}`

const pullRequestForBranchQuery = `query PullRequestForBranch($owner: String!, $repo: String!, $headRefName: String!, $states: [PullRequestState!]) {
  repository(owner: $owner, name: $repo) {
    pullRequests(headRefName: $headRefName, states: $states, first: 30, orderBy: { field: CREATED_AT, direction: DESC }) {
      nodes {number,url,state,id,baseRefName,headRefName,isCrossRepository,headRepositoryOwner{id,login,...on User{name}}}
    }
    defaultBranchRef { name }
  }
}`

const expandedPullRequestForBranchQuery = `query PullRequestForBranch($owner: String!, $repo: String!, $headRefName: String!, $states: [PullRequestState!]) {
  repository(name: $repo, owner: $owner) {
    defaultBranchRef { name }
    pullRequests(orderBy: {direction: DESC, field: CREATED_AT}, first: 30, states: $states, headRefName: $headRefName) {
      nodes {
        reviewDecision mergeStateStatus mergeable
        headRepositoryOwner { login id ... on User { name } }
        headRepository { nameWithOwner id name }
        headRefOid closedAt mergedAt state url number
        isCrossRepository headRefName baseRefName id
      }
    }
  }
}`

const pullRequestByNumberQuery = `query PullRequestByNumber($owner: String!, $repo: String!, $pr_number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $pr_number) {
      number url state mergedAt closedAt headRefName headRefOid
      headRepository { id name nameWithOwner }
      headRepositoryOwner { id login ... on User { name } }
      mergeable mergeStateStatus reviewDecision id
    }
  }
}`

const pullRequestStatusChecksQuery = `query PullRequestStatusChecks($id:ID!,$endCursor:String){node(id:$id){...on PullRequest{statusCheckRollup:commits(last:1){nodes{commit{statusCheckRollup{contexts(first:100,after:$endCursor){nodes{__typename ...on StatusContext{context state targetUrl createdAt description isRequired(pullRequestId:$id)} ...on CheckRun{name checkSuite{workflowRun{workflow{name}}} status conclusion startedAt completedAt detailsUrl isRequired(pullRequestId:$id)}} pageInfo{hasNextPage endCursor}}}}}}}}}`

func TestParsePullRequestStatusChecks(t *testing.T) {
	t.Parallel()
	id := encodePullRequestID("foo", "bar", 12)
	got, err := parsePullRequestStatusChecks(graphQLRequest{Query: pullRequestStatusChecksQuery, Variables: map[string]json.RawMessage{"id": json.RawMessage(`"` + id + `"`), "endCursor": json.RawMessage(`null`)}})
	if err != nil || got.ID != id || got.Cursor != "" {
		t.Fatalf("request/error = %#v/%v", got, err)
	}
}

func TestParseFeatureDetectionOperations(t *testing.T) {
	t.Parallel()
	for _, query := range []string{`query PullRequest_fields{PullRequest:__type(name:"PullRequest"){fields(includeDeprecated:true){name}} StatusCheckRollupContextConnection:__type(name:"StatusCheckRollupContextConnection"){fields(includeDeprecated:true){name}}}`, `query PullRequest_fields2{WorkflowRun:__type(name:"WorkflowRun"){fields(includeDeprecated:true){name}}}`} {
		operation, err := parseOperation(graphQLRequest{Query: query})
		if err != nil || operation.Name == "" {
			t.Fatalf("operation/error=%#v/%v", operation, err)
		}
	}
}

func TestParseRepositoryInfoRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		operationName string
	}{
		{name: "operation name omitted"},
		{name: "operation name supplied", operationName: "RepositoryInfo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			variables := map[string]json.RawMessage{
				"owner": json.RawMessage(`"foo"`),
				"name":  json.RawMessage(`"bar"`),
			}
			got, err := parseRepositoryInfo(graphQLRequest{
				Query:         repositoryInfoQuery,
				OperationName: tt.operationName,
				Variables:     variables,
			})
			if err != nil {
				t.Fatalf("parseRepositoryInfo() error = %v", err)
			}
			if got.Owner != "foo" || got.Name != "bar" {
				t.Fatalf("parseRepositoryInfo() = %#v, want owner foo and name bar", got)
			}
		})
	}
}

func TestParseRepositoryInfoRejectsInvalidVariables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		variables map[string]json.RawMessage
	}{
		{name: "missing owner", variables: map[string]json.RawMessage{"name": json.RawMessage(`"bar"`)}},
		{name: "empty owner", variables: map[string]json.RawMessage{"owner": json.RawMessage(`""`), "name": json.RawMessage(`"bar"`)}},
		{name: "non-string name", variables: map[string]json.RawMessage{"owner": json.RawMessage(`"foo"`), "name": json.RawMessage(`12`)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseRepositoryInfo(graphQLRequest{Query: repositoryInfoQuery, Variables: tt.variables})
			if err == nil {
				t.Fatal("parseRepositoryInfo() error = nil, want error")
			}
		})
	}
}

func TestParseRepositoryInfoRejectsUnsupportedOperation(t *testing.T) {
	t.Parallel()

	_, err := parseRepositoryInfo(graphQLRequest{
		Query: `query ViewerInfo { viewer { login } }`,
	})
	if err == nil {
		t.Fatal("parseRepositoryInfo() error = nil, want unsupported operation error")
	}
}

func TestParsePullRequestForBranch(t *testing.T) {
	t.Parallel()

	got, err := parsePullRequestForBranch(graphQLRequest{
		Query: pullRequestForBranchQuery,
		Variables: map[string]json.RawMessage{
			"owner":       json.RawMessage(`"foo"`),
			"repo":        json.RawMessage(`"bar"`),
			"headRefName": json.RawMessage(`"feature"`),
			"states":      json.RawMessage(`null`),
		},
	})
	if err != nil {
		t.Fatalf("parsePullRequestForBranch() error = %v", err)
	}
	if got.Owner != "foo" || got.Repo != "bar" || got.HeadRefName != "feature" {
		t.Fatalf("parsePullRequestForBranch() = %#v", got)
	}
}

func TestParsePullRequestForBranchRejectsNonNullStates(t *testing.T) {
	t.Parallel()

	_, err := parsePullRequestForBranch(graphQLRequest{
		Query: pullRequestForBranchQuery,
		Variables: map[string]json.RawMessage{
			"owner":       json.RawMessage(`"foo"`),
			"repo":        json.RawMessage(`"bar"`),
			"headRefName": json.RawMessage(`"feature"`),
			"states":      json.RawMessage(`["OPEN"]`),
		},
	})
	if err == nil {
		t.Fatal("parsePullRequestForBranch() error = nil, want error")
	}
}

func TestParseExpandedPullRequestForBranchIgnoresFieldOrdering(t *testing.T) {
	t.Parallel()

	got, err := parsePullRequestForBranch(graphQLRequest{
		Query: expandedPullRequestForBranchQuery,
		Variables: map[string]json.RawMessage{
			"owner": json.RawMessage(`"foo"`), "repo": json.RawMessage(`"bar"`),
			"headRefName": json.RawMessage(`"feature"`), "states": json.RawMessage(`null`),
		},
	})
	if err != nil {
		t.Fatalf("parsePullRequestForBranch() error = %v", err)
	}
	for _, field := range []string{"mergedAt", "closedAt", "headRefOid", "headRepository", "mergeable", "mergeStateStatus", "reviewDecision"} {
		if !got.Fields.Has(field) {
			t.Fatalf("fields = %#v, missing %q", got.Fields, field)
		}
	}
}

func TestParsePullRequestByNumberSupportsMetadataAndChecksSelections(t *testing.T) {
	t.Parallel()

	for _, query := range []string{
		pullRequestByNumberQuery,
		`query PullRequestByNumber($owner:String!,$repo:String!,$pr_number:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$pr_number){headRefName number id}}}`,
	} {
		got, err := parsePullRequestByNumber(graphQLRequest{
			Query: query,
			Variables: map[string]json.RawMessage{
				"owner": json.RawMessage(`"foo"`), "repo": json.RawMessage(`"bar"`), "pr_number": json.RawMessage(`12`),
			},
		})
		if err != nil {
			t.Fatalf("parsePullRequestByNumber() error = %v", err)
		}
		if got.Owner != "foo" || got.Repo != "bar" || got.Number != 12 || !got.Fields.Has("id") {
			t.Fatalf("parsed request = %#v", got)
		}
	}
}

func TestPullRequestSelectionRejectsUnknownField(t *testing.T) {
	t.Parallel()

	query := `query PullRequestByNumber($owner:String!,$repo:String!,$pr_number:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$pr_number){id title}}}`
	_, err := parsePullRequestByNumber(graphQLRequest{Query: query, Variables: map[string]json.RawMessage{
		"owner": json.RawMessage(`"foo"`), "repo": json.RawMessage(`"bar"`), "pr_number": json.RawMessage(`1`),
	}})
	if err == nil {
		t.Fatal("parsePullRequestByNumber() error = nil")
	}
}
