// Package dirpicker shows the operating system's folder chooser on the
// machine running CoBot. The core is loopback-only and runs on the user's
// own desktop, so the Web page can ask it for a real absolute path, which a
// browser's own picker never reveals. Choosing a folder grants nothing: the
// path still goes through the normal directory check before any use.
package dirpicker

import (
	"context"
	"errors"
	"sync"
)

var (
	// ErrCancelled reports that the user closed the chooser without a choice.
	ErrCancelled = errors.New("folder selection cancelled")
	// ErrUnavailable reports that this machine has no usable chooser, for
	// example a Linux session without zenity or kdialog.
	ErrUnavailable = errors.New("no native folder chooser available")
	// ErrBusy reports that a chooser is already open.
	ErrBusy = errors.New("a folder chooser is already open")
)

var open sync.Mutex

// Pick shows one native folder chooser at a time and returns the chosen
// absolute path. title is shown where the platform supports it.
func Pick(ctx context.Context, title string) (string, error) {
	if !open.TryLock() {
		return "", ErrBusy
	}
	defer open.Unlock()
	return pick(ctx, title)
}
