package presets

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
)

// measuredVendors is the vendor table as it was probed on 2026-10-20. The
// catalogue must match it exactly: a vendor that disappears, or one that
// appears without a measured row, fails the test. Anthropic served only the
// Anthropic Messages dialect, which CoBot no longer speaks, so it is not in
// the catalogue and its key is not recognised.
var measuredVendors = []string{
	"openai",
	"deepseek",
	"kimi",
	"zhipu",
	"zai",
	"minimax",
	"dashscope",
	"mimo",
	"xai",
	"openrouter",
	"opencode-zen",
}

// measuredBases holds the base every vendor was seen answering on, per
// dialect. OpenAI official is empty on purpose: it inherits the engine's own
// provider.
var measuredBases = map[string]map[string]string{
	"openai": {},
	"deepseek": {
		domain.DialectOpenAIResponses: "api.deepseek.com/v1",
	},
	"kimi": {
		domain.DialectOpenAIResponses: "api.moonshot.cn/v1",
	},
	"zhipu": {
		domain.DialectOpenAIResponses: "open.bigmodel.cn/api/paas/v4",
	},
	"zai": {
		domain.DialectOpenAIResponses: "api.z.ai/api/paas/v4",
	},
	"minimax": {
		domain.DialectOpenAIResponses: "api.minimaxi.com/v1",
	},
	"dashscope": {
		domain.DialectOpenAIResponses: "dashscope.aliyuncs.com/compatible-mode/v1",
	},
	"mimo": {
		domain.DialectOpenAIResponses: "api.xiaomimimo.com/v1",
	},
	"xai": {
		domain.DialectOpenAIResponses: "api.x.ai/v1",
	},
	"openrouter": {
		domain.DialectOpenAIResponses: "openrouter.ai/api/v1",
	},
	"opencode-zen": {
		domain.DialectOpenAIResponses: "opencode.ai/zen/v1",
	},
}

// officialID is the one preset that inherits the engine's own provider.
const officialID = "openai"

// isLetter reports whether r is an ASCII letter. A logo tile mark may be
// written in either case.
func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func TestAllCoversMeasuredVendors(t *testing.T) {
	all := All()
	if len(all) != len(measuredVendors) {
		t.Fatalf("All() returned %d presets, want %d", len(all), len(measuredVendors))
	}
	seen := make(map[string]bool, len(all))
	for _, p := range all {
		if seen[p.ID] {
			t.Errorf("duplicate preset id %q", p.ID)
		}
		seen[p.ID] = true
	}
	for _, id := range measuredVendors {
		if !seen[id] {
			t.Errorf("preset %q is missing from All()", id)
		}
	}
}

func TestPresetShape(t *testing.T) {
	colorPattern := regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	for _, p := range All() {
		if p.Name == "" {
			t.Errorf("preset %q has no display name", p.ID)
		}
		if len(p.Short) < 2 || len(p.Short) > 3 {
			t.Errorf("preset %q short mark %q is not two or three letters", p.ID, p.Short)
		}
		for _, r := range p.Short {
			if !isLetter(r) {
				t.Errorf("preset %q short mark %q is not a letter", p.ID, p.Short)
			}
		}
		if !colorPattern.MatchString(p.Color) {
			t.Errorf("preset %q color %q is not a hex colour", p.ID, p.Color)
		}
		if p.KeyURL == "" {
			t.Errorf("preset %q has no key url", p.ID)
		}
		if strings.Contains(p.KeyURL, "://") {
			t.Errorf("preset %q key url %q carries a scheme", p.ID, p.KeyURL)
		}
		switch p.AuthMode {
		case domain.ProviderAuthChatGPT, domain.ProviderAuthAPIKey:
		default:
			t.Errorf("preset %q has auth mode %q", p.ID, p.AuthMode)
		}
		if p.Official != (p.ID == officialID) {
			t.Errorf("preset %q official flag is %v, want %v", p.ID, p.Official, p.ID == officialID)
		}
	}
}

