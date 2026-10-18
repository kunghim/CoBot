package domain

import (
	"fmt"
	"strings"
)

// SupportedLanguages are the UI locales of the first version.
var SupportedLanguages = []string{"zh-CN", "en"}

// BudgetScopes select which entity a token budget applies to.
const (
	BudgetScopeNone = ""
	BudgetScopeBot  = "bot"
	BudgetScopeTeam = "team"
)

// Settings is the single business-settings row of the profile database.
// Defaults match the product decisions (member limit 6, chain limit 10, no
// budget, zh-CN).
type Settings struct {
	Revision     int64  `json:"revision"`
	Language     string `json:"language"`
	DefaultModel string `json:"defaultModel"`
	MemberLimit  int    `json:"memberLimit"`
	ChainLimit   int    `json:"chainLimit"`
	BudgetScope  string `json:"budgetScope"`
	BudgetTokens int64  `json:"budgetTokens"`
	EnginePath   string `json:"enginePath"`
}

// DefaultSettings returns first-run values; model/engine fields stay empty
// until the engine is configured — the product never fabricates them.
func DefaultSettings() Settings {
	return Settings{
		Revision:    1,
		Language:    "zh-CN",
		MemberLimit: 6,
		ChainLimit:  10,
		BudgetScope: BudgetScopeNone,
	}
}

// Validate checks ranges that the API and MCP tools must enforce.
func (s Settings) Validate() error {
	if !isSupportedLanguage(s.Language) {
		return InvalidRequest("error.settings.unsupported_language", map[string]any{"language": s.Language})
	}
	if s.MemberLimit < 1 || s.MemberLimit > 20 {
		return InvalidRequest("error.settings.member_limit_range", map[string]any{"min": 1, "max": 20})
	}
	if s.ChainLimit < 1 || s.ChainLimit > 100 {
		return InvalidRequest("error.settings.chain_limit_range", map[string]any{"min": 1, "max": 100})
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
	for _, l := range SupportedLanguages {
		if l == lang {
			return true
		}
	}
	return false
}

// SettingsPatch carries optional fields of PATCH /settings. Nil pointers mean
// "leave unchanged"; empty strings are rejected instead of silently clearing
// a value the caller forgot to send.
type SettingsPatch struct {
	Language     *string `json:"language"`
	DefaultModel *string `json:"defaultModel"`
	MemberLimit  *int    `json:"memberLimit"`
	ChainLimit   *int    `json:"chainLimit"`
	BudgetScope  *string `json:"budgetScope"`
	BudgetTokens *int64  `json:"budgetTokens"`
	EnginePath   *string `json:"enginePath"`
}

// Apply returns the settings value after the patch, without persisting.
func (s Settings) Apply(p SettingsPatch) Settings {
	out := s
	if p.Language != nil {
		out.Language = strings.TrimSpace(*p.Language)
	}
	if p.DefaultModel != nil {
		out.DefaultModel = strings.TrimSpace(*p.DefaultModel)
	}
	if p.MemberLimit != nil {
		out.MemberLimit = *p.MemberLimit
	}
	if p.ChainLimit != nil {
		out.ChainLimit = *p.ChainLimit
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
