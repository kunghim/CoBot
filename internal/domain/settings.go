package domain

import (
	"fmt"
	"strings"
)

// SupportedLanguages are the UI locales of the first version.
var SupportedLanguages = []string{"zh-CN", "en"}

// ColorModes choose light or dark; system follows the operating system.
var ColorModes = []string{"system", "light", "dark"}

// Themes are the interface's palettes; classic is the default for new profiles.
var Themes = []string{"graphite", "harbor", "dusk", "classic"}

// BudgetScopes select which entity a token budget applies to.
const (
	BudgetScopeNone = ""
	BudgetScopeBot  = "bot"
	BudgetScopeTeam = "team"
)

// Settings is the single business-settings row of the profile database.
// Defaults match the product decisions (member limit 6, chain limit 10,
// review reject limit 3, no budget, zh-CN).
type Settings struct {
	Revision     int64  `json:"revision"`
	Language     string `json:"language"`
	ColorMode    string `json:"colorMode"`
	Theme        string `json:"theme"`
	DefaultModel string `json:"defaultModel"`
	// DefaultProvider is the model provider the profile runs with. Empty
	// means the engine's own official provider, which is what a fresh
	// profile uses.
	//
	// The model actually used is resolved in this order:
	//
	//	team.model > provider.DefaultModel > settings.defaultModel > engine default
	//
	// Settings.DefaultModel is the last CoBot-level fallback; it is not
	// yet scoped to the selected provider, because the provider UI that
	// would scope it arrives with the same change that starts reading
	// this field.
	DefaultProvider string `json:"defaultProvider"`
	MemberLimit     int    `json:"memberLimit"`
	ChainLimit      int    `json:"chainLimit"`
	// ReviewRejectLimit is how often a reviewer may send one task back
	// before the leader decides.
	ReviewRejectLimit int    `json:"reviewRejectLimit"`
	BudgetScope       string `json:"budgetScope"`
	BudgetTokens      int64  `json:"budgetTokens"`
	EnginePath        string `json:"enginePath"`
}

// DefaultSettings returns first-run values; model/engine fields stay empty
// until the engine is configured — the product never fabricates them.
func DefaultSettings() Settings {
	return Settings{
		Revision:          1,
		Language:          "zh-CN",
		MemberLimit:       6,
		ChainLimit:        10,
		ReviewRejectLimit: 3,
		ColorMode:         "system",
		Theme:             "classic",
		BudgetScope:       BudgetScopeNone,
	}
}

// Validate checks ranges that the API and MCP tools must enforce.
func (s Settings) Validate() error {
	if !isSupportedLanguage(s.Language) {
		return InvalidRequest("error.settings.unsupported_language", map[string]any{"language": s.Language})
	}
	if !oneOf(ColorModes, s.ColorMode) {
		return InvalidRequest("error.settings.unsupported_color_mode", map[string]any{"colorMode": s.ColorMode})
	}
	if !oneOf(Themes, s.Theme) {
		return InvalidRequest("error.settings.unsupported_theme", map[string]any{"theme": s.Theme})
	}
	if s.MemberLimit < 1 || s.MemberLimit > 20 {
		return InvalidRequest("error.settings.member_limit_range", map[string]any{"min": 1, "max": 20})
	}
	if s.ChainLimit < 1 || s.ChainLimit > 100 {
		return InvalidRequest("error.settings.chain_limit_range", map[string]any{"min": 1, "max": 100})
	}
	if s.ReviewRejectLimit < 1 || s.ReviewRejectLimit > 20 {
		return InvalidRequest("error.settings.review_reject_limit_range", map[string]any{"min": 1, "max": 20})
	}
	switch s.BudgetScope {
	case BudgetScopeNone:
		if s.BudgetTokens != 0 {
			return InvalidRequest("error.settings.budget_scope_required", nil)
		}
	case BudgetScopeBot, BudgetScopeTeam:
		if s.BudgetTokens <= 0 {
			return InvalidRequest("error.settings.budget_tokens_positive", nil)
		}
	default:
		return InvalidRequest("error.settings.budget_scope_invalid", map[string]any{"scope": s.BudgetScope})
	}
	return nil
}

func isSupportedLanguage(lang string) bool {
	return oneOf(SupportedLanguages, lang)
}

func oneOf(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// SettingsPatch carries optional fields of PATCH /settings. Nil pointers mean
// "leave unchanged"; empty strings are rejected instead of silently clearing
// a value the caller forgot to send.
type SettingsPatch struct {
	ColorMode         *string `json:"colorMode"`
	Theme             *string `json:"theme"`
	Language          *string `json:"language"`
	DefaultModel      *string `json:"defaultModel"`
	DefaultProvider   *string `json:"defaultProvider"`
	MemberLimit       *int    `json:"memberLimit"`
	ChainLimit        *int    `json:"chainLimit"`
	ReviewRejectLimit *int    `json:"reviewRejectLimit"`
	BudgetScope       *string `json:"budgetScope"`
	BudgetTokens      *int64  `json:"budgetTokens"`
	EnginePath        *string `json:"enginePath"`
}

// Apply returns the settings value after the patch, without persisting.
func (s Settings) Apply(p SettingsPatch) Settings {
	out := s
	if p.Language != nil {
		out.Language = strings.TrimSpace(*p.Language)
	}
	if p.ColorMode != nil {
		out.ColorMode = strings.TrimSpace(*p.ColorMode)
	}
	if p.Theme != nil {
		out.Theme = strings.TrimSpace(*p.Theme)
	}
	if p.DefaultModel != nil {
		out.DefaultModel = strings.TrimSpace(*p.DefaultModel)
	}
	if p.DefaultProvider != nil {
		out.DefaultProvider = strings.TrimSpace(*p.DefaultProvider)
	}
	if p.MemberLimit != nil {
		out.MemberLimit = *p.MemberLimit
	}
	if p.ChainLimit != nil {
		out.ChainLimit = *p.ChainLimit
	}
	if p.ReviewRejectLimit != nil {
		out.ReviewRejectLimit = *p.ReviewRejectLimit
	}
	if p.BudgetScope != nil {
		out.BudgetScope = strings.TrimSpace(*p.BudgetScope)
	}
	if p.BudgetTokens != nil {
		out.BudgetTokens = *p.BudgetTokens
	}
	if p.EnginePath != nil {
		out.EnginePath = strings.TrimSpace(*p.EnginePath)
	}
	return out
}

// String renders compactly for logs (never contains secrets).
func (s Settings) String() string {
	return fmt.Sprintf("Settings{rev=%d lang=%s member=%d chain=%d}", s.Revision, s.Language, s.MemberLimit, s.ChainLimit)
}
