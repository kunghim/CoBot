// Package presets ships CoBot's built-in catalogue of third-party model
// vendors.
//
// A preset is pure data plus local key recognition: it never touches the
// network. Two facts shape the catalogue.
//
// The engine speaks only the OpenAI Responses protocol. A vendor that offers
// nothing else is useless to it unless the engine also speaks Anthropic
// Messages, so every preset lists the dialects the vendor really serves and
// never one it does not. A preset that pointed at a missing surface would
// cost the user a failed setup that looks like a wrong key.
//
// The shipped model list is a fallback, not the source of truth. Every vendor
// here exposes GET {base}/models, so the live catalogue is fetched at runtime
// and this list only keeps the picker usable when discovery fails. It is
// therefore deliberately thin: real model ids, and no context windows, effort
// levels or probe statuses, because none of those were measured.
package presets

import (
	"regexp"
	"strings"

	"github.com/kunghim/CoBot/internal/domain"
)

// Preset is one vendor CoBot ships a definition for.
type Preset struct {
	// ID is stable and used as the preset reference in stored providers.
	ID string
	// Name is the display name.
	Name string
	// Short is the two or three letter mark drawn on the logo tile.
	Short string
	// Color is the logo tile colour, as a hex value.
	Color string
	// KeyURL is where the user gets a key. It carries no scheme, so the
	// caller decides whether to open it in a browser or show it.
	KeyURL string
	// KeyPatterns are regular expressions that identify this vendor's key.
	// They are matched locally, against text the user typed, and never
	// sent anywhere.
	KeyPatterns []string
	// Endpoints are the dialects the vendor offers.
	Endpoints []Endpoint
	// Models is the fallback catalogue, used when discovery fails.
	Models []domain.ProviderModel
	// Official is true only for the ChatGPT-login provider. An official
	// provider that needs no endpoint inherits the engine's own.
	Official bool
	// AuthMode is domain.ProviderAuthChatGPT or domain.ProviderAuthAPIKey.
	AuthMode string
}

// Endpoint is one dialect the vendor offers.
type Endpoint struct {
	// Dialect is the wire protocol the endpoint speaks.
	Dialect string
	// BaseURL is the host and path without a scheme; the caller adds
	// https://.
	BaseURL string
	// Models is the fallback catalogue reachable over this dialect.
	Models []domain.ProviderModel
}

