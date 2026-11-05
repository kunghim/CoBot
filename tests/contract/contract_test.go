package contract

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

const testSpec = `
openapi: 3.0.3
info: {title: x, version: "1"}
paths: {}
components:
  schemas:
    Envelope:
      type: object
      properties:
        data: {}
        requestId: {type: string}
    Status:
      type: object
      properties:
        source: {type: string}
    Update:
      allOf:
        - $ref: "#/components/schemas/Status"
        - type: object
          properties:
            state: {type: string}
    Wrapped:
      allOf:
        - $ref: "#/components/schemas/Envelope"
        - type: object
          properties:
            data:
              $ref: "#/components/schemas/Update"
    List:
      type: object
      properties:
        items:
          type: array
          items:
            $ref: "#/components/schemas/Status"
    Open:
      type: object
      properties:
        fixed: {type: string}
      additionalProperties:
        $ref: "#/components/schemas/Status"
    Free:
      type: object
      additionalProperties: true
      properties:
        fixed: {type: string}
`

func TestUndeclaredFields(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData([]byte(testSpec))
	if err != nil {
		t.Fatal(err)
	}
	object := func(pairs ...any) map[string]any {
		out := map[string]any{}
		for i := 0; i < len(pairs); i += 2 {
			out[pairs[i].(string)] = pairs[i+1]
		}
		return out
	}
	cases := []struct {
		name, schema string
		value        any
		want         []string
	}{
		{"parts of an allOf count together", "Wrapped",
			object("requestId", "r", "data", object("source", "s", "state", "t")), nil},
		{"a field no part names is found, at its place", "Wrapped",
			object("requestId", "r", "bad", 1, "data", object("worse", 1)), []string{
				`/ properties: property "bad" is unsupported`,
				`/data properties: property "worse" is unsupported`,
			}},
		{"a shared schema alone is not widened by what composes it", "Status",
			object("source", "s", "state", "t"), []string{`/ properties: property "state" is unsupported`}},
		{"every array element is looked at", "List",
			object("items", []any{object("source", "s"), object("bad", 1)}), []string{`/items/* properties: property "bad" is unsupported`}},
		{"extra fields follow the schema given for them", "Open",
			object("any", object("source", "s"), "other", object("bad", 1)), []string{`/other properties: property "bad" is unsupported`}},
		{"extra fields may be allowed outright", "Free", object("anything", 1), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := undeclared([]*openapi3.SchemaRef{doc.Components.Schemas[c.schema]}, c.value, "")
			sort.Strings(got)
			if !slices.Equal(got, c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// Through the real specification: the envelope's requestId is declared, a field
// nobody declared is not.
func TestWrapReportsOnlyWhatTheSpecificationDoesNotName(t *testing.T) {
	handler := Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"requestId":"r","data":{"items":[],"madeUp":1}}`))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/teams", nil))
	var teams []string
	for _, d := range Current().Deviations {
		if strings.HasPrefix(d, "listTeams|") {
			teams = append(teams, d)
		}
	}
	if !slices.Contains(teams, `listTeams|response|200|/data properties: property "madeUp" is unsupported`) {
		t.Fatalf("the made-up field was not reported: %q", teams)
	}
	for _, d := range teams {
		if strings.Contains(d, "requestId") {
			t.Fatalf("requestId is declared by the envelope: %q", d)
		}
	}
}
