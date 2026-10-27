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
}

// TeamMarker is the on-disk identification file inside <teamDir>/.cobot.
// It never holds tokens, secrets or business state.
type TeamMarker struct {
	SchemaVersion int    `json:"schemaVersion"`
	InstanceID    string `json:"instanceId"`
	Profile       string `json:"profile"`
	TeamID        string `json:"teamId"`
	TeamName      string `json:"teamName,omitempty"`
	Directory     string `json:"directory"`
}

// CreateTeam creates the team, its members, its folder binding and the
// folder's .cobot data in one transaction: it either succeeds completely or
// leaves nothing behind. The user's own files are never touched.
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
	// A user file named .cobot cannot hold team resources. Check before
	// reading its marker: Linux reports ENOTDIR for .cobot/team.json here,
	// while Windows treats that path as missing. Neither case may replace
	// the user's file or leave a partially created team.
	if info, err := os.Stat(filepath.Join(canon.Display, ".cobot")); err == nil && !info.IsDir() {
		return domain.Team{}, nil, domain.Conflict("error.team.prepare_failed", map[string]any{"detail": ".cobot is not a directory"})
	}

	// One project belongs to one team for its whole life: a folder another
	// team still claims is refused, never taken over. Adoption of a foreign
	// marker goes through OpenTeam + AdoptTeam, the only path that rewrites
	// a marker, and it retires the foreign file before creation re-checks.
	if err := a.refuseClaimedDirectory(ctx, canon.Display); err != nil {
		return domain.Team{}, nil, err
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
		if existing.Status == domain.TeamStatusArchived {
			return domain.Team{}, nil, domain.Conflict("error.team.directory_archived", map[string]any{
				"teamId": existing.ID, "teamName": existing.Name, "directory": existing.Directory,
			})
		}
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
		LeaderID: leaderBot.ID, Status: domain.TeamStatusActive,
	}
	for i := range bots {
		bots[i].TeamID = team.ID
	}
	if err := sqlite.InsertTeam(ctx, tx, team, canon.Display, canon.Key, bots); err != nil {
		if isUniqueViolation(err) {
			return domain.Team{}, nil, domain.Conflict("error.team.directory_bound_race", nil)
		}
		return domain.Team{}, nil, err
	}
	// Creation is one transaction: the project folder is prepared while it
	// is still open, and any failure rolls the rows back and removes the
	// .cobot folder this attempt created. A team exists completely or not
	// at all; there is no half-created state to retry.
	cobotDir := filepath.Join(canon.Display, ".cobot")
	_, statErr := os.Stat(cobotDir)
	createdCobot := os.IsNotExist(statErr)
	undo := func() {
		if createdCobot {
			_ = os.RemoveAll(cobotDir)
		}
	}
	if err := a.prepareTeamResources(ctx, team, canon); err != nil {
		undo()
		return domain.Team{}, nil, domain.Conflict("error.team.prepare_failed", map[string]any{"detail": err.Error()})
	}
	created, _, err := sqlite.GetTeam(ctx, tx, team.ID)
	if err != nil {
		undo()
		return domain.Team{}, nil, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, TeamWithBots{Team: created, Bots: bots}); err != nil {
		undo()
		return domain.Team{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		undo()
		return domain.Team{}, nil, err
	}
	team = created

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
	bindings, err := sqlite.ListDirectoryBindings(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
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
		TeamName:      team.Name,
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
	return listTeamsOn(ctx, a.DB)
}

func listTeamsOn(ctx context.Context, q sqlite.Queryer) ([]TeamWithBots, error) {
	teams, err := sqlite.ListTeams(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]TeamWithBots, 0, len(teams))
	for _, team := range teams {
		item, err := teamWithBotsOn(ctx, q, team)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// teamWithBotsOn lists a team's members for the team list, each with a
// preview of its latest message.
func teamWithBotsOn(ctx context.Context, q sqlite.Queryer, team domain.Team) (TeamWithBots, error) {
	bots, err := sqlite.ListBots(ctx, q, team.ID)
	if err != nil {
		return TeamWithBots{}, err
	}
	// Each member's latest finished run, for the surfaces that ask whether
	// its last session failed.
	lastRuns := map[string]string{}
	rows, err := q.QueryContext(ctx, `SELECT bot_id, status FROM runs r
		WHERE ended_at != '' AND bot_id IN (SELECT id FROM bots WHERE team_id = ?)
		  AND NOT EXISTS (SELECT 1 FROM runs n WHERE n.bot_id = r.bot_id AND n.ended_at != '' AND (n.started_at, n.id) > (r.started_at, r.id))`, team.ID)
	if err == nil {
		for rows.Next() {
			var botID, status string
			if rows.Scan(&botID, &status) == nil {
				lastRuns[botID] = status
			}
		}
		rows.Close()
	}
	previews, err := sqlite.LatestMessagePreviews(ctx, q, team.ID)
	if err != nil {
		return TeamWithBots{}, err
	}
	for i := range bots {
		if preview, ok := previews[bots[i].ID]; ok {
			bots[i].LastMessage = &preview
		}
		bots[i].LastRunStatus = lastRuns[bots[i].ID]
	}
	return TeamWithBots{Team: team, Bots: bots}, nil
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

// markerClaim reports which team still claims a folder through its marker,
// or nil when nobody does. A marker written by this instance for a team that
// was deleted is stale and claims nothing. Any other marker — another CoBot
// instance or profile, a copied folder, an unreadable file — is a live claim
// this instance cannot verify, so it is respected.
func (a *App) markerClaim(ctx context.Context, dir string) (map[string]any, error) {
	marker, present, err := readTeamMarker(dir)
	if err != nil || !present {
		return nil, err
	}
	if marker.InstanceID == a.InstanceID && marker.TeamID != "" {
		team, _, err := sqlite.GetTeam(ctx, a.DB, marker.TeamID)
		if errors.Is(err, sqlite.ErrNotFound) || (err == nil && team.Status == domain.TeamStatusDeleted) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"teamId": team.ID, "teamName": team.Name, "external": false, "archived": team.Status == domain.TeamStatusArchived}, nil
	}
	return map[string]any{"teamId": marker.TeamID, "teamName": marker.TeamName, "external": true}, nil
}

// refuseClaimedDirectory rejects a folder another team still claims, naming
// that team; the marker is the only trace of teams in other CoBot instances.
func (a *App) refuseClaimedDirectory(ctx context.Context, dir string) error {
	claim, err := a.markerClaim(ctx, dir)
	if err != nil || claim == nil {
		return err
	}
	if claim["external"] == true {
		key := "error.team.directory_claimed_external"
		if claim["teamName"] == "" {
			key = "error.team.directory_claimed_unknown"
		}
		return domain.Conflict(key, map[string]any{"teamName": claim["teamName"], "directory": dir})
	}
	key := "error.team.directory_bound"
	if claim["archived"] == true {
		key = "error.team.directory_archived"
	}
	return domain.Conflict(key, map[string]any{"teamId": claim["teamId"], "teamName": claim["teamName"], "directory": dir})
}

// CheckTeamDirectories checks every live team's folder. It runs at startup
// and whenever the team list is loaded, so the list a user looks at is
// always honest; dispatch checks its own team before every turn.
func (a *App) CheckTeamDirectories(ctx context.Context) {
	teams, err := sqlite.ListTeams(ctx, a.DB)
	if err != nil {
		return
	}
	for _, team := range teams {
		a.checkTeamDirectory(ctx, team)
	}
}

// checkTeamDirectory keeps one team's state honest about its lifelong
// folder and returns the team as it is afterwards. A folder that is gone
// makes an active team directory_missing: it stays visible read-only and
// receives no work. Once the folder is back at the same path the team is
// active again by itself. A lost marker in a folder the team owns is
// rewritten; a marker naming another team is never overwritten.
func (a *App) checkTeamDirectory(ctx context.Context, team domain.Team) domain.Team {
	if team.Status != domain.TeamStatusActive && team.Status != domain.TeamStatusDirectoryMissing {
		return team
	}
	info, statErr := os.Stat(team.Directory)
	present := team.Directory != "" && statErr == nil && info.IsDir()
	if present {
		marker, has, err := readTeamMarker(team.Directory)
		ours := err == nil && (!has || (marker.InstanceID == a.InstanceID && marker.TeamID == team.ID))
		if ours && (!has || marker.TeamName != team.Name) {
			if canon, err := platform.CanonicalDirFrom(team.Directory); err == nil {
				if err := a.prepareTeamResources(ctx, team, canon); err != nil && a.Logger != nil {
					a.Logger.Warn("rewrite team marker", "teamId", team.ID, "error", err)
				}
			}
		} else if !ours && a.Logger != nil {
			a.Logger.Warn("team folder carries another team's marker; left untouched", "teamId", team.ID, "directory", team.Directory)
		}
	}
	next := ""
	switch {
	case !present && team.Status == domain.TeamStatusActive:
		next, team.StatusDetail = domain.TeamStatusDirectoryMissing, team.Directory
	case present && team.Status == domain.TeamStatusDirectoryMissing:
		next, team.StatusDetail = domain.TeamStatusActive, ""
	default:
		return team
	}
	updated, err := sqlite.SetTeamStatus(ctx, a.DB, team.ID, next, team.StatusDetail, 0)
	if err != nil {
		return team
	}
	a.recordEvent(ctx, domain.Event{Type: domain.EventTeamUpdated, TeamID: team.ID, ResourceID: team.ID, Revision: updated.Revision})
	if next == domain.TeamStatusActive && a.Scheduler != nil {
		for _, bot := range mustListBots(ctx, a, team.ID) {
			a.Scheduler.Wake(bot.ID)
		}
	}
	a.Hub.Publish()
	return updated
}

// OpenTeamInput names the folder the user wants to open.
type OpenTeamInput struct {
	Directory string
	RequestID string
}

// OpenTeam resolves a project folder to the team that owns it and reports
// what the user can do with it: jump to a live team, restore an archived
// one, adopt a folder another CoBot environment marked, or create a team in
// an unmarked folder. Nothing is written until the user confirms; adopting
// is the one case that overrides the marker, and only here - creation still
// refuses a claimed folder.
func (a *App) OpenTeam(ctx context.Context, principal auth.Principal, in OpenTeamInput) (map[string]any, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if strings.TrimSpace(in.Directory) == "" {
		return nil, domain.InvalidRequest("error.team.directory_required", nil)
	}
	canon, err := platform.CanonicalDirFrom(in.Directory)
	if err != nil {
		return nil, domain.InvalidRequest("error.team.directory_invalid", map[string]any{"detail": err.Error()})
	}
	if info, err := os.Stat(canon.Display); err != nil || !info.IsDir() {
		return nil, domain.InvalidRequest("error.team.directory_missing", map[string]any{"directory": canon.Display})
	}
	// The marker is read before the transaction: markerClaim touches the
	// database itself, and the pool is one connection — a second query inside
	// the open transaction would wait on itself.
	claim, err := a.markerClaim(ctx, canon.Display)
	if err != nil {
		return nil, err
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// A folder a live team of this instance holds belongs to it, whatever an
	// older marker says.
	existing, err := sqlite.FindActiveTeamByDirectoryKey(ctx, tx, canon.Key)
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		return nil, err
	}
	if existing.ID != "" {
		action := "open"
		if existing.Status == domain.TeamStatusArchived {
			action = "restore"
		}
		return map[string]any{
			"action": action, "teamId": existing.ID, "teamName": existing.Name,
			"archived": existing.Status == domain.TeamStatusArchived, "directory": canon.Display,
		}, nil
	}
	if claim != nil && claim["external"] == true {
		return map[string]any{
			"action": "adopt", "teamName": claim["teamName"], "directory": canon.Display,
		}, nil
	}
	// A marker this instance wrote but whose row is gone (another profile
	// deleted it) reads as a fresh folder: the user creates a team again.
	name := claim["teamName"]
	if name == nil || name == "" {
		name = folderDisplayName(canon.Display)
	}
	return map[string]any{
		"action": "create", "suggestedName": name, "directory": canon.Display,
	}, nil
}

// folderDisplayName is the last path segment, the name a new team starts
// with when the user picks a folder.
func folderDisplayName(path string) string {
	cleaned := strings.TrimRight(path, `\/`)
	if i := max(strings.LastIndexAny(cleaned, `/\`)+1, 0); i < len(cleaned) {
		return cleaned[i:]
	}
	return path
}

// AdoptTeamInput is a confirmed adoption: a folder another CoBot environment
// marked becomes this instance's team under a new identity.
type AdoptTeamInput struct {
	Directory        string
	Name             string
	InitialRoleIDs   []string
	RequestID        string
	ExpectedRevision int64
}

// AdoptTeam binds a folder that another CoBot instance marked to a new team
// of this instance, rewriting the marker. The old conversations and threads
// are not migrated; the user confirms this in the interface before calling.
func (a *App) AdoptTeam(ctx context.Context, principal auth.Principal, in AdoptTeamInput) (domain.Team, []domain.Bot, error) {
	if principal.Write == nil && in.RequestID != "" {
		var err error
		principal, err = WithWriteRequest(principal, "team.adopt", in.RequestID, in)
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
		return cached.Team, cached.Bots, nil
	}
	if principal.Kind != auth.KindUser {
		return domain.Team{}, nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	// The folder is canonicalized before any path use, exactly as creation
	// does: everything below works on the canonical display path only.
	canon, err := platform.CanonicalDirFrom(strings.TrimSpace(in.Directory))
	if err != nil {
		return domain.Team{}, nil, domain.InvalidRequest("error.team.directory_invalid", map[string]any{"detail": err.Error()})
	}
	if info, err := os.Stat(canon.Display); err != nil || !info.IsDir() {
		return domain.Team{}, nil, domain.InvalidRequest("error.team.directory_missing", map[string]any{"directory": canon.Display})
	}
	// The folder must still be marked by someone else: an unmarked folder
	// goes through creation, and a folder this instance holds stays with
	// that team.
	if err := a.refuseForeignOnly(ctx, canon.Display); err != nil {
		return domain.Team{}, nil, err
	}
	// Creation carries the rest: name, roles, limits, the nested check, and
	// the marker rewrite, which lands with the new team's own .cobot. The
	// foreign marker is the one thing creation may not re-refuse: adoption
	// just verified it is the very state this call exists for, so the marker
	// is retired first (it is about to be rewritten anyway) and the rest of
	// creation's guarantees - member limit, nesting, the transactional bind -
	// stay in force. The write scope is dropped before delegating:
	// CreateTeam re-derives the idempotency lock from the same request id and
	// would wait on itself.
	if err := a.retireForeignMarker(ctx, canon); err != nil {
		return domain.Team{}, nil, err
	}
	inner := principal
	inner.Write = nil
	team, bots, err := a.CreateTeam(ctx, inner, CreateTeamInput{
		Name: in.Name, Directory: in.Directory, InitialRoleIDs: in.InitialRoleIDs, RequestID: in.RequestID,
	})
	if err != nil {
		return domain.Team{}, nil, err
	}
	return team, bots, nil
}

// retireForeignMarker removes a foreign instance's marker file, leaving the
// folder unmarked until creation writes this instance's own. A failure here
// refuses the adoption: half-retired markers would let creation proceed on a
// folder whose state was never confirmed.
func (a *App) retireForeignMarker(ctx context.Context, canon platform.CanonicalDir) error {
	if err := a.refuseForeignOnly(ctx, canon.Display); err != nil {
		return err
	}
	root, err := os.OpenRoot(canon.Display)
	if err != nil {
		return domain.Conflict("error.team.prepare_failed", map[string]any{"detail": err.Error()})
	}
	defer root.Close()
	if err := root.Remove(filepath.Join(".cobot", "team.json")); err != nil {
		return domain.Conflict("error.team.prepare_failed", map[string]any{"detail": err.Error()})
	}
	return nil
}

// refuseForeignOnly refuses a folder unless a foreign instance marks it: the
// only state adoption exists for.
func (a *App) refuseForeignOnly(ctx context.Context, dir string) error {
	claim, err := a.markerClaim(ctx, dir)
	if err != nil {
		return err
	}
	if claim == nil {
		return domain.Conflict("error.team.directory_unmarked", map[string]any{"directory": dir})
	}
	if claim["external"] != true {
		return domain.Conflict("error.team.directory_bound", map[string]any{
			"teamId": claim["teamId"], "teamName": claim["teamName"], "directory": dir,
		})
	}
	return nil
}
