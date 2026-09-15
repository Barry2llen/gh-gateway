package graphqlapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

const repositorySchema = `
type Query {
  repository(owner: String!, name: String!): Repository
  node(id: ID!): Node
}

interface Node { id: ID! }

type Repository {
  id: ID!
  name: String!
  nameWithOwner: String!
  owner: RepositoryOwner!
  parent: Repository
  pullRequests(headRefName: String!, states: [PullRequestState!], first: Int!, orderBy: PullRequestOrder!): PullRequestConnection!
  pullRequest(number: Int!): PullRequest
  defaultBranchRef: Ref
}

interface RepositoryOwner {
  id: ID!
  login: String!
}

type User implements RepositoryOwner {
  id: ID!
  login: String!
  name: String
}

type Organization implements RepositoryOwner {
  id: ID!
  login: String!
}

type Ref {
  name: String!
}

type PullRequestConnection {
  nodes: [PullRequest!]!
}

type PullRequest implements Node {
  number: Int!
  url: String!
  state: PullRequestState!
  id: ID!
  baseRefName: String!
  headRefName: String!
  headRefOid: String!
  mergedAt: DateTime
  closedAt: DateTime
  headRepository: Repository
  isCrossRepository: Boolean!
  headRepositoryOwner: RepositoryOwner
  mergeable: MergeableState!
  mergeStateStatus: MergeStateStatus!
  reviewDecision: PullRequestReviewDecision
  commits(last: Int!): PullRequestCommitConnection!
}

type PullRequestCommitConnection { nodes: [PullRequestCommit!]! }
type PullRequestCommit { commit: Commit! }
type Commit { statusCheckRollup: StatusCheckRollup }
type StatusCheckRollup { contexts(first: Int!, after: String): StatusCheckRollupContextConnection! }
type StatusCheckRollupContextConnection { nodes: [StatusCheckRollupContext!]!, pageInfo: PageInfo! }
type PageInfo { hasNextPage: Boolean!, endCursor: String }
union StatusCheckRollupContext = StatusContext | CheckRun
type StatusContext {
  context: String!, state: StatusState!, targetUrl: String, createdAt: DateTime!, description: String,
  isRequired(pullRequestId: ID!): Boolean!
}
enum StatusState { EXPECTED ERROR FAILURE PENDING SUCCESS }
type CheckRun {
  name: String!, checkSuite: CheckSuite, status: String, conclusion: String, startedAt: DateTime,
  completedAt: DateTime, detailsUrl: String, isRequired(pullRequestId: ID!): Boolean!
}
type CheckSuite { workflowRun: WorkflowRun }
type WorkflowRun { workflow: Workflow }
type Workflow { name: String! }

scalar DateTime

enum MergeableState { MERGEABLE CONFLICTING UNKNOWN }
enum MergeStateStatus { BEHIND BLOCKED CLEAN DIRTY DRAFT HAS_HOOKS UNKNOWN UNSTABLE }
enum PullRequestReviewDecision { APPROVED CHANGES_REQUESTED REVIEW_REQUIRED }

enum PullRequestState {
  OPEN
  CLOSED
  MERGED
}

enum PullRequestOrderField {
  CREATED_AT
}

enum OrderDirection {
  DESC
}

input PullRequestOrder {
  field: PullRequestOrderField!
  direction: OrderDirection!
}
`

var schema = gqlparser.MustLoadSchema(&ast.Source{Name: "repository.graphql", Input: repositorySchema})

type graphQLRequest struct {
	Query         string                     `json:"query"`
	OperationName string                     `json:"operationName,omitempty"`
	Variables     map[string]json.RawMessage `json:"variables"`
}

type repositoryInfo struct {
	Owner string
	Name  string
}

type pullRequestForBranch struct {
	Owner       string
	Repo        string
	HeadRefName string
	Fields      pullRequestSelection
}

type pullRequestByNumber struct {
	Owner  string
	Repo   string
	Number int64
	Fields pullRequestSelection
}

type pullRequestStatusChecks struct {
	ID     string
	Cursor string
}

type fieldSet map[string]struct{}

func (s fieldSet) Has(name string) bool {
	_, ok := s[name]
	return ok
}

type pullRequestSelection struct {
	Fields              fieldSet
	HeadRepository      fieldSet
	HeadRepositoryOwner fieldSet
}

