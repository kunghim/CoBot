package browser

import (
	"context"
	"fmt"
)

// Viewport is a tab's real window size as the page renders it, set by the
// user or by a member's viewport step. The last setter wins. Nil (no
// override) means the browser's default window.
type Viewport struct {
	Width  int  `json:"width"`
	Height int  `json:"height"`
	Mobile bool `json:"mobile,omitempty"`
}

// viewportPresets are the fixed sizes behind the live view's dropdown.
// The two scaling modes (fit the workspace, show at 100%) change nothing
// here: they only scale the same picture in the page.
var viewportPresets = map[string]Viewport{
	"desktop": {Width: 1280, Height: 744},
	"tablet":  {Width: 768, Height: 1024},
	"phone":   {Width: 390, Height: 844, Mobile: true},
}

// ViewportRequest is one set-viewport call: a preset (desktop, tablet,
// phone or clear) or an explicit size.
type ViewportRequest struct {
	Preset string `json:"preset,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Mobile bool   `json:"mobile,omitempty"`
}

// resolveViewport turns a request into the viewport to apply; nil clears the
// override back to the browser's default window.
func resolveViewport(req ViewportRequest) (*Viewport, error) {
	switch {
	case req.Preset == "clear":
		return nil, nil
	case req.Preset != "":
		vp, ok := viewportPresets[req.Preset]
		if !ok {
			return nil, fmt.Errorf("%w: unknown viewport preset %q (use desktop, tablet, phone or clear)", ErrBadRequest, req.Preset)
		}
		return &vp, nil
	case req.Width > 0 && req.Height > 0:
		return &Viewport{Width: req.Width, Height: req.Height, Mobile: req.Mobile}, nil
	default:
		return nil, fmt.Errorf("%w: viewport needs a preset (desktop, tablet, phone, clear) or width and height", ErrBadRequest)
	}
}

// setViewport applies the window size to the page and remembers it.
func (t *tab) setViewport(ctx context.Context, vp *Viewport) error {
	if vp == nil {
		if err := t.cdp(ctx, "Emulation.clearDeviceMetricsOverride", nil, nil); err != nil {
			return err
		}
		t.mu.Lock()
		t.viewport = nil
		t.mu.Unlock()
		return nil
	}
	if err := t.cdp(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": vp.Width, "height": vp.Height, "deviceScaleFactor": 1, "mobile": vp.Mobile,
	}, nil); err != nil {
		return err
	}
	got := *vp
	t.mu.Lock()
	t.viewport = &got
	t.mu.Unlock()
	return nil
}

// viewportOf reads the tab's remembered window size (nil when untouched).
func (t *tab) viewportOf() *Viewport {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.viewport == nil {
		return nil
	}
	got := *t.viewport
	return &got
}
