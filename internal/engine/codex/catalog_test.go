package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func official(slug, visibility string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"slug": slug, "display_name": slug, "visibility": visibility, "base_instructions": "the engine's prompt",
		"context_window": 272000, "max_context_window": 872000, "apply_patch_tool_type": "freeform",
		"supported_reasoning_levels": []any{}, "input_modalities": []string{"text", "image"},
	})
	return raw
}

func entriesOf(t *testing.T, data []byte) map[string]map[string]any {
	t.Helper()
	var list struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, entry := range list.Models {
		out[entry["slug"].(string)] = entry
	}
	return out
}

func TestACatalogIsOnlyNeededForAWindowAboveTheFallback(t *testing.T) {
	if NeedsCatalog([]ClientModel{{Slug: "a", ContextWindow: 272000}, {Slug: "b", ContextWindow: 20000}}) {
		t.Fatal("a window the engine gives on its own needs no catalog")
	}
	if !NeedsCatalog([]ClientModel{{Slug: "a", ContextWindow: 1048576}}) {
		t.Fatal("a million-token model needs one")
	}
}

func TestTheCatalogKeepsTheEnginesModelsAndAddsTheThirdPartyOnes(t *testing.T) {
	data, err := composeCatalog(
		[]json.RawMessage{official("gpt-6-sol", "list"), official("gpt-5.5", "list"), official("codex-auto-review", "hide")},
		[]json.RawMessage{official("gpt-5.5", "list")},
		[]ClientModel{
			{Slug: "mimo-v2.6-flash", Name: "MiMo", ContextWindow: 1048576, Input: []string{"image", "video"}, Toggle: true},
			{Slug: "glm-5.3", ContextWindow: 1000000, Efforts: []string{"low", "high", "max"}},
			{Slug: "gpt-6-sol", ContextWindow: 5000},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	entries := entriesOf(t, data)
	for _, slug := range []string{"gpt-6-sol", "gpt-5.5", "codex-auto-review", "mimo-v2.6-flash", "glm-5.3"} {
		if entries[slug] == nil {
			t.Errorf("%s is missing: %v", slug, entries)
		}
	}
	// A model the engine lists keeps the engine's own entry.
	if entries["gpt-6-sol"]["context_window"].(float64) != 272000 {
		t.Errorf("the engine's entry was replaced: %v", entries["gpt-6-sol"])
	}
	mimo := entries["mimo-v2.6-flash"]
	if mimo["context_window"].(float64) != 1048576 || mimo["max_context_window"].(float64) != 1048576 {
		t.Errorf("window = %v / %v", mimo["context_window"], mimo["max_context_window"])
	}
	if mimo["visibility"] != "hide" || mimo["base_instructions"] != "the engine's prompt" {
		t.Errorf("a third-party model must be hidden and keep the engine's prompt: %v", mimo)
	}
	// The engine reads text and image; video is left to CoBot, the engine is not told of it.
	if !reflect.DeepEqual(mimo["input_modalities"], []any{"text", "image"}) {
		t.Errorf("input = %v", mimo["input_modalities"])
	}
	levels, _ := json.Marshal(mimo["supported_reasoning_levels"])
	if !strings.Contains(string(levels), `"none"`) || mimo["default_reasoning_level"] != nil {
		t.Errorf("a switch-only model: levels %s default %v", levels, mimo["default_reasoning_level"])
	}
	glm := entries["glm-5.3"]
	glmLevels, _ := json.Marshal(glm["supported_reasoning_levels"])
	if strings.Contains(string(glmLevels), `"none"`) || !strings.Contains(string(glmLevels), `"max"`) || glm["default_reasoning_level"] != "low" {
		t.Errorf("levels %s default %v", glmLevels, glm["default_reasoning_level"])
	}
}

func TestACatalogWithNothingToBuildFromIsRefused(t *testing.T) {
	if _, err := composeCatalog([]json.RawMessage{official("codex-auto-review", "hide")}, []json.RawMessage{official("codex-auto-review", "hide")}, []ClientModel{{Slug: "x", ContextWindow: 1000000}}); err == nil {
		t.Fatal("a catalog was built with no entry to take the prompt from")
	}
}

func TestTheEngineIsStartedWithTheCatalogFile(t *testing.T) {
	if got := (ClientCatalog{}).CatalogArgs(); got != nil {
		t.Fatalf("no catalog, args = %v", got)
	}
	// Written with the platform's own separator, so it reads the same on every OS.
	path := filepath.Join("home", "cobot-model-catalog.json")
	got := (ClientCatalog{Path: path}).CatalogArgs()
	if !reflect.DeepEqual(got, []string{"-c", `model_catalog_json="home/cobot-model-catalog.json"`}) {
		t.Fatalf("args = %v", got)
	}
}

// With a real engine: the catalog is built, the engine reads it, and a
// second build reuses the file.
func TestARealEngineAcceptsTheGeneratedCatalog(t *testing.T) {
	binary, err := Locate("")
	if err != nil {
		t.Skip("no codex engine on this machine")
	}
	home := t.TempDir()
	env := append(os.Environ(), "CODEX_HOME="+home)
	builder := &CatalogBuilder{Home: home}
	models := []ClientModel{{Slug: "cobot-test-model", Name: "Test", ContextWindow: 1048576, Toggle: true, Efforts: []string{"low", "high"}}}
	catalog, needed, err := builder.Build(context.Background(), binary, env, models)
	if err != nil || !needed || catalog.Path != filepath.Join(home, catalogFileName) || catalog.Fingerprint == "" {
		t.Fatalf("catalog = %+v needed=%v err=%v", catalog, needed, err)
	}
	again, _, err := builder.Build(context.Background(), binary, env, models)
	if err != nil || again != catalog {
		t.Fatalf("a second build = %+v (%v), want %+v", again, err, catalog)
	}
	changed, _, err := builder.Build(context.Background(), binary, env, []ClientModel{{Slug: "cobot-test-model", ContextWindow: 900000}})
	if err != nil || changed.Fingerprint == catalog.Fingerprint {
		t.Fatalf("another model description must give another fingerprint: %+v (%v)", changed, err)
	}
	none, needed, err := builder.Build(context.Background(), binary, env, []ClientModel{{Slug: "small", ContextWindow: 20000}})
	if err != nil || needed || none.Path != "" {
		t.Fatalf("no model needs one: %+v needed=%v err=%v", none, needed, err)
	}
}
