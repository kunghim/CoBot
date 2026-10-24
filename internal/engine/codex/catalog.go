package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The engine knows a model's context window from its own model list. A model
// it has never heard of (every third-party model) gets a fallback of
// defaultContextWindow, and a window set per thread can only lower it. To
// give such a model a larger window, the engine has to be started with a
// model catalog that lists it (`model_catalog_json`).
//
// Two facts about that catalog shape this file: the engine reads it once, at
// start, and once it is given, the engine knows only the models in it and no
// longer refreshes its own list. So the file is rebuilt every time an engine
// is started, from the engine's own list plus the third-party models, and
// only when some model needs a window above the fallback.

// defaultContextWindow is the window the engine assumes for a model it does
// not list.
const defaultContextWindow = 272000

// catalogFileName is the generated catalog, kept in the engine's home.
const catalogFileName = "cobot-model-catalog.json"

// ClientModel is a third-party model the catalog describes.
type ClientModel struct {
	Slug          string
	Name          string
	ContextWindow int
	// Input lists what the model takes besides text; the engine knows image only.
	Input []string
	// Toggle says thinking can be switched off; Efforts are its levels.
	Toggle  bool
	Efforts []string
	// DefaultEffort is the level used when none is chosen.
	DefaultEffort string
}

// ClientCatalog is a generated catalog file.
type ClientCatalog struct {
	// Path is the file the engine is started with.
	Path string
	// Fingerprint identifies its content; two engines started with the same
	// fingerprint know the same models.
	Fingerprint string
}

// CatalogArgs is what an engine is started with to use the catalog.
func (c ClientCatalog) CatalogArgs() []string {
	if c.Path == "" {
		return nil
	}
	return []string{"-c", "model_catalog_json=" + tomlString(filepath.ToSlash(c.Path))}
}

func tomlString(value string) string {
	b, _ := json.Marshal(value)
	return string(b)
}

// CatalogBuilder builds the catalog for an engine binary.
type CatalogBuilder struct {
	// Home is the engine's home (CODEX_HOME): the catalog is written here, and
	// the engine's cached account model list is read from here.
	Home string

	mu      sync.Mutex
	bundled struct {
		version string
		models  []json.RawMessage
	}
	last struct {
		key     string
		catalog ClientCatalog
		needed  bool
	}
}

// NeedsCatalog says whether any of the models needs a window the engine would
// not give it on its own.
func NeedsCatalog(models []ClientModel) bool {
	for _, model := range models {
		if model.ContextWindow > defaultContextWindow {
			return true
		}
	}
	return false
}

// Build returns the catalog for the models, or an empty one (and false) when
// none is needed. An error means the catalog could not be made or did not
// pass the engine's own parse: the caller starts the engine without it.
func (b *CatalogBuilder) Build(ctx context.Context, binary string, env []string, models []ClientModel) (ClientCatalog, bool, error) {
	if !NeedsCatalog(models) {
		return ClientCatalog{}, false, nil
	}
	version, err := ProbeVersion(ctx, binary)
	if err != nil {
		return ClientCatalog{}, false, err
	}
	bundled, err := b.bundledModels(ctx, binary, env, version)
	if err != nil {
		return ClientCatalog{}, false, err
	}
	official, source := bundled, "bundled"
	if cached, stamp, ok := b.cachedModels(version); ok {
		official, source = cached, "cache:"+stamp
	}
	data, err := composeCatalog(official, bundled, models)
	if err != nil {
		return ClientCatalog{}, false, err
	}
	sum := sha256.Sum256(data)
	fingerprint := hex.EncodeToString(sum[:8])
	key := version + "|" + source + "|" + fingerprint

	b.mu.Lock()
	defer b.mu.Unlock()
	path := filepath.Join(b.Home, catalogFileName)
	if b.last.key == key && fileHasHash(path, sum[:]) {
		return b.last.catalog, true, nil
	}
	if err := writeFileAtomic(path, data); err != nil {
		return ClientCatalog{}, false, err
	}
	if err := validateCatalog(ctx, binary, env, path, models); err != nil {
		_ = os.Remove(path)
		return ClientCatalog{}, false, err
	}
	catalog := ClientCatalog{Path: path, Fingerprint: fingerprint}
	b.last.key, b.last.catalog, b.last.needed = key, catalog, true
	return catalog, true, nil
}

