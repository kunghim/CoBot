package app

import (
	"context"
	"errors"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/browser"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// browserTeam checks that the user may look at a team's browser.
func (a *App) browserTeam(ctx context.Context, principal auth.Principal, teamID string) error {
	if principal.Kind != auth.KindUser {
		return domain.Forbidden("error.forbidden.user_only", nil)
	}
	if _, _, err := sqlite.GetTeam(ctx, a.DB, teamID); err != nil {
		return sqlite.NormalizeError(err)
	}
	return nil
}

// BrowserPanel lists a team's open browser tabs for the live view. It never
// starts a browser: a team that has not used one has none to show.
func (a *App) BrowserPanel(ctx context.Context, principal auth.Principal, teamID string) (map[string]any, error) {
	if err := a.browserTeam(ctx, principal, teamID); err != nil {
		return nil, err
	}
	out := map[string]any{"available": false, "running": false, "tabs": []browser.PanelTab{}}
	if a.Browser == nil {
		return out, nil
	}
	_, err := browser.Find()
	out["available"] = err == nil
	if running, tabs := a.Browser.Panel(ctx, teamID); running {
		out["running"] = true
		if tabs != nil {
			out["tabs"] = tabs
		}
	}
	return out, nil
}

// BrowserWatch streams one tab's pictures to send until ctx ends.
func (a *App) BrowserWatch(ctx context.Context, principal auth.Principal, teamID, tabID string, send func(browser.Frame)) error {
	if err := a.browserTeam(ctx, principal, teamID); err != nil {
		return err
	}
	if a.Browser == nil {
		return domain.CapabilityMissing("error.browser.unavailable", nil, nil)
	}
	return browserPanelError(a.Browser.Watch(ctx, teamID, tabID, send))
}

// BrowserInput applies what the user did in the live view.
func (a *App) BrowserInput(ctx context.Context, principal auth.Principal, teamID, tabID string, in browser.Input) error {
	if err := a.browserTeam(ctx, principal, teamID); err != nil {
		return err
	}
	if a.Browser == nil {
		return domain.CapabilityMissing("error.browser.unavailable", nil, nil)
	}
	return browserPanelError(a.Browser.Input(ctx, teamID, tabID, in))
}

func browserPanelError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, browser.ErrBadRequest):
		return domain.InvalidRequest("error.browser.bad_request", map[string]any{"reason": err.Error()})
	default:
		return domain.EngineUnavailable("error.browser.failed", map[string]any{"reason": err.Error()}, err)
	}
}
