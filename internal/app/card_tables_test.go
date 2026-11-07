package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func interfaceEnum(t *testing.T, schema, property string) []string {
	t.Helper()
	spec, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	for _, value := range spec.Components.Schemas[schema].Value.Properties[property].Value.Enum {
		values = append(values, value.(string))
	}
	if len(values) == 0 {
		t.Fatalf("%s.%s lists nothing", schema, property)
	}
	slices.Sort(values)
	return values
}

// domainConstants are the values of the domain's constants named with a prefix.
func domainConstants(t *testing.T, prefix string, except ...string) []string {
	t.Helper()
	packages, err := parser.ParseDir(token.NewFileSet(), "../domain", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.ValueSpec)
				if !ok || len(spec.Values) != len(spec.Names) {
					return true
				}
				for i, name := range spec.Names {
					literal, ok := spec.Values[i].(*ast.BasicLit)
					skip := slices.ContainsFunc(except, func(e string) bool { return strings.HasPrefix(name.Name, e) })
					if !ok || literal.Kind != token.STRING || skip || !strings.HasPrefix(name.Name, prefix) {
						continue
					}
					value, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatal(err)
					}
					values = append(values, value)
				}
				return true
			})
		}
	}
	slices.Sort(values)
	return values
}

func sameList(t *testing.T, what string, got, want []string) {
	t.Helper()
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("%s: %q, want %q", what, got, want)
	}
}

func TestEveryApprovalTypeHasADecider(t *testing.T) {
	types := interfaceEnum(t, "Approval", "type")
	sameList(t, "the domain's types", domainConstants(t, "Approval", "ApprovalState"), types)
	var rows []string
	for kind := range approvalDeciders {
		rows = append(rows, kind)
	}
	sameList(t, "the table's rows", rows, types)
}

func TestEveryQuestionKindAndDecisionHasOneRoute(t *testing.T) {
	kinds := interfaceEnum(t, "Question", "kind")
	sameList(t, "the domain's kinds", domainConstants(t, "QuestionKind"), kinds)
	// An answer with no decision is a member's question being answered.
	decisions := append(interfaceEnum(t, "AnswerQuestionRequest", "decision"), "")
	slices.Sort(decisions)
	var routedKinds, routedDecisions []string
	for _, route := range questionRoutes {
		routedKinds = append(routedKinds, route.kinds...)
		routedDecisions = append(routedDecisions, route.decisions...)
	}
	sameList(t, "the kinds with a route", routedKinds, kinds)
	sameList(t, "the decisions with a route", routedDecisions, decisions)
}
