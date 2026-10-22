package app

import (
	"context"
	"path/filepath"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/eventsink"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// ImportEventFiles drains the local ledger into the events table:
// every line is inserted by its event id, so an event that already arrived
// live is never duplicated; a corrupt file is quarantined as evidence after
// its valid lines are imported; a clean file is renamed imported-* and kept
// for retention before being pruned. Un-imported evidence is never deleted
// to make room — the over-capacity condition becomes a visible gap.
func (a *App) ImportEventFiles(ctx context.Context) (int, error) {
	if a.Events == nil {
		return 0, nil
	}
	files, err := a.Events.LiveFiles()
	if err != nil {
		return 0, err
	}
	imported := 0
	for _, path := range files {
		events, corrupt, readErr := eventsink.ReadLines(path)
		if readErr != nil {
			// Unreadable as a whole: quarantine the file so the evidence is
			// isolated, not silently dropped.
			_ = a.Events.Quarantine(path)
			if a.Logger != nil {
				a.Logger.Warn("quarantine unreadable event file", "file", filepath.Base(path), "error", readErr)
			}
			continue
		}
		for _, ev := range events {
			// A deleted team is gone for good: its events still in the
			// ledger are not brought back.
			if ev.TeamID != "" {
				if exists, err := sqlite.TeamExists(ctx, a.DB, ev.TeamID); err == nil && !exists {
					continue
				}
			}
			inserted, ierr := sqlite.ImportEventRow(ctx, a.DB, ev)
			if ierr != nil {
				return imported, ierr
			}
			if inserted {
				imported++
			}
		}
		if corrupt > 0 {
			// Valid lines are already durable; the file with its corrupt
			// remainder stays as quarantined evidence.
			_ = a.Events.Quarantine(path)
			_, _ = a.insertEvent(ctx, domain.Event{
				ID: domain.NewID(), Type: domain.EventLogQuarantined,
				ResourceID: filepath.Base(path),
				Payload:    mustJSON(map[string]any{"corruptLines": corrupt}),
			})
			continue
		}
		if merr := a.Events.MarkImported(path); merr != nil {
			return imported, merr
		}
	}
	_, over, perr := a.Events.PruneImported(time.Now())
	if perr != nil && a.Logger != nil {
		a.Logger.Warn("prune imported event files", "error", perr)
	}
	if over && a.eventGapNoted.CompareAndSwap(false, true) {
		// Un-imported evidence still exceeds the budget: files stay, and the
		// gap is recorded once in the database.
		if _, err := a.insertEvent(ctx, domain.Event{
			ID: domain.NewID(), Type: domain.EventLogGap, ResourceID: "events",
			Payload: mustJSON(map[string]any{"reason": "unimported_over_capacity"}),
		}); err == nil {
			a.Hub.Publish()
		}
	}
	if imported > 0 {
		a.Hub.Publish()
	}
	return imported, nil
}

// RecordEventForTest exposes recordEvent to integration tests that drive the
// ledger directly; production code calls recordEvent.
func (a *App) RecordEventForTest(ctx context.Context, ev domain.Event) error {
	return a.recordEvent(ctx, ev)
}