func (s pullRequestSelection) Has(name string) bool {
	return s.Fields.Has(name)
}

func parseRepositoryInfo(request graphQLRequest) (repositoryInfo, error) {
	operation, err := parseOperation(request)
	if err != nil {
		return repositoryInfo{}, err
	}
	return parseRepositoryInfoOperation(request, operation)
}

func parseRepositoryInfoOperation(request graphQLRequest, operation *ast.OperationDefinition) (repositoryInfo, error) {
	if operation.Name != "RepositoryInfo" {
		return repositoryInfo{}, errors.New("unsupported GraphQL operation; only RepositoryInfo is available")
	}
	if err := validateRepositoryInfoShape(operation); err != nil {
		return repositoryInfo{}, err
	}

	owner, err := requiredStringVariable(request.Variables, "owner")
	if err != nil {
		return repositoryInfo{}, err
	}
	name, err := requiredStringVariable(request.Variables, "name")
	if err != nil {
		return repositoryInfo{}, err
	}
	return repositoryInfo{Owner: owner, Name: name}, nil
}

func parsePullRequestForBranch(request graphQLRequest) (pullRequestForBranch, error) {
	operation, err := parseOperation(request)
	if err != nil {
		return pullRequestForBranch{}, err
	}
	return parsePullRequestForBranchOperation(request, operation)
}

func parsePullRequestForBranchOperation(request graphQLRequest, operation *ast.OperationDefinition) (pullRequestForBranch, error) {
	if operation.Name != "PullRequestForBranch" {
		return pullRequestForBranch{}, errors.New("unsupported GraphQL operation; only PullRequestForBranch is available")
	}
	fields, err := validatePullRequestForBranchShape(operation)
	if err != nil {
		return pullRequestForBranch{}, err
	}
	owner, err := requiredStringVariable(request.Variables, "owner")
	if err != nil {
		return pullRequestForBranch{}, err
	}
	repo, err := requiredStringVariable(request.Variables, "repo")
	if err != nil {
		return pullRequestForBranch{}, err
	}
	headRefName, err := requiredStringVariable(request.Variables, "headRefName")
	if err != nil {
		return pullRequestForBranch{}, err
	}
	states, ok := request.Variables["states"]
	if !ok || !bytes.Equal(bytes.TrimSpace(states), []byte("null")) {
		return pullRequestForBranch{}, errors.New("variable \"states\" must be null for PullRequestForBranch")
	}
	return pullRequestForBranch{Owner: owner, Repo: repo, HeadRefName: headRefName, Fields: fields}, nil
}

func parsePullRequestByNumber(request graphQLRequest) (pullRequestByNumber, error) {
	operation, err := parseOperation(request)
	if err != nil {
		return pullRequestByNumber{}, err
	}
	return parsePullRequestByNumberOperation(request, operation)
}

func parsePullRequestByNumberOperation(request graphQLRequest, operation *ast.OperationDefinition) (pullRequestByNumber, error) {
	if operation.Name != "PullRequestByNumber" {
		return pullRequestByNumber{}, errors.New("unsupported GraphQL operation; only PullRequestByNumber is available")
	}
	fields, err := validatePullRequestByNumberShape(operation)
	if err != nil {
		return pullRequestByNumber{}, err
	}
	owner, err := requiredStringVariable(request.Variables, "owner")
	if err != nil {
		return pullRequestByNumber{}, err
	}
	repo, err := requiredStringVariable(request.Variables, "repo")
	if err != nil {
		return pullRequestByNumber{}, err
	}
	number, err := requiredIntVariable(request.Variables, "pr_number")
	if err != nil {
		return pullRequestByNumber{}, err
	}
	return pullRequestByNumber{Owner: owner, Repo: repo, Number: number, Fields: fields}, nil
}

