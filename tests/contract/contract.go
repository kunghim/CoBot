// Package contract checks the API's real requests and responses against
// api/openapi.yaml while the tests run. Wrap a server's handler; what passes
// through is compared with the specification, stricter than it reads: an object
// may not carry a field the specification does not name.
//
// Known deviations are listed in known.json and the operations seen in
// covered.json. Check compares them at the end of a full test run: a new
// deviation fails, a fixed one still listed fails, and coverage may only grow.
package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
)

var (
	setupOnce sync.Once
	router    routers.Router
	setupErr  error

	mu         sync.Mutex
	covered    = map[string]bool{}
	deviations = map[string]bool{}
)

func here() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func setup() {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filepath.Join(here(), "..", "..", "api", "openapi.yaml"))
	if err != nil {
		setupErr = err
		return
	}
	if err := doc.Validate(context.Background()); err != nil {
		setupErr = err
		return
	}
	doc.Servers = nil
	router, setupErr = legacy.NewRouter(doc)
}

// Wrap checks everything the handler serves under the API's paths.
func Wrap(next http.Handler) http.Handler {
	setupOnce.Do(setup)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/internal/") {
			next.ServeHTTP(w, r)
			return
		}
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		capture := &capturing{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(capture, r)
		verify(r, body, capture)
	})
}

// capturing passes everything on as it comes, so streams keep streaming, and
// keeps what JSON it saw for the check afterwards.
type capturing struct {
	http.ResponseWriter
	status int
	wrote  bool
	body   bytes.Buffer
}

func (c *capturing) WriteHeader(status int) {
	if !c.wrote {
		c.status, c.wrote = status, true
	}
	c.ResponseWriter.WriteHeader(status)
}

func (c *capturing) Write(data []byte) (int, error) {
	c.wrote = true
	if strings.HasPrefix(c.Header().Get("Content-Type"), "application/json") {
		c.body.Write(data)
	}
	return c.ResponseWriter.Write(data)
}

func (c *capturing) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *capturing) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func verify(r *http.Request, body []byte, c *capturing) {
	if setupErr != nil {
		record("setup", "", 0, setupErr)
		return
	}
	check := r.Clone(context.Background())
	check.Body = io.NopCloser(bytes.NewReader(body))
	check.Host = ""
	check.URL.Host, check.URL.Scheme = "", ""
	route, params, err := router.FindRoute(check)
	if err != nil {
		note(fmt.Sprintf("%s %s|route|%d|%s", r.Method, normalize(r.URL.Path), c.status, err))
		return
	}
	op := route.Operation.OperationID
	mu.Lock()
	covered[op] = true
	mu.Unlock()

	options := &openapi3filter.Options{MultiError: true, AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}
	input := &openapi3filter.RequestValidationInput{Request: check, PathParams: params, Route: route, Options: options}
	// A refused request may be refused on purpose; one the server accepted
	// has to be one the specification allows.
	if c.status < 400 {
		if err := openapi3filter.ValidateRequest(context.Background(), input); err != nil {
			record(op, "request", c.status, err)
		}
		if bodySpec := route.Operation.RequestBody; bodySpec != nil && bodySpec.Value != nil {
			noteUndeclared(op, "request", c.status, bodySpec.Value.Content, body)
		}
	}
	contentType := c.Header().Get("Content-Type")
	responseInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: input, Status: c.status, Header: c.Header(),
		Options: &openapi3filter.Options{MultiError: true, ExcludeResponseBody: !strings.HasPrefix(contentType, "application/json")},
	}
	responseInput.SetBodyBytes(c.body.Bytes())
	if err := openapi3filter.ValidateResponse(context.Background(), responseInput); err != nil {
		record(op, "response", c.status, err)
	}
	response := route.Operation.Responses.Status(c.status)
	if response == nil {
		response = route.Operation.Responses.Default()
	}
	if response != nil && response.Value != nil {
		noteUndeclared(op, "response", c.status, response.Value.Content, c.body.Bytes())
	}
}

func noteUndeclared(op, kind string, status int, content openapi3.Content, body []byte) {
	media := content.Get("application/json")
	if media == nil || media.Schema == nil {
		return
	}
	var value any
	if json.Unmarshal(body, &value) != nil {
		return // not JSON: the validator has its own say about that
	}
	for _, leaf := range undeclared([]*openapi3.SchemaRef{media.Schema}, value, "") {
		note(fmt.Sprintf("%s|%s|%d|%s", op, kind, status, leaf))
	}
}

