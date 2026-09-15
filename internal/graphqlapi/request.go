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
}

type Repository {
  id: ID!
  name: String!
  nameWithOwner: String!
  owner: RepositoryOwner!
  parent: Repository
  pullRequests(headRefName: String!, states: [PullRequestState!], first: Int!, orderBy: PullRequestOrder!): PullRequestConnection!
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

type PullRequest {
  number: Int!
  url: String!
  state: PullRequestState!
  id: ID!
  baseRefName: String!
  headRefName: String!
  isCrossRepository: Boolean!
  headRepositoryOwner: RepositoryOwner
}

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
	if err := validatePullRequestForBranchShape(operation); err != nil {
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
	return pullRequestForBranch{Owner: owner, Repo: repo, HeadRefName: headRefName}, nil
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

func validatePullRequestForBranchShape(operation *ast.OperationDefinition) error {
	root, err := exactFields(operation.SelectionSet, "repository")
	if err != nil {
		return unsupportedPullRequestShape(err)
	}
	repositoryField := root["repository"]
	if !variableArgument(repositoryField, "owner", "owner") || !variableArgument(repositoryField, "name", "repo") || len(repositoryField.Arguments) != 2 {
		return unsupportedPullRequestShape(errors.New("repository arguments must be owner and repo variables"))
	}
	repositoryFields, err := exactFields(repositoryField.SelectionSet, "pullRequests", "defaultBranchRef")
	if err != nil {
		return unsupportedPullRequestShape(err)
	}
	pulls := repositoryFields["pullRequests"]
	if len(pulls.Arguments) != 4 ||
		!variableArgument(pulls, "headRefName", "headRefName") ||
		!variableArgument(pulls, "states", "states") ||
		!literalArgument(pulls, "first", ast.IntValue, "30") ||
		!orderByArgument(pulls) {
		return unsupportedPullRequestShape(errors.New("pullRequests arguments do not match the supported query"))
	}
	connectionFields, err := exactFields(pulls.SelectionSet, "nodes")
	if err != nil {
		return unsupportedPullRequestShape(err)
	}
	nodeFields, err := exactFields(connectionFields["nodes"].SelectionSet,
		"number", "url", "state", "id", "baseRefName", "headRefName", "isCrossRepository", "headRepositoryOwner")
	if err != nil {
		return unsupportedPullRequestShape(err)
	}
	for _, name := range []string{"number", "url", "state", "id", "baseRefName", "headRefName", "isCrossRepository"} {
		if len(nodeFields[name].SelectionSet) != 0 {
			return unsupportedPullRequestShape(fmt.Errorf("field %q cannot have subfields", name))
		}
	}
	if err := validateHeadRepositoryOwner(nodeFields["headRepositoryOwner"].SelectionSet); err != nil {
		return unsupportedPullRequestShape(err)
	}
	defaultBranchFields, err := exactFields(repositoryFields["defaultBranchRef"].SelectionSet, "name")
	if err != nil || len(defaultBranchFields["name"].SelectionSet) != 0 {
		if err == nil {
			err = errors.New("defaultBranchRef.name cannot have subfields")
		}
		return unsupportedPullRequestShape(err)
	}
	return nil
}

func validateHeadRepositoryOwner(selectionSet ast.SelectionSet) error {
	if len(selectionSet) != 3 {
		return errors.New("headRepositoryOwner must select id, login, and the User name fragment")
	}
	fields := make(ast.SelectionSet, 0, 2)
	var userFragment *ast.InlineFragment
	for _, selection := range selectionSet {
		switch value := selection.(type) {
		case *ast.Field:
			fields = append(fields, value)
		case *ast.InlineFragment:
			if userFragment != nil {
				return errors.New("multiple inline fragments are not supported")
			}
			userFragment = value
		default:
			return errors.New("fragment spreads are not supported")
		}
	}
	ownerFields, err := exactFields(fields, "id", "login")
	if err != nil || len(ownerFields["id"].SelectionSet) != 0 || len(ownerFields["login"].SelectionSet) != 0 {
		return errors.New("headRepositoryOwner scalar selection is unsupported")
	}
	if userFragment == nil || userFragment.TypeCondition != "User" {
		return errors.New("headRepositoryOwner must include an inline User fragment")
	}
	userFields, err := exactFields(userFragment.SelectionSet, "name")
	if err != nil || len(userFields["name"].SelectionSet) != 0 {
		return errors.New("User fragment must select name")
	}
	return nil
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
