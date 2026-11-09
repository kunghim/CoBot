package app

import (
	"context"
	"sort"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// clientModels are the third-party models the engine has to know: every model
// of every provider the user configured, as the model catalog and the user's
// entries describe it.
func (a *App) clientModels(ctx context.Context) []engine.ClientModel {
	providers, err := sqlite.ListProviders(ctx, a.DB)
	if err != nil {
		return nil
	}
	best := map[string]engine.ClientModel{}
	for _, p := range providers {
		if p.AuthMode == domain.ProviderAuthChatGPT {
			continue
		}
		for _, model := range p.Models {
			spec := a.resolveModel(model)
			if spec.ContextWindow <= 0 {
				continue
			}
			entry := engine.ClientModel{
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
	out := make([]engine.ClientModel, 0, len(best))
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
func (a *App) EngineCatalog(ctx context.Context) (engine.Catalog, error) {
	if a.Install == nil {
		return engine.Catalog{}, nil
	}
	settings, err := a.GetSettings(ctx)
	if err != nil {
		return engine.Catalog{}, err
	}
	binary, err := a.Install.Locate(settings.EnginePath)
	if err != nil {
		return engine.Catalog{}, err
	}
	return a.Install.BuildCatalog(ctx, binary, a.clientModels(ctx))
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
		catalog = engine.Catalog{}
	}
	stopped, busy := a.Scheduler.RestartStaleEngines(ctx, catalog.Fingerprint)
	return ApplyEngineCatalogResult{Restarted: stopped, Busy: busy}, nil
}

// transportCarrier is an engine that knows whether it was started HTTP-only.
type transportCarrier interface{ HTTPOnly() bool }

// engineTransportStale reports whether an engine runs the official account
// another way than the settings now ask for. An engine that cannot say, or a
// profile whose settings cannot be read, is left alone.
func (a *App) engineTransportStale(ctx context.Context, server engine.Server) bool {
	carrier, ok := server.(transportCarrier)
	if !ok {
		return false
	}
	settings, err := a.GetSettings(ctx)
	if err != nil {
		return false
	}
	return carrier.HTTPOnly() != settings.HTTPOnly()
}
