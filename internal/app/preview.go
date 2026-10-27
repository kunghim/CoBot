package app

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// previewRun is one "start and open" the user launched from an acceptance
// card. The command runs in the team's directory as a background process the
// core owns; it is not a member turn and never reaches the engine.
type previewRun struct {
	teamID string
	cmd    *exec.Cmd
	done   chan struct{}
}

// previewRuns is the process table. One process per team: a second start on
// the same team stops the first, so a re-run replaces its predecessor
// instead of fighting it over the port.
type previewTable struct {
	mu    sync.Mutex
	byCmd map[string]*previewRun // key: teamID + "\x00" + command
}

func (a *App) previewTableGet() *previewTable {
	a.previewOnce.Do(func() {
		a.previews = &previewTable{byCmd: map[string]*previewRun{}}
	})
	return a.previews
}

// StartPreview runs the given command in the team's directory. The process
// outlives the request and is stopped by StopPreview, team shutdown, or core
// exit - never by the turn that handed the card off.
func (a *App) StartPreview(ctx context.Context, principal auth.Principal, teamID, command string) (map[string]any, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	command = strings.TrimSpace(command)
	if command == "" || len(command) > 2000 || strings.ContainsAny(command, "\n\r") {
		return nil, domain.InvalidRequest("error.invalid_request", nil)
	}
	team, _, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	if team.Status != domain.TeamStatusActive {
		return nil, domain.Conflict("error.team.not_active", map[string]any{"status": team.Status})
	}
	table := a.previewTableGet()
	table.mu.Lock()
	defer table.mu.Unlock()
	key := teamID + "\x00" + command
	if existing := table.byCmd[key]; existing != nil {
		select {
		case <-existing.done:
		default:
			return map[string]any{"started": true, "running": true}, nil
		}
	}
	run, err := a.spawnPreview(teamID, team.Directory, command)
	if err != nil {
		return nil, err
	}
	table.byCmd[key] = run
	return map[string]any{"started": true, "running": true}, nil
}

// StopPreview stops one running preview, or every preview of the team when
// no command is named. Stopping a finished process is fine.
func (a *App) StopPreview(ctx context.Context, principal auth.Principal, teamID, command string) error {
	if principal.Kind != auth.KindUser {
		return domain.Forbidden("error.forbidden.user_only", nil)
	}
	table := a.previewTableGet()
	table.mu.Lock()
	defer table.mu.Unlock()
	for key, run := range table.byCmd {
		if run.teamID != teamID {
			continue
		}
		if command != "" && key != teamID+"\x00"+command {
			continue
		}
		a.stopPreview(run)
		delete(table.byCmd, key)
	}
	return nil
}

// stopTeamPreviews stops every preview of one team; the team archive and
// delete paths call it so no process outlives its folder's team.
func (a *App) stopTeamPreviews(teamID string) {
	table := a.previewTableGet()
	table.mu.Lock()
	defer table.mu.Unlock()
	for key, run := range table.byCmd {
		if run.teamID == teamID {
			a.stopPreview(run)
			delete(table.byCmd, key)
		}
	}
}

func (a *App) spawnPreview(teamID, dir, command string) (*previewRun, error) {
	cmd := previewCommandProcess(command)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return nil, domain.Conflict("error.preview.start_failed", map[string]any{"detail": err.Error()})
	}
	run := &previewRun{teamID: teamID, cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(run.done)
	}()
	return run, nil
}

func (a *App) stopPreview(run *previewRun) {
	select {
	case <-run.done:
		return
	default:
	}
	killProcessTree(run.cmd)
	select {
	case <-run.done:
	case <-time.After(5 * time.Second):
	}
}

// killProcessTree ends the process and its children: a start command usually
// spawns a tool that owns the port, so killing only the shell would leave
// the service running.

// stopAllPreviews stops every preview process; the core's shutdown calls it
// so nothing is left serving after the app exits.
func (a *App) stopAllPreviews() {
	table := a.previewTableGet()
	table.mu.Lock()
	defer table.mu.Unlock()
	for key, run := range table.byCmd {
		a.stopPreview(run)
		delete(table.byCmd, key)
	}
}
