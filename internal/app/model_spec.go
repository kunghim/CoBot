package app

import (
	"context"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/modelcatalog"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// The one field a model's catalog entry locks (see domain.ModelSpec.Locked):
// the others only have limits.
const lockedThinking = "thinking"

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

	// The catalog's limits are the most a model can do; the user may ask for
	// less (a smaller context, a shorter output, fewer kinds of input) but
	// never for more. Nothing entered means the catalog's value.
	spec.ContextLimit, spec.ContextWindow = known0(known, entry.Context), cappedBy(model.ContextWindow, known0(known, entry.Context))
	spec.MaxOutputLimit, spec.MaxOutput = known0(known, entry.Output), cappedBy(model.MaxOutput, known0(known, entry.Output))
	spec.InputOptions = allInputKinds
	spec.InputModalities = withText(model.InputModalities)
	if known && len(entry.Input) > 0 {
		spec.InputOptions = withText(entry.Input)
		spec.InputModalities = spec.InputOptions
		if len(model.InputModalities) > 0 {
			spec.InputModalities = intersect(withText(model.InputModalities), spec.InputOptions)
		}
	}

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

// allInputKinds are the kinds of input a model nothing is known about may be
// said to take.
var allInputKinds = []string{"text", "image", "video", "pdf"}

// known0 is the catalog's number when the model is in it, else 0 (no limit).
func known0(known bool, value int) int {
	if known && value > 0 {
		return value
	}
	return 0
}

// cappedBy is the user's number when it is set and within the limit, else
// the limit itself (0 when there is neither).
func cappedBy(entered, limit int) int {
	if entered <= 0 || (limit > 0 && entered > limit) {
		return limit
	}
	return entered
}

// intersect is the kinds of input in both lists, in the order of the first.
func intersect(list, allowed []string) []string {
	out := make([]string, 0, len(list))
	for _, kind := range list {
		if contains(allowed, kind) {
			out = append(out, kind)
		}
	}
	return out
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
// computed spec dropped, and what the catalog already says taken out, so
// nothing stale is left behind to reappear if the catalog stops knowing the
// model. What the user asked for below a limit is kept; a value at or above
// the limit is the catalog's own and is not stored.
func (a *App) clearedOfWhatTheCatalogGives(models []domain.ProviderModel) []domain.ProviderModel {
	out := make([]domain.ProviderModel, len(models))
	for i, model := range models {
		spec := a.resolveModel(model)
		model.Spec = nil
		// The catalog's own name is what a model is shown as without an alias;
		// keeping it as an alias would only freeze it.
		if spec.Name != "" && model.DisplayName == spec.Name {
			model.DisplayName = ""
		}
		if spec.ContextLimit > 0 && model.ContextWindow >= spec.ContextLimit {
			model.ContextWindow = 0
		}
		if spec.MaxOutputLimit > 0 && model.MaxOutput >= spec.MaxOutputLimit {
			model.MaxOutput = 0
		}
		if spec.Source == "catalog" && len(model.InputModalities) > 0 && sameKinds(spec.InputModalities, spec.InputOptions) {
			model.InputModalities = nil
		}
		for _, field := range spec.Locked {
			if field == lockedThinking {
				model.SupportedEfforts, model.ThinkingToggle, model.DefaultEffort = nil, false, ""
			}
		}
		out[i] = model
	}
	return out
}

// sameKinds says whether two lists hold the same kinds of input.
func sameKinds(a, b []string) bool {
	return len(a) == len(b) && len(intersect(a, b)) == len(a)
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
