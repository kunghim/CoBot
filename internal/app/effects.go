package app

import (
	"context"

	"github.com/kunghim/CoBot/internal/domain"
)

// effects is what a transaction leaves to do once it commits.
type effects struct {
	wake   []string
	events []domain.Event
}

func (e *effects) add(other effects) {
	e.wake = append(e.wake, other.wake...)
	e.events = append(e.events, other.events...)
}

// publish records the events and wakes the members. Call it after Commit.
func (a *App) publish(ctx context.Context, e effects) {
	for _, ev := range e.events {
		_ = a.recordEvent(ctx, ev)
	}
	a.Hub.Publish()
	if a.Scheduler == nil {
		return
	}
	for _, id := range e.wake {
		if id != "" {
			a.Scheduler.Wake(id)
		}
	}
}
