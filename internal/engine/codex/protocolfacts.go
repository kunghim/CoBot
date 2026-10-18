package codex

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// The app-server protocol as a list of facts, one line each: a method, a
// field and its type, an enum value, a variant of a union. Comparing two
// lists shows what a Codex release added, removed or changed without
// running anything; descriptions are left out so wording edits stay quiet.

//go:embed protocol_baseline.json
var baselineJSON []byte

// ProtocolFacts is the protocol of one Codex version.
type ProtocolFacts struct {
	CodexVersion string   `json:"codexVersion"`
	Facts        []string `json:"facts"`
}

// Baseline is the protocol this CoBot build was written against.
func Baseline() (ProtocolFacts, error) {
	var facts ProtocolFacts
	err := json.Unmarshal(baselineJSON, &facts)
	return facts, err
}

// The schema bundles `generate-json-schema` writes; together they hold every
// method and type of both protocol generations.
var schemaBundles = []string{"codex_app_server_protocol.schemas.json", "codex_app_server_protocol.v2.schemas.json"}

// ExportProtocol asks the given Codex binary for its protocol and lists it.
func ExportProtocol(ctx context.Context, binary, version string) (ProtocolFacts, error) {
	dir, err := os.MkdirTemp("", "cobot-protocol-*")
	if err != nil {
		return ProtocolFacts{}, err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, binary, "app-server", "generate-json-schema", "--out", dir)
	hideChildWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ProtocolFacts{}, fmt.Errorf("codex app-server generate-json-schema: %w: %s", err, strings.TrimSpace(string(out)))
	}
	facts := ProtocolFacts{CodexVersion: version}
	for _, bundle := range schemaBundles {
		data, err := os.ReadFile(filepath.Join(dir, bundle))
		if err != nil {
			return ProtocolFacts{}, fmt.Errorf("read %s: %w", bundle, err)
		}
		list, err := SchemaFacts(strings.TrimSuffix(bundle, ".json"), data)
		if err != nil {
			return ProtocolFacts{}, fmt.Errorf("%s: %w", bundle, err)
		}
		facts.Facts = append(facts.Facts, list...)
	}
	sort.Strings(facts.Facts)
	return facts, nil
}

// SchemaFacts lists every definition of one JSON Schema bundle as facts
// prefixed with the bundle name.
func SchemaFacts(bundle string, data []byte) ([]string, error) {
	var root struct {
		Definitions map[string]any `json:"definitions"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	var facts []string
	add := func(fact string) { facts = append(facts, fact) }
	for name, schema := range root.Definitions {
		walkSchema(bundle+"/"+name, schema, add)
	}
	sort.Strings(facts)
	return facts, nil
}

func walkSchema(path string, node any, add func(string)) {
	schema, ok := node.(map[string]any)
	if !ok {
		return
	}
	if ref, ok := schema["$ref"].(string); ok {
		add(path + " → " + strings.TrimPrefix(ref, "#/definitions/"))
	}
	if kind := schemaType(schema["type"]); kind != "" {
		add(path + " : " + kind)
	}
	if values, ok := schema["enum"].([]any); ok {
		for _, value := range values {
			add(fmt.Sprintf("%s = %v", path, value))
		}
	}
	if value, ok := schema["const"]; ok {
		add(fmt.Sprintf("%s = %v", path, value))
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		for name, prop := range props {
			walkSchema(path+"."+name, prop, add)
		}
	}
	if required, ok := schema["required"].([]any); ok {
		for _, name := range required {
			add(fmt.Sprintf("%s.%v (required)", path, name))
		}
	}
	if items, ok := schema["items"]; ok {
		walkSchema(path+"[]", items, add)
	}
	if extra, ok := schema["additionalProperties"].(map[string]any); ok {
		walkSchema(path+"{}", extra, add)
	}
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		variants, ok := schema[key].([]any)
		if !ok {
			continue
		}
		for i, variant := range variants {
			variantPath := fmt.Sprintf("%s|%s", path, variantLabel(variant, i))
			add(variantPath)
			walkSchema(variantPath, variant, add)
		}
	}
}

// variantLabel names a union member by its tag ("type=commandExecution",
// "method=turn/started"), else by title, else by position.
func variantLabel(node any, index int) string {
	schema, _ := node.(map[string]any)
	if props, ok := schema["properties"].(map[string]any); ok {
		for _, key := range []string{"type", "method", "kind", "status"} {
			if prop, ok := props[key].(map[string]any); ok {
				if values, ok := prop["enum"].([]any); ok && len(values) == 1 {
					return fmt.Sprintf("%s=%v", key, values[0])
				}
			}
		}
	}
	if ref, ok := schema["$ref"].(string); ok {
		return strings.TrimPrefix(ref, "#/definitions/")
	}
	if title, ok := schema["title"].(string); ok && title != "" {
		return title
	}
	return fmt.Sprintf("#%d", index)
}

func schemaType(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, part := range v {
			parts = append(parts, fmt.Sprint(part))
		}
		sort.Strings(parts)
		return strings.Join(parts, "|")
	}
	return ""
}

// ProtocolDiff is what changed between two protocol listings.
type ProtocolDiff struct {
	Added, Removed []string
}

// DiffProtocol compares a baseline with a newer listing.
func DiffProtocol(old, current ProtocolFacts) ProtocolDiff {
	before := map[string]bool{}
	for _, fact := range old.Facts {
		before[fact] = true
	}
	after := map[string]bool{}
	var diff ProtocolDiff
	for _, fact := range current.Facts {
		after[fact] = true
		if !before[fact] {
			diff.Added = append(diff.Added, fact)
		}
	}
	for _, fact := range old.Facts {
		if !after[fact] {
			diff.Removed = append(diff.Removed, fact)
		}
	}
	return diff
}

// methodUnions are the definitions whose variants are the protocol's
// messages; a change there is a message type appearing or going away.
var methodUnions = []string{"ClientRequest", "ServerNotification", "ServerRequest", "ClientNotification"}

// IsMethodFact reports whether a fact names a protocol message, and which.
func IsMethodFact(fact string) (union, method string, ok bool) {
	for _, name := range methodUnions {
		for _, bundle := range schemaBundles {
			prefix := strings.TrimSuffix(bundle, ".json") + "/" + name + "|method="
			if rest, found := strings.CutPrefix(fact, prefix); found && !strings.ContainsAny(rest, ".: |") {
				return name, rest, true
			}
		}
	}
	return "", "", false
}
