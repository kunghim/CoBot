package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// officialEngineProvider is the engine's name for its own account. A thread
// on the official provider names it outright: the engine's configured
// default is not a promise, and the engine reports this name back for such
// a thread, so naming it is also what keeps "is this thread switching
// provider?" from being true on every turn.
const officialEngineProvider = "openai"

// ModelChoice is the model a member runs with and where it came from.
type ModelChoice struct {
	// ProviderID is CoBot's provider id; empty is the engine's own account
	// with no provider row behind it.
	ProviderID string
	// EngineProvider is the provider id the engine is told for the thread.
	EngineProvider string
	Model          string
}

// MemberModel resolves the model a member runs with.
//
// The member's own choice wins, then the team's provider (kept for teams
// that named one before members could choose), then the default model the
// settings name, then the first enabled provider that can run a turn. A disabled or deleted
// provider is skipped wherever it is named, so a member that ran on it moves
// to the next choice rather than failing to start.
func (a *App) MemberModel(ctx context.Context, teamID, botID string) (ModelChoice, error) {
	settings, err := a.GetSettings(ctx)
	if err != nil {
		return ModelChoice{}, err
	}
	providers, err := sqlite.ListProviders(ctx, a.DB)
	if err != nil {
		return ModelChoice{}, err
	}
	byID := make(map[string]domain.Provider, len(providers))
	for _, p := range providers {
		byID[p.ID] = p
	}

	type candidate struct{ provider, model string }
	var candidates []candidate
	if botID != "" {
		bot, err := sqlite.GetBot(ctx, a.DB, botID)
		if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
			return ModelChoice{}, err
		}
		if bot.ProviderID != "" {
			candidates = append(candidates, candidate{bot.ProviderID, bot.Model})
		}
	}
	if teamID != "" {
		team, err := sqlite.GetTeamByID(ctx, a.DB, teamID)
		if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
			return ModelChoice{}, err
		}
		if team.ProviderID != "" {
			candidates = append(candidates, candidate{team.ProviderID, ""})
		}
	}
	if settings.DefaultProvider != "" {
		candidates = append(candidates, candidate{settings.DefaultProvider, settings.DefaultModel})
	}
	// The default model names a model of the default provider: it is no
	// fallback for another provider, whose models it is not. A default
	// with no provider is the older form, a model of the engine's own.
	fallback := func(p domain.Provider) string {
		if settings.DefaultProvider == "" || settings.DefaultProvider == p.ID {
			return settings.DefaultModel
		}
		return ""
	}
	for _, c := range candidates {
		if p, ok := byID[c.provider]; ok && p.Enabled {
			return a.namedModel(ctx, choiceFor(p, c.model, fallback(p))), nil
		}
	}
	// With nothing named, the first enabled provider that can run a turn:
	// the ChatGPT account only once it is signed in.
	signedIn := a.EngineStatusCached().LoggedIn
	for _, p := range providers {
		if p.Enabled && (p.AuthMode != domain.ProviderAuthChatGPT || signedIn) {
			return a.namedModel(ctx, choiceFor(p, "", fallback(p))), nil
		}
	}
	// Nothing is configured or everything is off: the engine's own account
	// is what is left, and the core does not refuse to run on it.
	return a.namedModel(ctx, ModelChoice{EngineProvider: officialEngineProvider, Model: fallback(domain.Provider{ID: officialEngineProvider})}), nil
}

// namedModel fills in the engine's own default model when a choice on the
// ChatGPT account names none. A turn that asks for no model keeps whatever
// model its thread last ran with, so a member moved back to "the default"
// from a model of its own would otherwise go on running that model.
func (a *App) namedModel(ctx context.Context, choice ModelChoice) ModelChoice {
	if choice.Model == "" && choice.EngineProvider == officialEngineProvider {
		choice.Model = a.engineDefaultModel(ctx)
	}
	return choice
}

// engineDefaultModel is the model the engine marks as its default, cached
// for a few minutes; empty when the engine cannot say.
func (a *App) engineDefaultModel(ctx context.Context) string {
	a.engineDefaultMu.Lock()
	defer a.engineDefaultMu.Unlock()
	if time.Now().Before(a.engineDefaultUntil) {
		return a.engineDefault
	}
	// An answer is kept for a few minutes. Not knowing is kept too, for
	// less: every turn would otherwise ask an engine that cannot say, one
	// after the other behind this lock.
	raw, err := a.ModelList(ctx)
	id := ""
	if err == nil {
		id = defaultModelIn(raw)
	}
	keep := 5 * time.Minute
	if id == "" {
		keep = 30 * time.Second
	}
	a.engineDefault, a.engineDefaultUntil = id, time.Now().Add(keep)
	return id
}