func fileHasHash(path string, want []byte) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(data)
	return bytes.Equal(sum[:], want)
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type rawCatalog struct {
	Models []json.RawMessage `json:"models"`
}

// bundledModels is the model list built into the binary, which does not
// depend on the account. It is kept for as long as the version is the same.
func (b *CatalogBuilder) bundledModels(ctx context.Context, binary string, env []string, version string) ([]json.RawMessage, error) {
	b.mu.Lock()
	if b.bundled.version == version && len(b.bundled.models) > 0 {
		models := b.bundled.models
		b.mu.Unlock()
		return models, nil
	}
	b.mu.Unlock()
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, binary, "debug", "models", "--bundled")
	cmd.Env = env
	hideChildWindow(cmd)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("codex debug models --bundled: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var list rawCatalog
	if err := json.Unmarshal(out.Bytes(), &list); err != nil || len(list.Models) == 0 {
		return nil, errors.New("codex debug models --bundled returned no models")
	}
	b.mu.Lock()
	b.bundled.version, b.bundled.models = version, list.Models
	b.mu.Unlock()
	return list.Models, nil
}

// cachedModels is the list the engine cached for this account: the models
// and windows this account has, when the cache is for this very version.
func (b *CatalogBuilder) cachedModels(version string) ([]json.RawMessage, string, bool) {
	data, err := os.ReadFile(filepath.Join(b.Home, "models_cache.json"))
	if err != nil {
		return nil, "", false
	}
	var cache struct {
		FetchedAt     string            `json:"fetched_at"`
		ClientVersion string            `json:"client_version"`
		Models        []json.RawMessage `json:"models"`
	}
	if json.Unmarshal(data, &cache) != nil || cache.ClientVersion != version || len(cache.Models) == 0 {
		return nil, "", false
	}
	return cache.Models, cache.FetchedAt, true
}

func slugOf(raw json.RawMessage) string {
	var head struct {
		Slug string `json:"slug"`
	}
	_ = json.Unmarshal(raw, &head)
	return head.Slug
}

// composeCatalog is the engine's models followed by the third-party ones.
// A third-party entry is made from the most general entry of the engine's
// built-in list (the bundled one carries the whole prompt), so
// the prompt and the tools stay the engine's; only what describes the model
// itself is replaced. A model the engine already lists keeps the engine's entry.
func composeCatalog(official, bundled []json.RawMessage, models []ClientModel) ([]byte, error) {
	known := map[string]bool{}
	for _, raw := range official {
		known[slugOf(raw)] = true
	}
	template, err := templateEntry(bundled)
	if err != nil {
		return nil, err
	}
	sorted := append([]ClientModel(nil), models...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Slug < sorted[j].Slug })
	entries := append([]json.RawMessage(nil), official...)
	seen := map[string]bool{}
	for _, model := range sorted {
		if model.Slug == "" || known[model.Slug] || seen[model.Slug] || model.ContextWindow <= 0 {
			continue
		}
		seen[model.Slug] = true
		entry, err := thirdPartyEntry(template, model, len(entries))
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return json.Marshal(rawCatalog{Models: entries})
}

// templateEntry picks the entry a third-party model is made from: the one
// that lists with a base prompt, oldest first (the last that is listed).
func templateEntry(official []json.RawMessage) (map[string]any, error) {
	var chosen map[string]any
	for _, raw := range official {
		var entry map[string]any
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		prompt, _ := entry["base_instructions"].(string)
		if prompt == "" || entry["visibility"] != "list" {
			continue
		}
		if slug, _ := entry["slug"].(string); strings.HasPrefix(slug, "gpt-5.5") {
			return entry, nil
		}
		chosen = entry
	}
	if chosen == nil {
		return nil, errors.New("the engine's model list has no entry to build from")
	}
	return chosen, nil
}