func TestOfficialPresetInheritsAndUsesChatGPT(t *testing.T) {
	preset, ok := ByID(officialID)
	if !ok {
		t.Fatalf("preset %q not found", officialID)
	}
	if len(preset.Endpoints) != 0 {
		t.Errorf("official preset declares %d endpoints, want 0", len(preset.Endpoints))
	}
	if preset.AuthMode != domain.ProviderAuthChatGPT {
		t.Errorf("official preset auth mode is %q, want %q", preset.AuthMode, domain.ProviderAuthChatGPT)
	}
}

func TestEndpointsMatchMeasuredTable(t *testing.T) {
	for _, p := range All() {
		want, ok := measuredBases[p.ID]
		if !ok {
			t.Errorf("preset %q has no measured row", p.ID)
			continue
		}
		if len(want) == 0 {
			if len(p.Endpoints) != 0 {
				t.Errorf("preset %q declares endpoints, want none", p.ID)
			}
			continue
		}
		if len(p.Endpoints) == 0 {
			t.Errorf("preset %q declares no endpoint", p.ID)
			continue
		}
		got := make(map[string]string, len(p.Endpoints))
		for _, ep := range p.Endpoints {
			switch ep.Dialect {
			case domain.DialectOpenAIResponses:
			default:
				t.Errorf("preset %q declares unknown dialect %q", p.ID, ep.Dialect)
			}
			if ep.BaseURL == "" {
				t.Errorf("preset %q dialect %q has no base url", p.ID, ep.Dialect)
			}
			if strings.Contains(ep.BaseURL, "://") {
				t.Errorf("preset %q base url %q carries a scheme", p.ID, ep.BaseURL)
			}
			if _, duplicate := got[ep.Dialect]; duplicate {
				t.Errorf("preset %q declares dialect %q twice", p.ID, ep.Dialect)
			}
			got[ep.Dialect] = ep.BaseURL
		}
		for dialect, base := range want {
			gotBase, ok := got[dialect]
			if !ok {
				t.Errorf("preset %q is missing dialect %q", p.ID, dialect)
				continue
			}
			if gotBase != base {
				t.Errorf("preset %q dialect %q base is %q, want %q", p.ID, dialect, gotBase, base)
			}
		}
		for dialect := range got {
			if _, ok := want[dialect]; !ok {
				t.Errorf("preset %q declares unmeasured dialect %q", p.ID, dialect)
			}
		}
	}
}

func TestFallbackModelsCarryNoMeasurements(t *testing.T) {
	for _, p := range All() {
		seen := make(map[string]bool, len(p.Models))
		for _, m := range p.Models {
			if m.ID == "" {
				t.Errorf("preset %q has a model without an id", p.ID)
			}
			if seen[m.ID] {
				t.Errorf("preset %q lists model %q twice", p.ID, m.ID)
			}
			seen[m.ID] = true
			if m.ContextWindow != 0 {
				t.Errorf("preset %q model %q claims context window %d, which was never measured", p.ID, m.ID, m.ContextWindow)
			}
			if m.Status != "" {
				t.Errorf("preset %q model %q claims status %q, which was never probed", p.ID, m.ID, m.Status)
			}
			if m.DefaultEffort != "" || len(m.SupportedEfforts) != 0 {
				t.Errorf("preset %q model %q claims effort levels, which were never measured", p.ID, m.ID)
			}
		}
		for _, ep := range p.Endpoints {
			if len(p.Models) == 0 {
				// A gateway that fronts other vendors has no single id to
				// ship, so it has no fallback catalogue either.
				continue
			}
			if len(ep.Models) == 0 {
				t.Errorf("preset %q dialect %q has no fallback models", p.ID, ep.Dialect)
			}
		}
	}
}