func parsePullRequestStatusChecks(request graphQLRequest) (pullRequestStatusChecks, error) {
	operation, err := parseOperation(request)
	if err != nil {
		return pullRequestStatusChecks{}, err
	}
	return parsePullRequestStatusChecksOperation(request, operation)
}
func parsePullRequestStatusChecksOperation(request graphQLRequest, operation *ast.OperationDefinition) (pullRequestStatusChecks, error) {
	if operation.Name != "PullRequestStatusChecks" {
		return pullRequestStatusChecks{}, errors.New("unsupported GraphQL operation; only PullRequestStatusChecks is available")
	}
	root, err := exactFields(operation.SelectionSet, "node")
	if err != nil {
		return pullRequestStatusChecks{}, err
	}
	if len(root["node"].Arguments) != 1 || !variableArgument(root["node"], "id", "id") {
		return pullRequestStatusChecks{}, errors.New("node.id must use id variable")
	}
	id, err := requiredStringVariable(request.Variables, "id")
	if err != nil {
		return pullRequestStatusChecks{}, err
	}
	cursor := ""
	if raw, ok := request.Variables["endCursor"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if err := json.Unmarshal(raw, &cursor); err != nil || cursor == "" {
			return pullRequestStatusChecks{}, errors.New("variable \"endCursor\" must be null or a non-empty string")
		}
	}
	return pullRequestStatusChecks{ID: id, Cursor: cursor}, nil
}

func parseOperation(request graphQLRequest) (*ast.OperationDefinition, error) {
	document, queryErrors := gqlparser.LoadQuery(schema, request.Query)
	if queryErrors != nil {
		return nil, fmt.Errorf("invalid GraphQL query: %s", queryErrors.Error())
	}
	var operation *ast.OperationDefinition
	if request.OperationName != "" {
		operation = document.Operations.ForName(request.OperationName)
	} else if len(document.Operations) == 1 {
		operation = document.Operations[0]
	}
	if operation == nil || operation.Operation != ast.Query {
		return nil, errors.New("a single named GraphQL query operation is required")
	}
	return operation, nil
}

