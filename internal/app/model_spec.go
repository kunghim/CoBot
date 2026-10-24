package app

import (
	"context"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/modelcatalog"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Field names a model's catalog entry can lock (see domain.ModelSpec.Locked).
const (
	lockedContext  = "contextWindow"
	lockedOutput   = "maxOutput"
	lockedInput    = "inputModalities"
	lockedThinking = "thinking"
)

// resolveModel puts together what is known about a model: the model catalog is
// trusted where it has a word, and the user's own entries stand where it has
// none (or does not know the model at all).
func (a *App) resolveModel(model domain.ProviderModel) domain.ModelSpec {
	entry, known := a.ModelCatalog.Match(model.ID)
	spec := domain.ModelSpec{Alias: model.DisplayName, WebSearch: model.WebSearch, Source: "custom", Locked: []string{}}
	if known {
		spec.Source, spec.Name, spec.CatalogID = "catalog", entry.Name, entry.ID
	}
	spec.DisplayName = firstNonEmpty(model.DisplayName, spec.Name, model.ID)

	spec.ContextWindow = model.ContextWindow
	if known && entry.Context > 0 {
		spec.ContextWindow = entry.Context
		spec.Locked = append(spec.Locked, lockedContext)
	}
	spec.MaxOutput = model.MaxOutput
	if known && entry.Output > 0 {
		spec.MaxOutput = entry.Output
		spec.Locked = append(spec.Locked, lockedOutput)
	}
	input := model.InputModalities
	if known && len(entry.Input) > 0 {
		input = entry.Input
		spec.Locked = append(spec.Locked, lockedInput)
	}
	spec.InputModalities = withText(input)

	switch {
	case known && entry.Thinking != nil:
		spec.Thinking = domain.SpecThinking{Toggle: entry.Thinking.Toggle, Efforts: append([]string{}, entry.Thinking.Efforts...)}
		spec.Locked = append(spec.Locked, lockedThinking)
	case len(model.SupportedEfforts) > 0 || model.ThinkingToggle:
		spec.Thinking = domain.SpecThinking{Toggle: model.ThinkingToggle, Efforts: append([]string{}, model.SupportedEfforts...)}
	default:
		// A model nobody has said anything about: the three levels every
		// vendor that grades thinking at all has some form of.
		spec.Thinking = domain.SpecThinking{Efforts: append([]string{}, domain.DefaultEfforts...)}
	}
	if contains(spec.Thinking.Efforts, model.DefaultEffort) {
		spec.Thinking.DefaultEffort = model.DefaultEffort
	}
	return spec
}

// withText lists what a model takes with text first: every model takes text.
func withText(kinds []string) []string {
	out := []string{"text"}
	for _, kind := range kinds {
		if kind != "text" && !contains(out, kind) {
			out = append(out, kind)
		}
	}
	return out
}

func contains(list []string, value string) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// withSpecs is the provider's models with what is known about each filled in,
// for showing; the provider itself is untouched.
func (a *App) withSpecs(p domain.Provider) domain.Provider {
	if p.AuthMode == domain.ProviderAuthChatGPT || len(p.Models) == 0 {
		return p
	}
	models := make([]domain.ProviderModel, len(p.Models))
	for i, model := range p.Models {
		spec := a.resolveModel(model)
		model.Spec = &spec
		models[i] = model
	}
	p.Models = models
	return p
}

// clearedOfWhatTheCatalogGives is the provider's models ready to store: the
// computed spec dropped, and the entries the catalog already answers for taken
// out, so nothing stale is left behind to reappear if the catalog stops
// knowing the model.
func (a *App) clearedOfWhatTheCatalogGives(models []domain.ProviderModel) []domain.ProviderModel {
	out := make([]domain.ProviderModel, len(models))
	for i, model := range models {
		spec := a.resolveModel(model)
		model.Spec = nil
		for _, field := range spec.Locked {
			switch field {
			case lockedContext:
				model.ContextWindow = 0
			case lockedOutput:
				model.MaxOutput = 0
			case lockedInput:
				model.InputModalities = nil
			case lockedThinking:
				model.SupportedEfforts, model.ThinkingToggle, model.DefaultEffort = nil, false, ""
			}
		}
		out[i] = model
	}
	return out
}

// specOfChoice is what is known about the model a member runs with, when it
// belongs to a provider the user configured; the engine's own account is the
// engine's to describe.
func (a *App) specOfChoice(ctx context.Context, choice ModelChoice, model string) (domain.ModelSpec, bool) {
	if choice.ProviderID == "" {
		return domain.ModelSpec{}, false
	}
	provider, err := sqlite.GetProvider(ctx, a.DB, choice.ProviderID)
	if err != nil || provider.AuthMode == domain.ProviderAuthChatGPT {
		return domain.ModelSpec{}, false
	}
	if model == "" {
		model = choice.Model
	}
	for _, entry := range provider.Models {
		if entry.ID == model {
			return a.resolveModel(entry), true
		}
	}
	return domain.ModelSpec{}, false
}

// ModelCatalogView is the model catalog in use, and how the running engines stand against it.
type ModelCatalogView struct {
	modelcatalog.Status
	Engine EngineCatalogState `json:"engine"`
}

// ModelCatalogStatus says which model catalog is in use.
func (a *App) ModelCatalogStatus(ctx context.Context) ModelCatalogView {
	view := ModelCatalogView{Engine: a.engineCatalogState(ctx)}
	if a.ModelCatalog != nil {
		view.Status = a.ModelCatalog.Status()
	}
	return view
}

// ModelCatalogUpdate is the outcome of asking for a newer catalog.
type ModelCatalogUpdate struct {
	// State is updated, unreachable or rejected.
	State string `json:"state"`
	modelcatalog.Status
	Engine EngineCatalogState `json:"engine"`
}

// UpdateModelCatalog downloads the newest catalog now. It is the user pressing
// a button; a failure is reported in State and leaves the catalog in use as it
// was.
func (a *App) UpdateModelCatalog(ctx context.Context, principal auth.Principal) (ModelCatalogUpdate, error) {
	if principal.Kind != auth.KindUser {
		return ModelCatalogUpdate{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if a.ModelCatalog == nil {
		return ModelCatalogUpdate{State: string(modelcatalog.Unreachable)}, nil
	}
	status, err := a.ModelCatalog.Update(ctx, time.Now())
	if kind, failed := modelcatalog.IsUpdateError(err); failed {
		return ModelCatalogUpdate{State: string(kind), Status: status}, nil
	}
	if err != nil {
		return ModelCatalogUpdate{}, domain.Internal(err)
	}
	a.Hub.Publish()
	return ModelCatalogUpdate{State: "updated", Status: status, Engine: a.engineCatalogState(ctx)}, nil
}

// webSearchAllowed says whether the user has switched web search on for the
// model a member runs with.
func (a *App) webSearchAllowed(ctx context.Context, choice ModelChoice) bool {
	spec, ok := a.specOfChoice(ctx, choice, "")
	return ok && spec.WebSearch
}

// ResolveModels is what is known about each of the given models, by id.
func (a *App) ResolveModels(models []domain.ProviderModel) map[string]domain.ModelSpec {
	out := make(map[string]domain.ModelSpec, len(models))
	for _, model := range models {
		out[model.ID] = a.resolveModel(model)
	}
	return out
}
