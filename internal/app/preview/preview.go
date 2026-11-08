// Package preview runs the "start and open" commands of acceptance cards.
package preview

import (
	"context"
	"database/sql"
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

// Table is the process table; the zero value is ready. One process per team: a
// second start on the same team stops the first, so a re-run replaces its
// predecessor instead of fighting it over the port.
type Table struct {
	mu    sync.Mutex
	byCmd map[string]*previewRun // key: teamID + "\x00" + command
}

// Service runs previews of the teams in DB, keeping them in Table.
type Service struct {
	DB    *sql.DB
	Table *Table
}

// Start runs the given command in the team's directory. The process
// outlives the request and is stopped by StopPreview, team shutdown, or core
// exit - never by the turn that handed the card off.
func (s Service) Start(ctx context.Context, principal auth.Principal, teamID, command string) (map[string]any, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	command = strings.TrimSpace(command)
	if command == "" || len(command) > 2000 || strings.ContainsAny(command, "\n\r") {
		return nil, domain.InvalidRequest("error.invalid_request", nil)
	}
	team, _, err := sqlite.GetTeam(ctx, s.DB, teamID)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	if team.Status != domain.TeamStatusActive {
		return nil, domain.Conflict("error.team.not_active", map[string]any{"status": team.Status})
	}
	table := s.Table
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
	run, err := spawnPreview(teamID, team.Directory, command)
	if err != nil {
		return nil, err
	}
	if table.byCmd == nil {
		table.byCmd = map[string]*previewRun{}
	}
	table.byCmd[key] = run
	return map[string]any{"started": true, "running": true}, nil
}

// Stop stops one running preview, or every preview of the team when
// no command is named. Stopping a finished process is fine.
func (s Service) Stop(ctx context.Context, principal auth.Principal, teamID, command string) error {
	if principal.Kind != auth.KindUser {
		return domain.Forbidden("error.forbidden.user_only", nil)
	}
	table := s.Table
	table.mu.Lock()
	defer table.mu.Unlock()
	for key, run := range table.byCmd {
		if run.teamID != teamID {
			continue
		}
		if command != "" && key != teamID+"\x00"+command {
			continue
		}
		stopPreview(run)
		delete(table.byCmd, key)
	}
	return nil
}

// StopTeam stops every preview of one team; the team archive and
// delete paths call it so no process outlives its folder's team.
func (s Service) StopTeam(teamID string) {
	table := s.Table
	table.mu.Lock()
	defer table.mu.Unlock()
	for key, run := range table.byCmd {
		if run.teamID == teamID {
			stopPreview(run)
			delete(table.byCmd, key)
		}
	}
}

func spawnPreview(teamID, dir, command string) (*previewRun, error) {
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

func stopPreview(run *previewRun) {
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

// StopAll stops every preview process; the core's shutdown calls it
// so nothing is left serving after the app exits.
func (s Service) StopAll() {
	table := s.Table
	table.mu.Lock()
	defer table.mu.Unlock()
	for key, run := range table.byCmd {
		stopPreview(run)
		delete(table.byCmd, key)
	}
}