func thirdPartyEntry(template map[string]any, model ClientModel, priority int) (json.RawMessage, error) {
	entry := make(map[string]any, len(template))
	for key, value := range template {
		entry[key] = value
	}
	name := model.Name
	if name == "" {
		name = model.Slug
	}
	entry["slug"] = model.Slug
	entry["display_name"] = name
	entry["description"] = name
	// Hidden: the engine's own picker (the ChatGPT account's) must not list
	// these; CoBot lists them under their provider.
	entry["visibility"] = "hide"
	entry["priority"] = priority + 100
	entry["context_window"] = model.ContextWindow
	entry["max_context_window"] = model.ContextWindow
	entry["upgrade"] = nil
	entry["availability_nux"] = nil
	entry["service_tiers"] = []any{}
	entry["additional_speed_tiers"] = []any{}
	// What the engine's own models rely on and a third-party endpoint may
	// refuse: the freeform patch tool (a custom tool, which MiMo's endpoint
	// answers with protocol_unsupported; the engine knows no other form), a
	// verbosity setting, the hosted search tool, original-size images.
	entry["supports_search_tool"] = false
	entry["apply_patch_tool_type"] = nil
	entry["support_verbosity"] = false
	entry["default_verbosity"] = nil
	entry["web_search_tool_type"] = "text"
	entry["supports_image_detail_original"] = false
	input := []string{"text"}
	for _, kind := range model.Input {
		if kind == "image" {
			input = append(input, kind)
		}
	}
	entry["input_modalities"] = input

	levels := []map[string]string{}
	if model.Toggle {
		levels = append(levels, map[string]string{"effort": "none", "description": "none"})
	}
	for _, effort := range model.Efforts {
		levels = append(levels, map[string]string{"effort": effort, "description": effort})
	}
	entry["supported_reasoning_levels"] = levels
	switch {
	case containsString(model.Efforts, model.DefaultEffort):
		entry["default_reasoning_level"] = model.DefaultEffort
	case containsString(model.Efforts, "medium"):
		entry["default_reasoning_level"] = "medium"
	case len(model.Efforts) > 0:
		entry["default_reasoning_level"] = model.Efforts[0]
	default:
		entry["default_reasoning_level"] = nil
	}
	return json.Marshal(entry)
}

func containsString(list []string, value string) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

// validateCatalog has the engine itself read the file, so a format the
// engine no longer accepts is found here and not when a turn starts.
func validateCatalog(ctx context.Context, binary string, env []string, path string, models []ClientModel) error {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, binary, "debug", "models", "-c", "model_catalog_json="+tomlString(filepath.ToSlash(path)))
	cmd.Env = env
	hideChildWindow(cmd)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the engine does not accept the model catalog: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var list rawCatalog
	if err := json.Unmarshal(out.Bytes(), &list); err != nil {
		return fmt.Errorf("the engine's answer about the model catalog is not readable: %w", err)
	}
	listed := map[string]bool{}
	for _, raw := range list.Models {
		listed[slugOf(raw)] = true
	}
	for _, model := range models {
		if model.ContextWindow > defaultContextWindow && !listed[model.Slug] && !officialHas(path, model.Slug) {
			return fmt.Errorf("the engine does not list %s from the model catalog", model.Slug)
		}
	}
	return nil
}

// officialHas is true when the catalog file holds the slug from the engine's
// own list (so a third-party model of the same name was left out on purpose).
func officialHas(path, slug string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var list rawCatalog
	if json.Unmarshal(data, &list) != nil {
		return false
	}
	for _, raw := range list.Models {
		if slugOf(raw) == slug {
			return true
		}
	}
	return false
}
