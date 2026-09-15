package graphqlapi

import (
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
}

type RepositoryOwner {
  id: ID!
  login: String!
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

func parseRepositoryInfo(request graphQLRequest) (repositoryInfo, error) {
	document, queryErrors := gqlparser.LoadQuery(schema, request.Query)
	if queryErrors != nil {
		return repositoryInfo{}, fmt.Errorf("invalid GraphQL query: %s", queryErrors.Error())
	}

	var operation *ast.OperationDefinition
	if request.OperationName != "" {
		operation = document.Operations.ForName(request.OperationName)
	} else if len(document.Operations) == 1 {
		operation = document.Operations[0]
	}
	if operation == nil || operation.Operation != ast.Query || operation.Name != "RepositoryInfo" {
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
