package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// CreateTeamInput is the user-facing create request.
type CreateTeamInput struct {
	Name             string
	Directory        string
	InitialRoleIDs   []string
	RequestID        string
	ExpectedRevision int64
	// AdoptMarker confirms the user's explicit choice to claim a directory
	// that already carries a team marker (B04): without it the product
	// refuses rather than adopting or overwriting another claim.
	AdoptMarker bool
}

// TeamMarker is the on-disk identification file inside <teamDir>/.cobot.
// It never holds tokens, secrets or business state.
type TeamMarker struct {
	SchemaVersion int    `json:"schemaVersion"`
	InstanceID    string `json:"instanceId"`
	Profile       string `json:"profile"`
	TeamID        string `json:"teamId"`
	Directory     string `json:"directory"`
}

// CreateTeam reserves the binding and members atomically, then prepares the
// directory marker; a preparation failure flips the team to error (retryable)
// and never deletes the user's directory contents.
func (a *App) CreateTeam(ctx context.Context, principal auth.Principal, in CreateTeamInput) (domain.Team, []domain.Bot, error) {
	if principal.Write == nil && in.RequestID != "" {
		var err error
		principal, err = WithWriteRequest(principal, "team.create", in.RequestID, in)
		if err != nil {
			return domain.Team{}, nil, err
		}
	}
	cached, unlock, writeErr := beginWrite[TeamWithBots](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Team{}, nil, writeErr
	}
	if cached != nil {
		team, _, err := sqlite.GetTeam(ctx, a.DB, cached.Team.ID)
		if err != nil {
			return domain.Team{}, nil, err
		}
		bots, err := sqlite.ListBots(ctx, a.DB, team.ID)
		return team, bots, err
	}

	if principal.Kind != auth.KindUser {
		return domain.Team{}, nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return domain.Team{}, nil, domain.InvalidRequest("error.team.name_required", nil)
	}
	if strings.TrimSpace(in.Directory) == "" {
		return domain.Team{}, nil, domain.InvalidRequest("error.team.directory_required", nil)
	}
	settings, err := a.GetSettings(ctx)
	if err != nil {
		return domain.Team{}, nil, err
	}

	canon, err := platform.CanonicalDirFrom(in.Directory)
	if err != nil {
		return domain.Team{}, nil, domain.InvalidRequest("error.team.directory_invalid", map[string]any{"detail": err.Error()})
	}
	if info, err := os.Stat(canon.Display); err != nil || !info.IsDir() {
		return domain.Team{}, nil, domain.InvalidRequest("error.team.directory_missing", map[string]any{"directory": canon.Display})
	}
	if reserved, ok := platform.IsReservedDir(a.Paths.Reserved(), canon); ok {
		return domain.Team{}, nil, domain.InvalidRequest("error.team.directory_reserved", map[string]any{"reserved": reserved})
	}

	// A marker in an unbound directory claims the folder for some team —
	// this profile's deleted one, a moved original, or a copy (B04). Never
	// adopt it silently: only an explicit adoptMarker overwrites it.
	if marker, present, merr := readTeamMarker(canon.Display); merr != nil {
		return domain.Team{}, nil, merr
	} else if present && !in.AdoptMarker {
		return domain.Team{}, nil, domain.InvalidRequest("error.team.marker_adopt_required", map[string]any{
			"instanceId": marker.InstanceID, "directory": canon.Display,
		})
	}

	// Uniqueness and nesting run inside the transaction's read phase.
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, nil, err
	}
	defer tx.Rollback()
	settings, err = sqlite.GetSettings(ctx, tx)
	if err != nil {
		return domain.Team{}, nil, err
	}

	if existing, err := sqlite.FindActiveTeamByDirectoryKey(ctx, tx, canon.Key); err == nil {
		return domain.Team{}, nil, domain.Conflict("error.team.directory_bound", map[string]any{
			"teamId": existing.ID, "teamName": existing.Name, "directory": existing.Directory,
		})
	} else if !errors.Is(err, sqlite.ErrNotFound) {
		return domain.Team{}, nil, err
	}
	if conflict, err := a.findNestedBinding(ctx, tx, canon); err != nil {
		return domain.Team{}, nil, err
	} else if conflict != nil {
		return domain.Team{}, nil, domain.Conflict("error.team.directory_nested", map[string]any{
			"teamId": conflict.ID, "teamName": conflict.Name, "directory": conflict.Directory,
		})
	}

	defaults, err := roles.Defaults()
	if err != nil {
		return domain.Team{}, nil, err
	}
	roleByID := map[string]roles.Role{}
	for _, role := range defaults {
		roleByID[role.ID] = role
	}
	leader, ok := roleByID[roles.LeaderRoleID]
	if !ok {
		return domain.Team{}, nil, domain.Internal(errors.New("leader role missing"))
	}
	seenNames := map[string]int{}
	bots := make([]domain.Bot, 0, 1+len(in.InitialRoleIDs))
	leaderBot := domain.Bot{
		ID: domain.NewID(), TeamID: "", Name: uniqueBotName(leader.Name, seenNames),
		RoleID: leader.ID, RoleVersion: leader.ContentVersion, IsLeader: true,
	}
	bots = append(bots, leaderBot)
	for _, roleID := range in.InitialRoleIDs {
		role, ok := roleByID[roleID]
		if !ok || role.ID == roles.LeaderRoleID {
			return domain.Team{}, nil, domain.InvalidRequest("error.team.unknown_role", map[string]any{"roleId": roleID})
		}
		bots = append(bots, domain.Bot{
			ID: domain.NewID(), TeamID: "", Name: uniqueBotName(role.Name, seenNames),
			RoleID: role.ID, RoleVersion: role.ContentVersion,
		})
	}
	if len(bots) > settings.MemberLimit {
		return domain.Team{}, nil, domain.InvalidRequest("error.team.member_limit", map[string]any{
			"requested": len(bots), "limit": settings.MemberLimit,
		})
	}

	team := domain.Team{
		ID: domain.NewID(), Name: name, ProfileInstance: a.InstanceID,
		LeaderID: leaderBot.ID, Status: domain.TeamStatusCreating,
	}
	for i := range bots {
		bots[i].TeamID = team.ID
	}
	if err := sqlite.InsertTeamStaged(ctx, tx, team, canon.Display, canon.Key, bots); err != nil {
		if isUniqueViolation(err) {
			return domain.Team{}, nil, domain.Conflict("error.team.directory_bound_race", nil)
		}
		return domain.Team{}, nil, err
	}
	reserved, _, err := sqlite.GetTeam(ctx, tx, team.ID)
	if err != nil {
		return domain.Team{}, nil, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, TeamWithBots{Team: reserved, Bots: bots}); err != nil {
		return domain.Team{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Team{}, nil, err
	}

	// Resource preparation happens after the reservation committed. Failure
	// keeps a retryable error state; the user's files are never removed.
	if err := a.prepareTeamResources(ctx, team, canon); err != nil {
		if _, serr := sqlite.SetTeamStatus(ctx, a.DB, team.ID, domain.TeamStatusError, err.Error(), 0); serr != nil {
			a.Logger.Error("mark team error failed", "teamId", team.ID, "error", serr)
		}
		a.recordEvent(ctx, domain.Event{Type: domain.EventTeamUpdated, TeamID: team.ID, ResourceID: team.ID})
		a.Hub.Publish()
		return domain.Team{}, nil, domain.Conflict("error.team.prepare_failed", map[string]any{"detail": err.Error()})
	}
	if _, err := sqlite.SetTeamStatus(ctx, a.DB, team.ID, domain.TeamStatusActive, "", 0); err != nil {
		return domain.Team{}, nil, err
	}

	team, _, err = sqlite.GetTeam(ctx, a.DB, team.ID)
	if err != nil {
		return domain.Team{}, nil, err
	}
	botsOut, err := sqlite.ListBots(ctx, a.DB, team.ID)
	if err != nil {
		return domain.Team{}, nil, err
	}
	if err := a.recordEvent(ctx, domain.Event{
		Type: domain.EventTeamCreated, TeamID: team.ID, ResourceID: team.ID, Revision: team.Revision,
		Payload: mustJSON(map[string]any{"teamId": team.ID, "name": team.Name}),
	}); err != nil {
		return domain.Team{}, nil, err
	}
	for _, bot := range botsOut {
		_ = a.recordEvent(ctx, domain.Event{Type: domain.EventBotAdded, TeamID: team.ID, BotID: bot.ID, ResourceID: bot.ID})
	}
	a.Hub.Publish()
	return team, botsOut, nil
}

