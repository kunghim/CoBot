package engine_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/kunghim/CoBot/internal/domain"
)

// stepKinds are the values of the const group that starts with StepReasoning.
func stepKinds(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "engine.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		var kinds []string
		found := false
		for _, s := range group.Specs {
			spec := s.(*ast.ValueSpec)
			found = found || spec.Names[0].Name == "StepReasoning"
			value, err := strconv.Unquote(spec.Values[0].(*ast.BasicLit).Value)
			if err != nil {
				t.Fatal(err)
			}
			kinds = append(kinds, value)
		}
		if found {
			return kinds
		}
	}
	t.Fatal("no const group holds StepReasoning")
	return nil
}

// What the engine and CoBot call a step must be what the interface promises.
func TestEveryStepKindIsInTheInterface(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var promised []string
	for _, value := range spec.Components.Schemas["RunStep"].Value.Properties["kind"].Value.Enum {
		promised = append(promised, value.(string))
	}
	kinds := append(stepKinds(t), domain.StepKindNotice, domain.StepKindRetry)
	slices.Sort(kinds)
	slices.Sort(promised)
	if !slices.Equal(kinds, promised) {
		t.Fatalf("the code has %q, the interface %q", kinds, promised)
	}
}
