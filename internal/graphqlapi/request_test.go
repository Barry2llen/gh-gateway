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
