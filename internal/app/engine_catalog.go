package app

import (
	"context"
	"sort"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// clientModels are the third-party models the engine has to know: every model
// of every provider the user configured, as the model catalog and the user's
// entries describe it.
func (a *App) clientModels(ctx context.Context) []codex.ClientModel {
	providers, err := sqlite.ListProviders(ctx, a.DB)
	if err != nil {
		return nil
	}
	best := map[string]codex.ClientModel{}
	for _, p := range providers {
		if p.AuthMode == domain.ProviderAuthChatGPT {
			continue
		}
		for _, model := range p.Models {
			spec := a.resolveModel(model)
			if spec.ContextWindow <= 0 {
				continue
			}
			entry := codex.ClientModel{
				Slug:          model.ID,
				Name:          spec.DisplayName,
				ContextWindow: spec.ContextWindow,
				Input:         spec.InputModalities,
				Toggle:        spec.Thinking.Toggle,
				Efforts:       spec.Thinking.Efforts,
				DefaultEffort: spec.Thinking.DefaultEffort,
			}
			// One slug, two providers: the engine keeps one entry, and the smaller
			// window is the one that is safe for both.
			if have, ok := best[model.ID]; !ok || entry.ContextWindow < have.ContextWindow {
				best[model.ID] = entry
			}
		}
	}
	out := make([]codex.ClientModel, 0, len(best))
	for _, entry := range best {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// EngineCatalog is the model catalog an engine is started with now. The empty
// one means none is needed; an error means it could not be made, and the
// engine then starts without it (the models keep the engine's own fallback
// window).
func (a *App) EngineCatalog(ctx context.Context) (codex.ClientCatalog, error) {
	if a.ClientCatalogs == nil {
		return codex.ClientCatalog{}, nil
	}
	settings, err := a.GetSettings(ctx)
	if err != nil {
		return codex.ClientCatalog{}, err
	}
	binary, err := codex.Locate(settings.EnginePath)
	if err != nil {
		return codex.ClientCatalog{}, err
	}
	catalog, _, err := a.ClientCatalogs.Build(ctx, binary, codex.EngineEnv(a.Paths, nil), a.clientModels(ctx))
	return catalog, err
}

// EngineCatalogState says how the running engines stand against the catalog now wanted.
type EngineCatalogState struct {
	// Active is whether a catalog is wanted at all.
	Active bool `json:"active"`
	// Stale is the number of running team engines started without the
	// catalog now wanted.
	Stale int `json:"stale"`
}

func (a *App) engineCatalogState(ctx context.Context) EngineCatalogState {
	catalog, err := a.EngineCatalog(ctx)
	if err != nil && a.Logger != nil {
		a.Logger.Warn("model catalog for the engine unavailable", "error", err)
	}
	state := EngineCatalogState{Active: catalog.Path != ""}
	if a.Scheduler != nil {
		state.Stale = a.Scheduler.StaleEngines(catalog.Fingerprint)
	}
	return state
}

// ApplyEngineCatalogResult is the outcome of restarting engines that run an older catalog.
type ApplyEngineCatalogResult struct {
	// Restarted engines were stopped and start again with the next turn.
	Restarted int `json:"restarted"`
	// Busy engines have a turn running and are left; they take the catalog
	// when they next start.
	Busy int `json:"busy"`
}

// ApplyEngineCatalog stops the idle team engines that run an older model
// catalog, so their next turn starts with the one now wanted.
func (a *App) ApplyEngineCatalog(ctx context.Context, principal auth.Principal) (ApplyEngineCatalogResult, error) {
	if principal.Kind != auth.KindUser {
		return ApplyEngineCatalogResult{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if a.Scheduler == nil {
		return ApplyEngineCatalogResult{}, nil
	}
	catalog, err := a.EngineCatalog(ctx)
	if err != nil {
		catalog = codex.ClientCatalog{}
	}
	stopped, busy := a.Scheduler.RestartStaleEngines(ctx, catalog.Fingerprint)
	return ApplyEngineCatalogResult{Restarted: stopped, Busy: busy}, nil
}