func TestByID(t *testing.T) {
	for _, id := range measuredVendors {
		preset, ok := ByID(id)
		if !ok {
			t.Errorf("ByID(%q) found nothing", id)
			continue
		}
		if preset.ID != id {
			t.Errorf("ByID(%q) returned %q", id, preset.ID)
		}
	}
	for _, id := range []string{"", "nope", "OpenAI", "deep seek"} {
		if _, ok := ByID(id); ok {
			t.Errorf("ByID(%q) found something", id)
		}
	}
}

func TestAllReturnsCopies(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("All() returned nothing")
	}
	all[0].Name = "mutated"
	if len(all[0].Endpoints) > 0 {
		all[0].Endpoints[0].BaseURL = "mutated"
	}
	fresh := All()
	if fresh[0].Name == "mutated" {
		t.Error("All() handed out the catalogue itself")
	}
	if len(fresh[0].Endpoints) > 0 && fresh[0].Endpoints[0].BaseURL == "mutated" {
		t.Error("All() handed out the endpoint list itself")
	}
}

func TestDetectVendorKnowsRealKeys(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "openai",
			key:  "sk-proj-" + strings.Repeat("A0b_", 10),
			want: "openai",
		},
		{
			name: "xai",
			key:  "xai-" + strings.Repeat("z", 60),
			want: "xai",
		},
		{
			name: "openrouter",
			key:  "sk-or-v1-" + strings.Repeat("a1b2", 8),
			want: "openrouter",
		},
		{
			name: "minimax",
			key:  "eyJ" + strings.Repeat("a", 20) + "." + strings.Repeat("b", 20) + "." + strings.Repeat("c", 30),
			want: "minimax",
		},
		{
			name: "deepseek",
			key:  "sk-" + strings.Repeat("a1", 16),
			want: "deepseek",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DetectVendor(c.key); got != c.want {
				t.Errorf("DetectVendor() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDetectVendorRefusesToGuess(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{name: "empty", key: ""},
		{name: "blank", key: "   "},
		{name: "short", key: "sk-abc"},
		{name: "plain", key: "not-a-key"},
		{name: "other vendor", key: "gsk_test_0123456789abcdef"},
		{name: "anthropic key", key: "sk-ant-api03-" + strings.Repeat("a", 80)},
		{name: "short deepseek", key: "sk-" + strings.Repeat("a1", 15)},
		{name: "long deepseek", key: "sk-" + strings.Repeat("a1", 17)},
		{
			// Zhipu BigModel and Z.ai issue keys in the same shape and live
			// on different hosts, so the key cannot say which one it is.
			name: "ambiguous zhipu and zai",
			key:  strings.Repeat("a1", 16) + "." + strings.Repeat("b2", 8),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DetectVendor(c.key); got != "" {
				t.Errorf("DetectVendor() = %q, want no vendor", got)
			}
		})
	}
}

func TestDetectVendorResultIsInCatalogue(t *testing.T) {
	keys := []string{
		"sk-proj-" + strings.Repeat("A0b_", 10),
		"xai-" + strings.Repeat("z", 60),
		"sk-or-v1-" + strings.Repeat("a1b2", 8),
		"eyJ" + strings.Repeat("a", 20) + "." + strings.Repeat("b", 20) + "." + strings.Repeat("c", 30),
		"sk-" + strings.Repeat("a1", 16),
		strings.Repeat("a1", 16) + "." + strings.Repeat("b2", 8),
		"sk-abc",
		"",
	}
	for _, key := range keys {
		got := DetectVendor(key)
		if got == "" {
			continue
		}
		if _, ok := ByID(got); !ok {
			t.Errorf("DetectVendor(%q) = %q, which is not in the catalogue", key, got)
		}
	}
}

func TestKeyPatternsCompile(t *testing.T) {
	for _, p := range All() {
		for _, pattern := range p.KeyPatterns {
			if _, err := regexp.Compile(pattern); err != nil {
				t.Errorf("preset %q has an invalid key pattern %q: %v", p.ID, pattern, err)
			}
		}
	}
}
