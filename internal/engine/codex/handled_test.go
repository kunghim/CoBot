package codex

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// caseStrings collects the string literals in the case clauses of the named
// functions, and those compared with ==, in one source file.
func caseStrings(t *testing.T, file string, funcs ...string) []string {
	return literals(t, file, true, funcs...)
}

// switchCases is caseStrings without the == comparisons.
func switchCases(t *testing.T, file string, funcs ...string) []string {
	return literals(t, file, false, funcs...)
}

func literals(t *testing.T, file string, equals bool, funcs ...string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	add := func(expr ast.Expr) {
		if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if value, err := strconv.Unquote(lit.Value); err == nil {
				out = append(out, value)
			}
		}
	}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !slices.Contains(funcs, fn.Name.Name) {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.CaseClause:
				for _, expr := range n.List {
					add(expr)
				}
			case *ast.BinaryExpr:
				if equals && n.Op == token.EQL {
					add(n.X)
					add(n.Y)
				}
			}
			return true
		})
	}
	return out
}

func TestHandledListsMatchTheCode(t *testing.T) {
	isMessage := func(s string) bool { return strings.Contains(s, "/") || s == "error" || s == "warning" || s == "guardianWarning" }
	var messages []string
	for _, s := range append(caseStrings(t, "server.go", "handleNotify", "handleRequest"),
		caseStrings(t, "../../app/engine_events.go", "onNotification")...) {
		if isMessage(s) {
			messages = append(messages, s)
		}
	}
	for _, method := range messages {
		if !slices.Contains(HandledMessages, method) {
			t.Errorf("the code handles %q but HandledMessages does not list it", method)
		}
	}
	for _, method := range HandledMessages {
		if !slices.Contains(messages, method) {
			t.Errorf("HandledMessages lists %q but no handler has a case for it", method)
		}
	}

	items := switchCases(t, "steps.go", "normalizeStep")
	for _, s := range caseStrings(t, "../../app/engine_events.go", "onNotification") {
		if s == "agentMessage" {
			items = append(items, s)
		}
	}
	sort.Strings(items)
	items = slices.Compact(items)
	if !slices.Equal(items, HandledItemTypes) {
		t.Errorf("item types in code %v, HandledItemTypes %v", items, HandledItemTypes)
	}
}
