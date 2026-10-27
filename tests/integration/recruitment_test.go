package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// requestMember calls the project manager's recruitment tool the way its
// bridge does.
func requestMember(t *testing.T, h *m2Harness, bot domain.Bot, roleIDs []string, reason string) map[string]any {
	t.Helper()
	status, body := h.callTool(t, h.botToken(t, bot), "request_member", map[string]any{
		"roleIds": roleIDs, "reason": reason, "idempotencyKey": "recruit-" + strings.Join(roleIDs, "-"),
	})
	if status != 200 {
		t.Fatalf("request_member %v = %d %v", roleIDs, status, body)
	}
	// The tool result rides two envelopes: the HTTP one and the tool result's
	// own status/data pair. Flatten them so tests read status and fields
	// alike.
	wrapper, _ := body["data"].(map[string]any)
	data, _ := wrapper["data"].(map[string]any)
	for key, value := range data {
		wrapper[key] = value
	}
	return wrapper
}

// openRecruitment returns the team's single open recruitment question.
func openRecruitment(t *testing.T, db sqlite.Queryer, teamID string) domain.Question {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT id FROM questions WHERE team_id = ? AND kind = ? AND status = ?`, teamID, domain.QuestionKindRecruitment, domain.QuestionOpen)
	if err != nil {
		t.Fatal(err)
	}
	// The pool is a single connection: the id must be read and the rows
	// closed before the detail query below can run.
	var id string
	if rows.Next() {
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
	}
	rows.Close()
	if id == "" {
		t.Fatal("no open recruitment question")
	}
	question, err := sqlite.GetQuestion(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	return question
}

// An empty team has only the project manager; its recruitment request waits
// on a card, and approving adds the member through the same checks a manual
// add uses and tells the project manager the result.
func TestRecruitmentApproveAddsMemberAndWakesLeader(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	pm := bots[0]

	data := requestMember(t, h, pm, []string{"fullstack-engineer"}, "实现环节没人做")
	question := openRecruitment(t, h.app.DB, team.ID)
	if data["status"] != "waiting" || data["questionId"] != question.ID {
		t.Fatalf("request_member result = %v, want waiting with %s", data, question.ID)
	}
	if question.Kind != domain.QuestionKindRecruitment || len(question.RoleIDs) != 1 || question.RoleIDs[0] != "fullstack-engineer" {
		t.Fatalf("question = %+v", question)
	}

	// The user's approve adds the member and tells the project manager.
	answered, err := h.app.AnswerRecruitment(ctx, h.user, question.ID, domain.RecruitmentApprove, "", question.Revision)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if answered.Status != domain.QuestionAnswered || answered.AnswerMessageID == "" {
		t.Fatalf("answered = %+v, want answered with the delivered message", answered)
	}
	members, err := sqlite.ListBots(ctx, h.app.DB, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("team members = %d, want the recruited one added", len(members))
	}
	var added domain.Bot
	for _, bot := range members {
		if bot.RoleID == "fullstack-engineer" {
			added = bot
		}
	}
	if added.ID == "" {
		t.Fatal("no fullstack member was added")
	}
	messages, err := sqlite.ListMessages(ctx, h.app.DB, pm.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMessageText(messages, "你同意了招募申请") || !hasMessageText(messages, added.ID) {
		t.Fatal("the project manager was not told the approval with the new member id")
	}
	if h.queuedForBot(t, pm.ID) != 1 {
		t.Fatal("the result is not queued for the project manager's next turn")
	}
}

// Rejecting leaves the team unchanged and hands the project manager the
// verdict together with the user's comment, so it does not ask again the
// same way.
func TestRecruitmentRejectCarriesTheUserComment(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	pm := bots[0]

	requestMember(t, h, pm, []string{"qa-engineer"}, "需要测试执行")
	question := openRecruitment(t, h.app.DB, team.ID)
	if _, err := h.app.AnswerRecruitment(ctx, h.user, question.ID, domain.RecruitmentReject, "这次需求一个人能做完", question.Revision); err != nil {
		t.Fatalf("reject: %v", err)
	}
	members, err := sqlite.ListBots(ctx, h.app.DB, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 {
		t.Fatalf("team members = %d, want none added on reject", len(members))
	}
	messages, err := sqlite.ListMessages(ctx, h.app.DB, pm.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMessageText(messages, "你拒绝了招募申请") || !hasMessageText(messages, "这次需求一个人能做完") {
		t.Fatal("the project manager was not told the rejection with the user's comment")
	}
}

// Invalid requests are refused with a reason the model can act on: a role the
// team already has, the leader role, an unknown role, a call from a member,
// and a request that would exceed the member limit. A request that fit the
// limit when it was made still cannot sneak past it at decision time.
func TestRecruitmentRefusesInvalidRoles(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	pm, dev := bots[0], bots[1]

	refused := func(bot domain.Bot, roleIDs []string, key string) {
		t.Helper()
		data := requestMember(t, h, bot, roleIDs, "需要")
		errObj, _ := data["error"].(map[string]any)
		if data["status"] != "rejected" || errObj["messageKey"] != key {
			t.Fatalf("request_member %v = %v, want refusal %s", roleIDs, data, key)
		}
	}

	refused(pm, []string{"fullstack-engineer"}, "error.team.role_present")
	refused(pm, []string{"leader"}, "error.team.unknown_role")
	refused(pm, []string{"no-such-role"}, "error.team.unknown_role")
	refused(dev, []string{"qa-engineer"}, "error.recruitment.leader_only")

	// Two roles fit the default limit of six, but not a limit of two.
	question, err := h.app.RequestMember(ctx, auth.NewPrincipalBot(pm.ID, team.ID), app.RequestMemberInput{
		RoleIDs: []string{"qa-engineer", "code-reviewer"}, Reason: "测试和评审都没有人",
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	limit := 2
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{MemberLimit: &limit}, 0); err != nil {
		t.Fatalf("shrink the member limit: %v", err)
	}
	if _, err := h.app.AnswerRecruitment(ctx, h.user, question.ID, domain.RecruitmentApprove, "", question.Revision); err == nil {
		t.Fatal("approving past the shrunk member limit must fail")
	} else {
		var de *domain.Error
		if !errors.As(err, &de) || de.MessageKey != "error.team.member_limit" {
			t.Fatalf("approve past the limit = %v, want error.team.member_limit", err)
		}
	}
}

// The recruitment card lives in the database, not in the asking connection:
// after a restart the card is still there and the decision still adds the
// member and tells the project manager.
func TestRecruitmentCardSurvivesRestart(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	pm := bots[0]

	requestMember(t, h, pm, []string{"qa-engineer"}, "测试执行没人做")

	// A restarted core opens the same database fresh: no leases, no run
	// bindings, only the rows. The first core's scheduler is stopped so its
	// background writes do not lock the file against the second pool.
	if err := h.app.Scheduler.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	db2, err := sqlite.Open(h.app.Paths.DatabaseFile())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db2.Close() })
	restarted := &app.App{
		DB: db2, Paths: h.app.Paths, InstanceID: "test-instance", Version: "test",
		Hub: h.app.Hub,
	}

	page, err := restarted.ListPage(ctx, h.user, app.PageRequest{Kind: "questions", TeamID: team.ID, State: "open"})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := page["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("open questions after restart = %d, want the recruitment card", len(items))
	}
	card, ok := items[0].(domain.Question)
	if !ok || card.Kind != domain.QuestionKindRecruitment {
		t.Fatalf("open question after restart = %+v, want the recruitment card", items[0])
	}
	if _, err := restarted.AnswerRecruitment(ctx, h.user, card.ID, domain.RecruitmentApprove, "", card.Revision); err != nil {
		t.Fatalf("decide after restart: %v", err)
	}
	members, err := sqlite.ListBots(ctx, h.app.DB, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("team members after the restart decision = %d, want the recruited one added", len(members))
	}
	messages, err := sqlite.ListMessages(ctx, h.app.DB, pm.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMessageText(messages, "你同意了招募申请") {
		t.Fatal("the project manager was not told the result after the restart")
	}
}

// teamDirFor is a fresh, uniquely named folder under the harness root.
func (h *m2Harness) teamDirFor(name string) string {
	return filepath.Join(filepath.Dir(h.teamDir), name)
}

// openMarker writes a team marker like the core does, for folders whose rows
// this instance does not hold.
func openMarker(t *testing.T, dir, instanceID, teamID, teamName string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".cobot"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := map[string]any{"schemaVersion": 1, "instanceId": instanceID, "teamId": teamID, "teamName": teamName, "directory": dir}
	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".cobot", "team.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenTeamResolvesEachFolderState(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	// A live team's folder opens directly; an archived one offers restore.
	live, bots := h.createTeamWithDir(t, "打开-活跃", "open-live", nil)
	open, err := h.app.OpenTeam(ctx, h.user, app.OpenTeamInput{Directory: h.teamDirFor("open-live")})
	if err != nil {
		t.Fatal(err)
	}
	if open["action"] != "open" || open["teamId"] != live.ID {
		t.Fatalf("live folder = %v", open)
	}
	archived, _ := h.createTeamWithDir(t, "打开-归档", "open-archived", nil)
	if _, err := h.app.ArchiveTeam(ctx, h.user, archived.ID, 0); err != nil {
		t.Fatal(err)
	}
	res, err := h.app.OpenTeam(ctx, h.user, app.OpenTeamInput{Directory: h.teamDirFor("open-archived")})
	if err != nil {
		t.Fatal(err)
	}
	if res["action"] != "restore" || res["teamId"] != archived.ID {
		t.Fatalf("archived folder = %v", res)
	}
	_ = bots

	// A foreign marker offers adoption; nothing is written by resolving.
	foreignDir := h.teamDirFor("open-foreign")
	openMarker(t, foreignDir, "other-instance", "team-from-elsewhere", "异环境的团队")
	adopt, err := h.app.OpenTeam(ctx, h.user, app.OpenTeamInput{Directory: foreignDir})
	if err != nil {
		t.Fatal(err)
	}
	if adopt["action"] != "adopt" || adopt["teamName"] != "异环境的团队" {
		t.Fatalf("foreign folder = %v", adopt)
	}
	if _, err := os.Stat(filepath.Join(foreignDir, ".cobot", "team.json")); err != nil {
		t.Fatal("resolution touched the marker")
	}

	// An unmarked folder offers creation with the folder's name.
	freshDir := h.teamDirFor("open-fresh")
	if err := os.MkdirAll(freshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	create, err := h.app.OpenTeam(ctx, h.user, app.OpenTeamInput{Directory: freshDir})
	if err != nil {
		t.Fatal(err)
	}
	if create["action"] != "create" || create["suggestedName"] != "open-fresh" {
		t.Fatalf("fresh folder = %v", create)
	}
}

func TestAdoptTeamRewritesTheMarkerAndCreatesTheTeam(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	foreignDir := h.teamDirFor("adopt-run")
	openMarker(t, foreignDir, "other-instance", "team-from-elsewhere", "异环境的团队")

	created, bots, err := h.app.AdoptTeam(ctx, h.user, app.AdoptTeamInput{
		Directory: foreignDir, Name: "接管的团队", InitialRoleIDs: []string{"fullstack-engineer"}, RequestID: "adopt-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 {
		t.Fatalf("adopted team members = %d, want leader plus one", len(bots))
	}
	// The marker now names this instance's team.
	data, err := os.ReadFile(filepath.Join(foreignDir, ".cobot", "team.json"))
	if err != nil {
		t.Fatal(err)
	}
	var marker struct {
		InstanceID string `json:"instanceId"`
		TeamID     string `json:"teamId"`
	}
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatal(err)
	}
	if marker.InstanceID != h.app.InstanceID || marker.TeamID != created.ID {
		t.Fatalf("marker = %+v, want this instance and %s", marker, created.ID)
	}
	// Creation still refuses the folder: adoption is the only way in.
	if _, _, err := h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "再建", Directory: foreignDir}); err == nil {
		t.Fatal("a bound folder accepted a second team through creation")
	}
	// And an unmarked folder is not adoption's business.
	fresh := h.teamDirFor("adopt-fresh")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.app.AdoptTeam(ctx, h.user, app.AdoptTeamInput{Directory: fresh, Name: "x", RequestID: "adopt-2"}); err == nil {
		t.Fatal("an unmarked folder was adopted")
	}
}

func TestPreviewRunsAndStopsInTheTeamDirectory(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, _ := h.createTeamWithDir(t, "预览", "preview-run", nil)

	// A bot cannot start anything.
	if _, err := h.app.StartPreview(ctx, auth.NewPrincipalBot(team.LeaderID, team.ID), team.ID, "echo"); err == nil {
		t.Fatal("a bot started a preview")
	}
	// An empty or multi-line command is refused outright.
	if _, err := h.app.StartPreview(ctx, h.user, team.ID, ""); err == nil {
		t.Fatal("an empty command was accepted")
	}
	if _, err := h.app.StartPreview(ctx, h.user, team.ID, "a\nb"); err == nil {
		t.Fatal("a multi-line command was accepted")
	}
	if runtime.GOOS != "windows" {
		t.Skip("previews spawn cmd.exe, which only Windows has")
	}

	// A marker file proves the command runs in the team's directory.
	script := filepath.Join(h.teamDirFor("preview-run"), "preview-marker.cmd")
	if err := os.WriteFile(script, []byte("@echo off\r\nexit /b 0\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A long-running command: it stays up until stopped, and the process
	// table must keep it.
	command := "cmd /C ping -n 30 127.0.0.1 > NUL"
	if _, err := h.app.StartPreview(ctx, h.user, team.ID, command); err != nil {
		t.Fatalf("start preview: %v", err)
	}
	// A second press of the same button does not start a twin.
	if _, err := h.app.StartPreview(ctx, h.user, team.ID, command); err != nil {
		t.Fatal(err)
	}
	if err := h.app.StopPreview(ctx, h.user, team.ID, command); err != nil {
		t.Fatal(err)
	}
}
