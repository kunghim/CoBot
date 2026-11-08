package workspace

import (
	"slices"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// A kind the interface promises must be one the viewer can tell apart and serve.
func TestEveryKindInTheInterfaceHasAFormat(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, value := range spec.Components.Schemas["WorkspaceFile"].Value.Properties["kind"].Value.Enum {
		kinds = append(kinds, value.(string))
	}
	if len(kinds) == 0 {
		t.Fatal("the interface lists no kinds")
	}
	// A directory has no content; a blocked file's is never served.
	exempt := []string{"directory", "blocked"}
	for _, kind := range kinds {
		has := slices.ContainsFunc(formats, func(f Format) bool { return f.Kind == kind })
		if has == slices.Contains(exempt, kind) {
			t.Errorf("kind %q: has a format = %v, exempt = %v", kind, has, slices.Contains(exempt, kind))
		}
	}
	for _, format := range formats {
		if !slices.Contains(kinds, format.Kind) {
			t.Errorf("format %q is not in the interface", format.Kind)
		}
	}
}

func TestAnExtensionNamesOneKind(t *testing.T) {
	seen := map[string]string{}
	for _, format := range formats {
		for _, ext := range format.Extensions {
			if other, ok := seen[ext]; ok {
				t.Errorf("%s is both %s and %s", ext, other, format.Kind)
			}
			seen[ext] = format.Kind
		}
	}
}

func TestKindsAndContentTypes(t *testing.T) {
	for _, tc := range []struct{ name, kind, contentType string }{
		{"a.md", "markdown", "text/plain; charset=utf-8"},
		{"A.HTM", "html", "text/plain; charset=utf-8"},
		{"a.svg", "svg", "image/svg+xml"},
		{"a.png", "image", "image/png"},
		{"A.JPG", "image", "image/jpeg"},
		{"a.ico", "image", "image/x-icon"},
		{"a.pdf", "pdf", "application/pdf"},
		{"a.mp3", "audio", "audio/mpeg"},
		{"a.opus", "audio", "audio/ogg"},
		{"a.webm", "video", "video/webm"},
		{"a.docx", "document", binaryContentType},
		{"a.csv", "spreadsheet", binaryContentType},
		{"a.pptx", "presentation", binaryContentType},
		{"a.ipynb", "notebook", "text/plain; charset=utf-8"},
		{"a.tex", "latex", "text/plain; charset=utf-8"},
		{"a.doc", "binary", binaryContentType},
		{"a.go", "text", "text/plain; charset=utf-8"},
	} {
		got := kind(tc.name, []byte("hello"))
		if got != tc.kind {
			t.Errorf("kind(%s) = %s, want %s", tc.name, got, tc.kind)
		}
		if contentType := ContentType(got, tc.name); contentType != tc.contentType {
			t.Errorf("ContentType(%s) = %s, want %s", tc.name, contentType, tc.contentType)
		}
	}
	for _, kind := range []string{"directory", "blocked", "file", "nonsense"} {
		if got := ContentType(kind, "a.txt"); got != binaryContentType {
			t.Errorf("ContentType(%s) = %s", kind, got)
		}
	}
	// A file named nothing it knows is judged by its bytes.
	if got := kind("a.go", []byte{'a', 0, 'b'}); got != "binary" {
		t.Errorf("a file with a NUL byte = %s", got)
	}
}