// undeclared finds the fields of value that no schema names, unless the
// specification says what extra fields look like. It looks at the parts of a
// composed schema together: a field named by one part of an allOf is declared,
// even though the other parts do not mention it. Setting additionalProperties
// on the parts would judge each alone, and would change the shared schemas
// they refer to. The parts of a oneOf or anyOf count together too, so a field
// of the branch that did not match goes unnoticed.
func undeclared(schemas []*openapi3.SchemaRef, value any, pointer string) []string {
	parts := flatten(schemas)
	var found []string
	switch typed := value.(type) {
	case []any:
		var items []*openapi3.SchemaRef
		for _, part := range parts {
			if part.Items != nil {
				items = append(items, part.Items)
			}
		}
		for _, element := range typed {
			found = append(found, undeclared(items, element, pointer+"/*")...)
		}
	case map[string]any:
		named, open := false, false
		var extra []*openapi3.SchemaRef
		for _, part := range parts {
			named = named || len(part.Properties) > 0
			open = open || part.AdditionalProperties.Has != nil && *part.AdditionalProperties.Has
			if part.AdditionalProperties.Schema != nil {
				open = true
				extra = append(extra, part.AdditionalProperties.Schema)
			}
		}
		for key, field := range typed {
			var fieldSchemas []*openapi3.SchemaRef
			for _, part := range parts {
				if schema, ok := part.Properties[key]; ok {
					fieldSchemas = append(fieldSchemas, schema)
				}
			}
			if len(fieldSchemas) == 0 {
				if named && !open {
					at := pointer
					if at == "" {
						at = "/"
					}
					found = append(found, fmt.Sprintf("%s properties: property %q is unsupported", at, key))
					continue
				}
				fieldSchemas = extra
			}
			found = append(found, undeclared(fieldSchemas, field, pointer+"/"+key)...)
		}
	}
	return found
}

// flatten returns the schemas and every part they are composed of.
func flatten(schemas []*openapi3.SchemaRef) []*openapi3.Schema {
	var parts []*openapi3.Schema
	for _, ref := range schemas {
		if ref == nil || ref.Value == nil {
			continue
		}
		s := ref.Value
		parts = append(parts, s)
		parts = append(parts, flatten(s.AllOf)...)
		parts = append(parts, flatten(s.OneOf)...)
		parts = append(parts, flatten(s.AnyOf)...)
	}
	return parts
}

func note(key string) {
	mu.Lock()
	defer mu.Unlock()
	deviations[key] = true
}

// record files one failure per place in the schema: the values differ from
// request to request, what is wrong does not.
func record(op, kind string, status int, err error) {
	for _, leaf := range leaves(err) {
		note(fmt.Sprintf("%s|%s|%d|%s", op, kind, status, leaf))
	}
}

var (
	idLike  = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F-]{27}`)
	indexes = regexp.MustCompile(`/\d+(/|$)`)
)

func normalize(s string) string { return idLike.ReplaceAllString(s, "{id}") }

func leaves(err error) []string {
	var multi openapi3.MultiError
	if errors.As(err, &multi) {
		var out []string
		for _, e := range multi {
			out = append(out, leaves(e)...)
		}
		return out
	}
	var schema *openapi3.SchemaError
	if errors.As(err, &schema) {
		pointer := "/" + strings.Join(schema.JSONPointer(), "/")
		return []string{indexes.ReplaceAllString(pointer, "/*$1") + " " + schema.SchemaField + ": " + shortReason(schema.Reason)}
	}
	var response *openapi3filter.ResponseError
	if errors.As(err, &response) && response.Err == nil {
		return []string{response.Reason}
	}
	var request *openapi3filter.RequestError
	if errors.As(err, &request) && request.Err == nil {
		return []string{request.Reason}
	}
	first, _, _ := strings.Cut(err.Error(), "\n")
	return []string{normalize(first)}
}

var quoted = regexp.MustCompile(`value "[^"]*"`)

func shortReason(reason string) string { return quoted.ReplaceAllString(reason, `value "…"`) }

// Result is what a run found.
type Result struct {
	Covered    []string
	Deviations []string
}

// Current returns what has been seen so far, sorted.
func Current() Result {
	mu.Lock()
	defer mu.Unlock()
	return Result{Covered: sortedKeys(covered), Deviations: sortedKeys(deviations)}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// needsBrowser are reached only by tests that skip on a machine without a
// Chromium-based browser, so a run may or may not have seen them.
var needsBrowser = []string{"closeTeamBrowserTab", "getTeamBrowser", "openTeamBrowserTab", "sendTeamBrowserInput", "setTeamBrowserTabViewport"}

func opOf(entry string) string {
	op, _, _ := strings.Cut(entry, "|")
	return op
}

func mayBeAbsent(entry string) bool { return slices.Contains(needsBrowser, opOf(entry)) }

func readList(name string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(here(), name))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []string
	return list, json.Unmarshal(data, &list)
}

func writeList(name string, list []string) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(here(), name), append(data, '\n'), 0o644)
}

// Update rewrites covered.json and known.json from this run.
func Update() error {
	result := Current()
	if err := writeList("covered.json", result.Covered); err != nil {
		return err
	}
	return writeList("known.json", result.Deviations)
}

// Check compares a full run with the committed lists and says what is wrong.
func Check() []string {
	result := Current()
	knownList, err := readList("known.json")
	if err != nil {
		return []string{err.Error()}
	}
	coveredList, err := readList("covered.json")
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	for _, d := range result.Deviations {
		if !slices.Contains(knownList, d) {
			problems = append(problems, "API differs from api/openapi.yaml: "+d)
		}
	}
	for _, d := range knownList {
		if !slices.Contains(result.Deviations, d) && !(mayBeAbsent(d) && !slices.Contains(result.Covered, opOf(d))) {
			problems = append(problems, "no longer differs, remove it from tests/contract/known.json: "+d)
		}
	}
	for _, op := range coveredList {
		if !slices.Contains(result.Covered, op) && !mayBeAbsent(op) {
			problems = append(problems, "operation no longer reached by any test: "+op)
		}
	}
	for _, op := range result.Covered {
		if !slices.Contains(coveredList, op) {
			problems = append(problems, "operation newly covered, add it to tests/contract/covered.json: "+op)
		}
	}
	return problems
}