// findNestedBinding returns a team whose bound directory contains or is
// contained by the candidate, comparing component-wise on canonical keys.
func (a *App) findNestedBinding(ctx context.Context, tx *sql.Tx, candidate platform.CanonicalDir) (*domain.Team, error) {
	return a.findNestedBindingExcluding(ctx, tx, candidate, "")
}

// findNestedBindingExcluding ignores one team's own binding, which a rebind
// must do: moving a team must not conflict with where it lives today.
func (a *App) findNestedBindingExcluding(ctx context.Context, tx *sql.Tx, candidate platform.CanonicalDir, excludeTeamID string) (*domain.Team, error) {
	bindings, err := sqlite.ListDirectoryBindings(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		if binding.TeamID == excludeTeamID {
			continue
		}
		other := platform.CanonicalDir{Key: binding.DirectoryKey, Display: binding.Directory}
		if other.Contains(candidate) || candidate.Contains(other) {
			team, _, err := sqlite.GetTeam(ctx, tx, binding.TeamID)
			if err != nil {
				return nil, err
			}
			return &team, nil
		}
	}
	return nil, nil
}

// prepareTeamResources writes the .cobot marker and ensures the Git local
// exclude entry without touching shared ignores or initialising Git.
func (a *App) prepareTeamResources(ctx context.Context, team domain.Team, canon platform.CanonicalDir) error {
	if err := roles.EnsureProfileCopy(a.Paths.RolesDir()); err != nil {
		return err
	}
	cobotDir := filepath.Join(canon.Display, ".cobot")
	if err := os.MkdirAll(cobotDir, 0o755); err != nil {
		return fmt.Errorf("create .cobot: %w", err)
	}
	marker := TeamMarker{
		SchemaVersion: 1,
		InstanceID:    a.InstanceID,
		Profile:       string(a.Paths.Environment),
		TeamID:        team.ID,
		Directory:     canon.Display,
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	markerPath := filepath.Join(cobotDir, "team.json")
	if err := os.WriteFile(markerPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write team marker: %w", err)
	}
	for _, sub := range []string{"context", "resources"} {
		if err := os.MkdirAll(filepath.Join(cobotDir, sub), 0o755); err != nil {
			return fmt.Errorf("create .cobot/%s: %w", sub, err)
		}
	}
	// Git local exclude is best-effort: a non-Git directory is valid.
	if excludePath, err := platform.GitExcludePath(canon.Display); err == nil {
		if err := platform.EnsureLocalCobotExclude(excludePath); err != nil {
			return fmt.Errorf("update git exclude: %w", err)
		}
	}
	_ = ctx
	return nil
}

// RetryTeamCreation re-runs resource preparation for a team stuck in the
// error state (B06): no half-created bots are duplicated and the reservation
// is reused.
func (a *App) RetryTeamCreation(ctx context.Context, principal auth.Principal, teamID string) (domain.Team, error) {
	cached, unlock, writeErr := beginWrite[domain.Team](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Team{}, writeErr
	}
	if cached != nil {
		team, _, err := sqlite.GetTeam(ctx, a.DB, cached.ID)
		return team, err
	}

	if principal.Kind != auth.KindUser {
		return domain.Team{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	team, directory, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return domain.Team{}, sqlite.NormalizeError(err)
	}
	if team.Status != domain.TeamStatusError {
		return domain.Team{}, domain.Conflict("error.team.not_retryable", map[string]any{"status": team.Status})
	}
	canon, err := platform.CanonicalDirFrom(directory)
	if err != nil {
		return domain.Team{}, domain.InvalidRequest("error.team.directory_invalid", nil)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Team{}, err
	}
	defer tx.Rollback()
	expected := team.Revision
	if principal.Write != nil && principal.Write.ExpectedRevision > 0 {
		expected = principal.Write.ExpectedRevision
	}
	reserved, err := sqlite.SetTeamStatusTx(ctx, tx, team.ID, domain.TeamStatusCreating, "", expected)
	if err != nil {
		return domain.Team{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, reserved); err != nil {
		return domain.Team{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Team{}, err
	}
	if err := a.prepareTeamResources(ctx, team, canon); err != nil {
		_, _ = sqlite.SetTeamStatus(ctx, a.DB, team.ID, domain.TeamStatusError, "resource_preparation_failed", 0)
		return domain.Team{}, domain.Conflict("error.team.prepare_failed", map[string]any{"detail": err.Error()})
	}
	activated, err := sqlite.SetTeamStatus(ctx, a.DB, teamID, domain.TeamStatusActive, "", 0)
	if err != nil {
		return domain.Team{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTeamUpdated, TeamID: teamID, ResourceID: teamID})
	a.Hub.Publish()
	return activated, nil
}

// RebuildMarker recreates the identification marker after the user deleted
// .cobot, reporting (never deleting) lost resources.
func (a *App) RebuildMarker(ctx context.Context, principal auth.Principal, teamID string) (map[string]any, error) {
	cached, unlock, writeErr := beginWrite[map[string]any](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return nil, writeErr
	}
	if cached != nil {
		return *cached, nil
	}

	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	team, directory, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	canon, err := platform.CanonicalDirFrom(directory)
	if err != nil {
		return nil, domain.InvalidRequest("error.team.directory_invalid", nil)
	}
	cobotDir := filepath.Join(canon.Display, ".cobot")
	lostResources := false
	if entries, err := os.ReadDir(cobotDir); err != nil {
		lostResources = true
	} else {
		for _, entry := range entries {
			if entry.Name() != "team.json" && entry.Name() != "context" && entry.Name() != "resources" {
				lostResources = true
			}
		}
	}
	intent := map[string]any{"rebuilt": false, "resourcesLost": lostResources, "state": "unknown"}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	fresh, _, err := sqlite.GetTeam(ctx, tx, teamID)
	if err != nil {
		return nil, err
	}
	if principal.Write != nil && principal.Write.ExpectedRevision > 0 && fresh.Revision != principal.Write.ExpectedRevision {
		return nil, domain.RevisionConflict("error.team.revision_conflict", nil)
	}
	if err := a.saveWriteResult(ctx, tx, principal, intent); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := a.prepareTeamResources(ctx, team, canon); err != nil {
		return nil, domain.Conflict("error.team.prepare_failed", map[string]any{"detail": err.Error()})
	}
	result := map[string]any{"rebuilt": true, "resourcesLost": lostResources, "state": "completed"}
	if err := a.finishWriteIntent(ctx, principal, result); err != nil {
		return nil, err
	}
	return result, nil
}

// AddBot appends one member of an existing role to an active team; the user
// does this directly without an approval round trip.
func (a *App) AddBot(ctx context.Context, principal auth.Principal, teamID, roleID string) (domain.Bot, error) {
	if principal.Kind != auth.KindUser {
		return domain.Bot{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	cached, unlock, writeErr := beginWrite[domain.Bot](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Bot{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Bot{}, err
	}
	defer tx.Rollback()
	bot, err := a.addBotLocked(ctx, tx, teamID, roleID)
	if err != nil {
		return domain.Bot{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, bot); err != nil {
		return domain.Bot{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Bot{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventBotAdded, TeamID: teamID, BotID: bot.ID, ResourceID: bot.ID})
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTeamUpdated, TeamID: teamID, ResourceID: teamID})
	a.Hub.Publish()
	return bot, nil
}

// Recruitment decisions and manual creation share the same transactional
// member-limit/name/role checks; neither can bypass the other's insert.
func (a *App) addBotLocked(ctx context.Context, tx *sql.Tx, teamID, roleID string) (domain.Bot, error) {
	team, _, err := sqlite.GetTeam(ctx, tx, teamID)
	if err != nil {
		return domain.Bot{}, sqlite.NormalizeError(err)
	}
	if team.Status != domain.TeamStatusActive {
		return domain.Bot{}, domain.Conflict("error.team.not_active", map[string]any{"status": team.Status})
	}
	settings, err := sqlite.GetSettings(ctx, tx)
	if err != nil {
		return domain.Bot{}, err
	}
	bots, err := sqlite.ListBots(ctx, tx, teamID)
	if err != nil {
		return domain.Bot{}, err
	}
	if len(bots) >= settings.MemberLimit {
		return domain.Bot{}, domain.Conflict("error.team.member_limit", map[string]any{"limit": settings.MemberLimit})
	}
	role, err := roles.Default(roleID)
	if err != nil || role.ID == roles.LeaderRoleID {
		return domain.Bot{}, domain.InvalidRequest("error.team.unknown_role", map[string]any{"roleId": roleID})
	}
	seen := map[string]int{}
	for _, b := range bots {
		seen[b.Name]++
	}
	bot := domain.Bot{ID: domain.NewID(), TeamID: teamID, Name: uniqueBotName(role.Name, seen), RoleID: role.ID, RoleVersion: role.ContentVersion}
	if err := sqlite.InsertBotTx(ctx, tx, bot); err != nil {
		return domain.Bot{}, err
	}
	return sqlite.GetBot(ctx, tx, bot.ID)
}

// ListTeams projects the user-visible teams with their members.
func (a *App) ListTeams(ctx context.Context, principal auth.Principal) ([]TeamWithBots, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	teams, err := sqlite.ListTeams(ctx, a.DB)
	if err != nil {
		return nil, err
	}
	out := make([]TeamWithBots, 0, len(teams))
	for _, team := range teams {
		bots, err := sqlite.ListBots(ctx, a.DB, team.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, TeamWithBots{Team: team, Bots: bots})
	}
	return out, nil
}

// TeamWithBots is the list projection.
type TeamWithBots struct {
	Team domain.Team  `json:"team"`
	Bots []domain.Bot `json:"bots"`
}

// GetTeamDetail returns one team with members for the conversation shell.
func (a *App) GetTeamDetail(ctx context.Context, principal auth.Principal, teamID string) (TeamWithBots, error) {
	if principal.Kind != auth.KindUser {
		return TeamWithBots{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	team, _, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return TeamWithBots{}, sqlite.NormalizeError(err)
	}
	bots, err := sqlite.ListBots(ctx, a.DB, teamID)
	if err != nil {
		return TeamWithBots{}, err
	}
	return TeamWithBots{Team: team, Bots: bots}, nil
}

func uniqueBotName(base string, seen map[string]int) string {
	seen[base]++
	if seen[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s %d", base, seen[base])
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// TouchTeamActivity records a directory existence check for the UI.
func (a *App) VerifyTeamDirectory(teamID string) bool {
	_, directory, err := sqlite.GetTeam(context.Background(), a.DB, teamID)
	if err != nil || directory == "" {
		return false
	}
	info, err := os.Stat(directory)
	return err == nil && info.IsDir()
}