// defaultModelIn picks the default model out of a sanitized model list: the
// one marked as default, else the first.
func defaultModelIn(raw json.RawMessage) string {
	var list struct {
		Models []struct {
			Slug      string `json:"slug"`
			ID        string `json:"id"`
			IsDefault bool   `json:"isDefault"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &list) != nil || len(list.Models) == 0 {
		return ""
	}
	pick := list.Models[0]
	for _, m := range list.Models {
		if m.IsDefault {
			pick = m
			break
		}
	}
	if pick.Slug != "" {
		return pick.Slug
	}
	return pick.ID
}

// turnModel is the model a turn asks the team's own engine for. A choice on
// the ChatGPT account that still names none takes the engine's default from
// that engine: a turn without a model keeps the one its thread last used.
func turnModel(ctx context.Context, choice ModelChoice, server interface {
	ModelList(context.Context) (json.RawMessage, error)
}) string {
	if choice.Model != "" || choice.EngineProvider != officialEngineProvider {
		return choice.Model
	}
	raw, err := server.ModelList(ctx)
	if err != nil {
		return ""
	}
	return defaultModelIn(sanitizeModels(raw))
}

// choiceFor names the model a provider serves for a requested id: the
// requested one when the provider has it, else the provider's own default,
// else its first model. The official provider's models are the engine's own
// list, which CoBot does not keep, so a requested id is taken as it is.
func choiceFor(p domain.Provider, requested, fallback string) ModelChoice {
	if p.AuthMode == domain.ProviderAuthChatGPT {
		model := requested
		if model == "" {
			model = p.DefaultModel
		}
		if model == "" {
			model = fallback
		}
		return ModelChoice{ProviderID: p.ID, EngineProvider: officialEngineProvider, Model: model}
	}
	model := ""
	if requested != "" && providerHasModel(p, requested) {
		model = requested
	}
	if model == "" && p.DefaultModel != "" && providerHasModel(p, p.DefaultModel) {
		model = p.DefaultModel
	}
	if model == "" {
		for _, m := range p.Models {
			if m.Status != domain.ModelStatusFailed {
				model = m.ID
				break
			}
		}
	}
	if model == "" && len(p.Models) > 0 {
		model = p.Models[0].ID
	}
	return ModelChoice{ProviderID: p.ID, EngineProvider: p.ID, Model: model}
}

func providerHasModel(p domain.Provider, id string) bool {
	for _, m := range p.Models {
		if m.ID == id {
			return true
		}
	}
	return false
}

// SetBotModel sets the model a member runs with. An empty provider puts the
// member back on the default model the settings name.
//
// The change applies from the member's next turn. Another vendor's model
// starts a new thread handed over from the old one (see needsFreshThread);
// another model of the same vendor keeps the thread.
func (a *App) SetBotModel(ctx context.Context, principal auth.Principal, botID, providerID, model string, expectedRevision int64) (domain.Bot, error) {
	cached, unlock, writeErr := beginWrite[domain.Bot](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Bot{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindUser {
		return domain.Bot{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if providerID == "" {
		model = ""
	} else if err := a.requireSelectableModel(ctx, providerID, model); err != nil {
		return domain.Bot{}, err
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Bot{}, err
	}
	defer tx.Rollback()
	current, err := sqlite.GetBot(ctx, tx, botID)
	if err != nil {
		return domain.Bot{}, sqlite.NormalizeError(err)
	}
	if expectedRevision > 0 && current.Revision != expectedRevision {
		return domain.Bot{}, domain.RevisionConflict("error.bot.revision_conflict", map[string]any{"expected": expectedRevision, "actual": current.Revision})
	}
	if err := sqlite.SetBotModel(ctx, tx, botID, providerID, model); err != nil {
		return domain.Bot{}, sqlite.NormalizeError(err)
	}
	bot, err := sqlite.GetBot(ctx, tx, botID)
	if err != nil {
		return domain.Bot{}, sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, principal, bot); err != nil {
		return domain.Bot{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Bot{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventBotUpdated, TeamID: bot.TeamID, BotID: bot.ID, ResourceID: bot.ID, Revision: bot.Revision})
	a.Hub.Publish()
	return bot, nil
}

// requireSelectableModel refuses a model a member cannot run with: one of a
// provider that is gone or disabled, or one the provider does not keep.
func (a *App) requireSelectableModel(ctx context.Context, providerID, model string) error {
	providers, err := sqlite.ListProviders(ctx, a.DB)
	if err != nil {
		return err
	}
	var p domain.Provider
	found := false
	for _, entry := range providers {
		if entry.ID == providerID {
			p, found = entry, true
			break
		}
	}
	if !found {
		return domain.InvalidRequest("error.provider.not_found", map[string]any{"id": providerID})
	}
	if !p.Enabled {
		return domain.InvalidRequest("error.provider.disabled", map[string]any{"id": providerID})
	}
	if model == "" {
		return domain.InvalidRequest("error.provider.model_required", nil)
	}
	if p.AuthMode != domain.ProviderAuthChatGPT && !providerHasModel(p, model) {
		return domain.InvalidRequest("error.provider.model_not_kept", map[string]any{"id": providerID, "model": model})
	}
	return nil
}

// SetProviderEnabled switches whether members may pick a provider's models.
// Members that ran on a provider that is switched off move to the next
// choice from their next turn.
func (a *App) SetProviderEnabled(ctx context.Context, id string, enabled bool, expectedRevision int64) (ProviderView, error) {
	saved, err := sqlite.SetProviderEnabled(ctx, a.DB, id, enabled, expectedRevision)
	if err != nil {
		return ProviderView{}, sqlite.NormalizeError(err)
	}
	a.Hub.Publish()
	return a.providerView(ctx, saved), nil
}