// catalogue is the measured vendor table. The endpoints were probed on
// 2026-10-20: a dialect path answering 401, 403 or 400 exists, a 404 does
// not. Bases are written without a scheme so the caller can decide on
// https:// and nothing else.
var catalogue = []Preset{
	{
		ID:     "openai",
		Name:   "OpenAI",
		Short:  "OA",
		Color:  "#10A37F",
		KeyURL: "platform.openai.com/api-keys",
		// The official provider logs in with ChatGPT and stores no key, so
		// this pattern only helps when a user pastes an API key anyway.
		KeyPatterns: []string{
			`^sk-proj-[A-Za-z0-9_-]{40,}$`,
		},
		// Official OpenAI declares no endpoint on purpose. The engine
		// already knows the OpenAI provider and keeps the ChatGPT session,
		// so an endpoint here would only override a working login with a
		// guess. "Official" means "inherit", not "point somewhere else".
		Endpoints: nil,
		Models:    []domain.ProviderModel{fallbackModel("gpt-6.1-sol")},
		Official:  true,
		AuthMode:  domain.ProviderAuthChatGPT,
	},
	{
		ID:     "anthropic",
		Name:   "Anthropic",
		Short:  "AN",
		Color:  "#D97757",
		KeyURL: "console.anthropic.com/settings/keys",
		KeyPatterns: []string{
			`^sk-ant-api03-[A-Za-z0-9_-]{80,}$`,
		},
		// Anthropic does not serve the OpenAI Responses protocol at all —
		// that surface answers 404 — so this preset declares only the
		// Anthropic Messages dialect. Inventing a responses endpoint here
		// would send the engine to a path that does not exist.
		Endpoints: []Endpoint{
			{Dialect: domain.DialectAnthropicMessages, BaseURL: "api.anthropic.com"},
		},
		Models:   []domain.ProviderModel{fallbackModel("claude-opus-5")},
		AuthMode: domain.ProviderAuthAPIKey,
	},
	{
		ID:     "deepseek",
		Name:   "DeepSeek",
		Short:  "DS",
		Color:  "#4D6BFE",
		KeyURL: "platform.deepseek.com/api_keys",
		KeyPatterns: []string{
			`^sk-[a-f0-9]{32}$`,
		},
		Endpoints: []Endpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "api.deepseek.com/v1"},
			{Dialect: domain.DialectAnthropicMessages, BaseURL: "api.deepseek.com/anthropic"},
		},
		Models:   []domain.ProviderModel{fallbackModel("deepseek-v4-pro")},
		AuthMode: domain.ProviderAuthAPIKey,
	},
	{
		ID:     "kimi",
		Name:   "Moonshot Kimi",
		Short:  "KM",
		Color:  "#2B2B2B",
		KeyURL: "platform.moonshot.cn/console/api-keys",
		// No key pattern: Moonshot issues a plain "sk-" key, the same
		// shape as several other vendors, so recognising it locally would
		// be a guess. The user picks the vendor instead.
		Endpoints: []Endpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "api.moonshot.cn/v1"},
			{Dialect: domain.DialectAnthropicMessages, BaseURL: "api.moonshot.cn/anthropic"},
		},
		Models:   []domain.ProviderModel{fallbackModel("kimi-k3")},
		AuthMode: domain.ProviderAuthAPIKey,
	},
	{
		ID:     "zhipu",
		Name:   "Zhipu BigModel",
		Short:  "GLM",
		Color:  "#3859FF",
		KeyURL: "open.bigmodel.cn/usercenter/apikeys",
		KeyPatterns: []string{
			`^[a-f0-9]{32}\.[a-f0-9]{16}$`,
		},
		Endpoints: []Endpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "open.bigmodel.cn/api/paas/v4"},
			{Dialect: domain.DialectAnthropicMessages, BaseURL: "open.bigmodel.cn/api/anthropic"},
		},
		Models:   []domain.ProviderModel{fallbackModel("GLM-5.3")},
		AuthMode: domain.ProviderAuthAPIKey,
	},
	{
		ID:     "zai",
		Name:   "Z.ai",
		Short:  "ZAI",
		Color:  "#8B5CF6",
		KeyURL: "z.ai/manage/apikey/apikey-list",
		// Z.ai issues keys in the same shape as BigModel, and the two
		// platforms live on different hosts, so a key alone cannot tell
		// them apart. Detection reports such a key as unknown.
		KeyPatterns: []string{
			`^[a-f0-9]{32}\.[a-f0-9]{16}$`,
		},
		Endpoints: []Endpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "api.z.ai/api/paas/v4"},
			{Dialect: domain.DialectAnthropicMessages, BaseURL: "api.z.ai/api/anthropic"},
		},
		Models:   []domain.ProviderModel{fallbackModel("GLM-5.3")},
		AuthMode: domain.ProviderAuthAPIKey,
	},
	{
		ID:     "minimax",
		Name:   "MiniMax",
		Short:  "MM",
		Color:  "#F43F5E",
		KeyURL: "platform.minimaxi.com/user-center/basic-information/interface-key",
		KeyPatterns: []string{
			`^eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.`,
		},
		Endpoints: []Endpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "api.minimaxi.com/v1"},
			{Dialect: domain.DialectAnthropicMessages, BaseURL: "api.minimaxi.com/anthropic"},
		},
		Models:   []domain.ProviderModel{fallbackModel("MiniMax-M3")},
		AuthMode: domain.ProviderAuthAPIKey,
	},
	{
		ID:     "dashscope",
		Name:   "Alibaba Bailian",
		Short:  "QW",
		Color:  "#FF6A00",
		KeyURL: "bailian.console.aliyun.com",
		// No key pattern: Bailian issues a generic "sk-" key, shared with
		// other vendors that are not in this catalogue.
		Endpoints: []Endpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "dashscope.aliyuncs.com/compatible-mode/v1"},
			{Dialect: domain.DialectAnthropicMessages, BaseURL: "dashscope.aliyuncs.com/apps/anthropic"},
		},
		Models:   []domain.ProviderModel{fallbackModel("qwen3.8-max")},
		AuthMode: domain.ProviderAuthAPIKey,
	},
	{
		ID:     "mimo",
		Name:   "Xiaomi MiMo",
		Short:  "MI",
		Color:  "#0EA5E9",
		KeyURL: "platform.xiaomimimo.com",
		// No key pattern: MiMo issues a generic "sk-" key.
		Endpoints: []Endpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "api.xiaomimimo.com/v1"},
			{Dialect: domain.DialectAnthropicMessages, BaseURL: "api.xiaomimimo.com/anthropic"},
		},
		Models:   []domain.ProviderModel{fallbackModel("mimo-v2.5-pro")},
		AuthMode: domain.ProviderAuthAPIKey,
	},
	{
		ID:     "xai",
		Name:   "xAI",
		Short:  "XA",
		Color:  "#000000",
		KeyURL: "console.x.ai",
		KeyPatterns: []string{
			`^xai-[A-Za-z0-9]{60,}$`,
		},
		// xAI serves no Anthropic-compatible surface, so only the OpenAI
		// dialect is declared.
		Endpoints: []Endpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "api.x.ai/v1"},
		},
		Models:   []domain.ProviderModel{fallbackModel("grok-4.6")},
		AuthMode: domain.ProviderAuthAPIKey,
	},
	{
		ID:     "openrouter",
		Name:   "OpenRouter",
		Short:  "OR",
		Color:  "#6467F2",
		KeyURL: "openrouter.ai/keys",
		KeyPatterns: []string{
			`^sk-or-v1-[a-z0-9]{32,}$`,
		},
		Endpoints: []Endpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "openrouter.ai/api/v1"},
			{Dialect: domain.DialectAnthropicMessages, BaseURL: "openrouter.ai/api"},
		},
		// No fallback models: OpenRouter namespaces its catalogue per
		// upstream vendor ("vendor/model"), so there is no single id worth
		// shipping. The list from GET {base}/models is the real one.
		Models:   nil,
		AuthMode: domain.ProviderAuthAPIKey,
	},
	{
		ID:     "opencode-zen",
		Name:   "OpenCode Zen",
		Short:  "ZEN",
		Color:  "#0F766E",
		KeyURL: "opencode.ai/zen",
		Endpoints: []Endpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "opencode.ai/zen/v1"},
			{Dialect: domain.DialectAnthropicMessages, BaseURL: "opencode.ai/zen/v1"},
		},
		// No fallback models: Zen fronts several upstream vendors under
		// its own ids, and none of them was measured here.
		Models:   nil,
		AuthMode: domain.ProviderAuthAPIKey,
	},
}