func requiredStringVariable(variables map[string]json.RawMessage, name string) (string, error) {
	raw, ok := variables[name]
	if !ok {
		return "", fmt.Errorf("variable %q is required", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("variable %q must be a string", name)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("variable %q must not be empty", name)
	}
	return value, nil
}

func requiredIntVariable(variables map[string]json.RawMessage, name string) (int64, error) {
	raw, ok := variables[name]
	if !ok {
		return 0, fmt.Errorf("variable %q is required", name)
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil || value <= 0 {
		return 0, fmt.Errorf("variable %q must be a positive integer", name)
	}
	return value, nil
}

func validateRepositoryInfoShape(operation *ast.OperationDefinition) error {
	root, err := exactFields(operation.SelectionSet, "repository")
	if err != nil {
		return unsupportedShape(err)
	}
	repositoryField := root["repository"]
	if !variableArgument(repositoryField, "owner", "owner") || !variableArgument(repositoryField, "name", "name") || len(repositoryField.Arguments) != 2 {
		return unsupportedShape(errors.New("repository arguments must be owner and name variables"))
	}

	repositoryFields, err := exactFields(repositoryField.SelectionSet, "nameWithOwner", "parent")
	if err != nil {
		return unsupportedShape(err)
	}
	if len(repositoryFields["nameWithOwner"].SelectionSet) != 0 {
		return unsupportedShape(errors.New("nameWithOwner cannot have subfields"))
	}
	parentFields, err := exactFields(repositoryFields["parent"].SelectionSet, "id", "name", "owner")
	if err != nil {
		return unsupportedShape(err)
	}
	if len(parentFields["id"].SelectionSet) != 0 || len(parentFields["name"].SelectionSet) != 0 {
		return unsupportedShape(errors.New("parent scalar fields cannot have subfields"))
	}
	ownerFields, err := exactFields(parentFields["owner"].SelectionSet, "id", "login")
	if err != nil {
		return unsupportedShape(err)
	}
	if len(ownerFields["id"].SelectionSet) != 0 || len(ownerFields["login"].SelectionSet) != 0 {
		return unsupportedShape(errors.New("owner scalar fields cannot have subfields"))
	}
	return nil
}

func validatePullRequestForBranchShape(operation *ast.OperationDefinition) (pullRequestSelection, error) {
	root, err := exactFields(operation.SelectionSet, "repository")
	if err != nil {
		return pullRequestSelection{}, unsupportedPullRequestShape(err)
	}
	repositoryField := root["repository"]
	if !variableArgument(repositoryField, "owner", "owner") || !variableArgument(repositoryField, "name", "repo") || len(repositoryField.Arguments) != 2 {
		return pullRequestSelection{}, unsupportedPullRequestShape(errors.New("repository arguments must be owner and repo variables"))
	}
	repositoryFields, err := exactFields(repositoryField.SelectionSet, "pullRequests", "defaultBranchRef")
	if err != nil {
		return pullRequestSelection{}, unsupportedPullRequestShape(err)
	}
	pulls := repositoryFields["pullRequests"]
	if len(pulls.Arguments) != 4 ||
		!variableArgument(pulls, "headRefName", "headRefName") ||
		!variableArgument(pulls, "states", "states") ||
		!literalArgument(pulls, "first", ast.IntValue, "30") ||
		!orderByArgument(pulls) {
		return pullRequestSelection{}, unsupportedPullRequestShape(errors.New("pullRequests arguments do not match the supported query"))
	}
	connectionFields, err := exactFields(pulls.SelectionSet, "nodes")
	if err != nil {
		return pullRequestSelection{}, unsupportedPullRequestShape(err)
	}
	selection, err := validatePullRequestSelection(connectionFields["nodes"].SelectionSet)
	if err != nil {
		return pullRequestSelection{}, unsupportedPullRequestShape(err)
	}
	defaultBranchFields, err := exactFields(repositoryFields["defaultBranchRef"].SelectionSet, "name")
	if err != nil || len(defaultBranchFields["name"].SelectionSet) != 0 {
		if err == nil {
			err = errors.New("defaultBranchRef.name cannot have subfields")
		}
		return pullRequestSelection{}, unsupportedPullRequestShape(err)
	}
	return selection, nil
}

func validatePullRequestByNumberShape(operation *ast.OperationDefinition) (pullRequestSelection, error) {
	root, err := exactFields(operation.SelectionSet, "repository")
	if err != nil {
		return pullRequestSelection{}, unsupportedPullRequestShape(err)
	}
	repositoryField := root["repository"]
	if len(repositoryField.Arguments) != 2 || !variableArgument(repositoryField, "owner", "owner") || !variableArgument(repositoryField, "name", "repo") {
		return pullRequestSelection{}, unsupportedPullRequestShape(errors.New("repository arguments must be owner and repo variables"))
	}
	repositoryFields, err := exactFields(repositoryField.SelectionSet, "pullRequest")
	if err != nil {
		return pullRequestSelection{}, unsupportedPullRequestShape(err)
	}
	pull := repositoryFields["pullRequest"]
	if len(pull.Arguments) != 1 || !variableArgument(pull, "number", "pr_number") {
		return pullRequestSelection{}, unsupportedPullRequestShape(errors.New("pullRequest.number must use pr_number"))
	}
	selection, err := validatePullRequestSelection(pull.SelectionSet)
	if err != nil {
		return pullRequestSelection{}, unsupportedPullRequestShape(err)
	}
	return selection, nil
}

func validatePullRequestSelection(selectionSet ast.SelectionSet) (pullRequestSelection, error) {
	allowed := map[string]bool{
		"number": true, "url": true, "state": true, "id": true, "baseRefName": true, "headRefName": true,
		"headRefOid": true, "mergedAt": true, "closedAt": true, "isCrossRepository": true,
		"headRepository": true, "headRepositoryOwner": true, "mergeable": true, "mergeStateStatus": true, "reviewDecision": true,
	}
	result := pullRequestSelection{Fields: fieldSet{}}
	if len(selectionSet) == 0 {
		return result, errors.New("pull request selection must not be empty")
	}
	for _, item := range selectionSet {
		field, ok := item.(*ast.Field)
		if !ok || field.Alias != field.Name || !allowed[field.Name] || result.Fields.Has(field.Name) {
			return result, errors.New("unsupported pull request field selection")
		}
		result.Fields[field.Name] = struct{}{}
		switch field.Name {
		case "headRepository":
			fields, err := exactFieldsSubset(field.SelectionSet, "id", "name", "nameWithOwner")
			if err != nil {
				return result, err
			}
			result.HeadRepository = fields
		case "headRepositoryOwner":
			fields, err := validateHeadRepositoryOwner(field.SelectionSet)
			if err != nil {
				return result, err
			}
			result.HeadRepositoryOwner = fields
		default:
			if len(field.SelectionSet) != 0 {
				return result, fmt.Errorf("field %q cannot have subfields", field.Name)
			}
		}
	}
	return result, nil
}

func validateHeadRepositoryOwner(selectionSet ast.SelectionSet) (fieldSet, error) {
	if len(selectionSet) != 3 {
		return nil, errors.New("headRepositoryOwner must select id, login, and the User name fragment")
	}
	fields := make(ast.SelectionSet, 0, 2)
	var userFragment *ast.InlineFragment
	for _, selection := range selectionSet {
		switch value := selection.(type) {
		case *ast.Field:
			fields = append(fields, value)
		case *ast.InlineFragment:
			if userFragment != nil {
				return nil, errors.New("multiple inline fragments are not supported")
			}
			userFragment = value
		default:
			return nil, errors.New("fragment spreads are not supported")
		}
	}
	ownerFields, err := exactFields(fields, "id", "login")
	if err != nil || len(ownerFields["id"].SelectionSet) != 0 || len(ownerFields["login"].SelectionSet) != 0 {
		return nil, errors.New("headRepositoryOwner scalar selection is unsupported")
	}
	if userFragment == nil || userFragment.TypeCondition != "User" {
		return nil, errors.New("headRepositoryOwner must include an inline User fragment")
	}
	userFields, err := exactFields(userFragment.SelectionSet, "name")
	if err != nil || len(userFields["name"].SelectionSet) != 0 {
		return nil, errors.New("User fragment must select name")
	}
	return fieldSet{"id": {}, "login": {}, "name": {}}, nil
}

func exactFieldsSubset(selectionSet ast.SelectionSet, names ...string) (fieldSet, error) {
	if len(selectionSet) == 0 {
		return nil, errors.New("field selection must not be empty")
	}
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	result := fieldSet{}
	for _, item := range selectionSet {
		field, ok := item.(*ast.Field)
		if !ok || field.Alias != field.Name || len(field.SelectionSet) != 0 {
			return nil, errors.New("unsupported nested field selection")
		}
		if _, ok := allowed[field.Name]; !ok || result.Has(field.Name) {
			return nil, errors.New("unsupported nested field selection")
		}
		result[field.Name] = struct{}{}
	}
	return result, nil
}

func literalArgument(field *ast.Field, name string, kind ast.ValueKind, raw string) bool {
	argument := field.Arguments.ForName(name)
	return argument != nil && argument.Value != nil && argument.Value.Kind == kind && argument.Value.Raw == raw
}

func orderByArgument(field *ast.Field) bool {
	argument := field.Arguments.ForName("orderBy")
	if argument == nil || argument.Value == nil || argument.Value.Kind != ast.ObjectValue || len(argument.Value.Children) != 2 {
		return false
	}
	orderField := argument.Value.Children.ForName("field")
	direction := argument.Value.Children.ForName("direction")
	return orderField != nil && direction != nil &&
		orderField.Kind == ast.EnumValue && orderField.Raw == "CREATED_AT" &&
		direction.Kind == ast.EnumValue && direction.Raw == "DESC"
}

func exactFields(selectionSet ast.SelectionSet, names ...string) (map[string]*ast.Field, error) {
	if len(selectionSet) != len(names) {
		return nil, errors.New("unexpected field selection")
	}
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[name] = struct{}{}
	}
	fields := make(map[string]*ast.Field, len(names))
	for _, selection := range selectionSet {
		field, ok := selection.(*ast.Field)
		if !ok || field.Alias != field.Name {
			return nil, errors.New("aliases and fragments are not supported")
		}
		if _, ok := wanted[field.Name]; !ok {
			return nil, fmt.Errorf("field %q is not supported", field.Name)
		}
		if _, duplicate := fields[field.Name]; duplicate {
			return nil, fmt.Errorf("field %q is duplicated", field.Name)
		}
		fields[field.Name] = field
	}
	return fields, nil
}

func variableArgument(field *ast.Field, argumentName, variableName string) bool {
	argument := field.Arguments.ForName(argumentName)
	return argument != nil && argument.Value != nil && argument.Value.Kind == ast.Variable && argument.Value.Raw == variableName
}

func unsupportedShape(err error) error {
	return fmt.Errorf("unsupported RepositoryInfo query shape: %w", err)
}

func unsupportedPullRequestShape(err error) error {
	return fmt.Errorf("unsupported PullRequestForBranch query shape: %w", err)
}