// detectionOrder lists the presets by how distinctive their key shape is,
// most specific first. The order is written out instead of inherited from the
// catalogue so the outcome of a key never depends on map iteration or on
// where a preset is added later.
var detectionOrder = []string{
	"anthropic",  // sk-ant-api03-…
	"openai",     // sk-proj-…
	"xai",        // xai-…
	"openrouter", // sk-or-v1-…
	"minimax",    // eyJ… JWT style
	"zhipu",      // 32 hex . 16 hex
	"zai",        // same shape as zhipu
	"deepseek",   // sk- + 32 hex, the most generic shape in this file
}

// matcher is one compiled key pattern and the preset it identifies.
type matcher struct {
	presetID string
	re       *regexp.Regexp
}

// keyMatchers is compiled once, at start-up. A pattern that does not compile
// panics here rather than silently disabling one vendor's recognition for
// the rest of the process lifetime.
var keyMatchers = compileKeyMatchers()

// fallbackModel builds a catalogue entry for a model whose capabilities were
// not measured. Status stays empty: "not probed" is a real state, and
// writing "ok" or a context window that was never observed would make the
// picker state something untrue.
func fallbackModel(id string) domain.ProviderModel {
	return domain.ProviderModel{ID: id}
}

// compileKeyMatchers compiles every preset's key patterns, most specific
// preset first.
func compileKeyMatchers() []matcher {
	ordered := make([]Preset, 0, len(catalogue))
	byID := make(map[string]Preset, len(catalogue))
	for _, p := range catalogue {
		byID[p.ID] = p
	}
	seen := make(map[string]bool, len(catalogue))
	for _, id := range detectionOrder {
		p, ok := byID[id]
		if !ok {
			continue
		}
		ordered = append(ordered, p)
		seen[id] = true
	}
	for _, p := range catalogue {
		if !seen[p.ID] {
			ordered = append(ordered, p)
		}
	}
	var out []matcher
	for _, p := range ordered {
		for _, pattern := range p.KeyPatterns {
			out = append(out, matcher{presetID: p.ID, re: regexp.MustCompile(pattern)})
		}
	}
	return out
}

// init gives every endpoint the vendor's fallback catalogue. The shipped
// models are expected on each dialect the vendor offers, and a narrower
// list is only known after the runtime discovery.
func init() {
	for i := range catalogue {
		p := &catalogue[i]
		for j := range p.Endpoints {
			if len(p.Endpoints[j].Models) == 0 {
				p.Endpoints[j].Models = p.Models
			}
		}
	}
}

// All returns every preset, in a stable order.
func All() []Preset {
	out := make([]Preset, len(catalogue))
	for i, p := range catalogue {
		out[i] = clonePreset(p)
	}
	return out
}

// ByID looks a preset up.
func ByID(id string) (Preset, bool) {
	for _, p := range catalogue {
		if p.ID == id {
			return clonePreset(p), true
		}
	}
	return Preset{}, false
}

// DetectVendor returns the preset id whose key pattern matches, or "" when
// nothing matches. It never guesses: a key that two vendors claim, such as
// the shape Zhipu BigModel and Z.ai share, returns "" as well, because the
// key alone cannot say which of the two hosts it belongs to.
func DetectVendor(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	found := ""
	for _, m := range keyMatchers {
		if !m.re.MatchString(key) {
			continue
		}
		if found == "" {
			found = m.presetID
			continue
		}
		if found != m.presetID {
			return ""
		}
	}
	return found
}

// clonePreset copies the parts a caller could otherwise write through.
func clonePreset(p Preset) Preset {
	out := p
	if p.Endpoints != nil {
		out.Endpoints = make([]Endpoint, len(p.Endpoints))
		for i, ep := range p.Endpoints {
			ep.Models = cloneModels(ep.Models)
			out.Endpoints[i] = ep
		}
	}
	out.Models = cloneModels(p.Models)
	return out
}

// cloneModels copies a model list. The model values themselves are shared
// and must be treated as read-only.
func cloneModels(models []domain.ProviderModel) []domain.ProviderModel {
	if models == nil {
		return nil
	}
	out := make([]domain.ProviderModel, len(models))
	copy(out, models)
	return out
}
