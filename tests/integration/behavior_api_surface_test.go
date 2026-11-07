package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	apihttp "github.com/kunghim/CoBot/internal/api/http"
	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/contract"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

// These reach the operations other tests call as Go functions, so what the
// product answers over HTTP is compared with api/openapi.yaml as well. Nothing
// here opens a dialog, a file manager or a real engine.

func stringAt(t *testing.T, data map[string]any, keys ...string) string {
	t.Helper()
	var current any = data
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("no %v in %v", keys, data)
		}
		current = object[key]
	}
	text, ok := current.(string)
	if !ok || text == "" {
		t.Fatalf("no %v in %v", keys, data)
	}
	return text
}

func numberAt(t *testing.T, data map[string]any, keys ...string) float64 {
	t.Helper()
	var current any = data
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("no %v in %v", keys, data)
		}
		current = object[key]
	}
	number, ok := current.(float64)
	if !ok {
		t.Fatalf("no number at %v in %v", keys, data)
	}
	return number
}

// deleteAt sends the two headers a DELETE carries instead of a body.
func (h *m2Harness) deleteAt(t *testing.T, path string, revision int64) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, h.api.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	req.Header.Set("Idempotency-Key", domain.NewID())
	req.Header.Set("If-Match", strconv.FormatInt(revision, 10))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("DELETE %s = %d: %s", path, resp.StatusCode, body)
	}
}

func TestTeamLifeOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	var opened []string
	h.app.OpenLocal = func(path string, _ bool) error {
		opened = append(opened, path)
		return nil
	}

	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/roles", nil)
	directory := h.teamDirFor("http-team")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	created := h.expectAPI(t, http.StatusCreated, http.MethodPost, "/api/v1/teams", map[string]any{
		"name": "接口团队", "directory": directory, "initialRoleIds": []string{"fullstack-engineer"},
	})
	id := stringAt(t, created, "team", "id")
	path := "/api/v1/teams/" + id

	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/teams", nil)
	if got := stringAt(t, h.expectAPI(t, http.StatusOK, http.MethodGet, path, nil), "team", "id"); got != id {
		t.Fatalf("team = %s, want %s", got, id)
	}
	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/snapshot", nil)
	h.expectAPI(t, http.StatusAccepted, http.MethodPost, path+"/warm", map[string]any{})
	h.expectAPI(t, http.StatusOK, http.MethodGet, path+"/requirements", nil)

	if err := os.WriteFile(filepath.Join(directory, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.expectAPI(t, http.StatusOK, http.MethodPost, path+"/open-path", map[string]any{"path": "note.txt"})
	if len(opened) != 1 {
		t.Fatalf("opened = %v", opened)
	}
	h.expectAPI(t, http.StatusBadRequest, http.MethodPost, path+"/open-path", map[string]any{"path": "../outside.txt"})

	// A folder that already holds a team opens it; an empty one offers creating.
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/teams/open", map[string]any{"path": directory})
	fresh := h.teamDirFor("http-adopted")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/teams/open", map[string]any{"path": fresh})

	// There is no browser here, and the picture stream refuses a missing tab.
	status, _ := h.callAPI(t, http.MethodGet, path+"/browser/stream?tabId=missing", nil)
	if status == http.StatusOK {
		t.Fatal("streamed a tab that does not exist")
	}

	team, _, err := sqlite.GetTeam(context.Background(), h.app.DB, id)
	if err != nil {
		t.Fatal(err)
	}
	archived, err := h.app.ArchiveTeam(context.Background(), h.user, id, team.Revision)
	if err != nil {
		t.Fatal(err)
	}
	restored := h.expectAPI(t, http.StatusOK, http.MethodPost, path+"/restore", map[string]any{"expectedRevision": archived.Revision})
	if got := stringAt(t, restored, "status"); got != domain.TeamStatusActive {
		t.Fatalf("restored = %v", restored)
	}
	again, err := h.app.ArchiveTeam(context.Background(), h.user, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	h.deleteAt(t, path, again.Revision)
	h.expectAPI(t, http.StatusNotFound, http.MethodGet, path, nil)
}

func TestAdoptingAFolderOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	directory := h.teamDirFor("http-adopt")
	openMarker(t, directory, "other-instance", "team-from-elsewhere", "别处的团队")
	created := h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/teams/adopt", map[string]any{
		"name": "接手团队", "path": directory, "initialRoleIds": []string{"fullstack-engineer"},
	})
	stringAt(t, created, "team", "id")
}

func TestMemberWorkOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	dev := bots[1]
	fake := h.ensureFake(t, team.ID)
	messages := "/api/v1/bots/" + dev.ID + "/messages"

	h.expectAPI(t, http.StatusAccepted, http.MethodPost, messages, map[string]any{"text": "第一件事"})
	waitUntil(t, 5*time.Second, "the turn", func() bool { return len(fake.Turns()) == 1 })
	waitUntil(t, 5*time.Second, "the turn acknowledged", func() bool {
		run, err := sqlite.FindActiveRun(context.Background(), h.app.DB, dev.ID)
		return err == nil && run.ProviderTurnID != ""
	})
	run, err := sqlite.FindActiveRun(context.Background(), h.app.DB, dev.ID)
	if err != nil {
		t.Fatal(err)
	}

	// The member is busy, so the next message queues and can be taken back.
	fake.SteerErr = errors.New("no active turn")
	queued := h.expectAPI(t, http.StatusAccepted, http.MethodPost, messages, map[string]any{"text": "第二件事"})
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/messages/"+stringAt(t, queued, "message", "id")+"/cancel", map[string]any{})

	h.expectAPI(t, http.StatusOK, http.MethodGet, messages, nil)
	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/bots/"+dev.ID+"/stats", nil)
	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/bots/"+dev.ID+"/collaboration", nil)
	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/search?q="+url.QueryEscape("第一件事"), nil)

	h.expectAPI(t, http.StatusAccepted, http.MethodPost, "/api/v1/runs/"+run.ID+"/interrupt", map[string]any{})
	waitUntil(t, 5*time.Second, "the interrupt", func() bool { return len(fake.Interrupts()) == 1 })

	turn := fake.Turns()[0]
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, domain.RunFailed, "坏了")
	settlesAt(t, h, dev.ID, domain.BotStatusIdle)
	h.expectAPI(t, http.StatusAccepted, http.MethodPost, "/api/v1/runs/"+run.ID+"/retry", map[string]any{})
	waitUntil(t, 5*time.Second, "the retried turn", func() bool { return len(fake.Turns()) == 2 })
}

func TestCardsAndTasksOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	leader, owner := bots[0], bots[1]

	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/approvals?teamId="+team.ID, nil)
	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/questions?teamId="+team.ID, nil)

	question, err := h.app.AskUser(ctx, auth.NewPrincipalBot(owner.ID, team.ID), app.AskUserInput{Question: "还要吗"})
	if err != nil {
		t.Fatal(err)
	}
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/questions/"+question.ID+"/cancel", map[string]any{"expectedRevision": question.Revision})

	task, req := h.handCard(t, team, leader, owner, "接口卡片", "做完")
	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/tasks/"+task.ID, nil)
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/requirements/"+req.ID+"/cancel", map[string]any{})
}

func TestSettingsAndEngineOverHTTP(t *testing.T) {
	h := newM2Harness(t)

	settings := h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/settings", nil)
	h.expectAPI(t, http.StatusOK, http.MethodPatch, "/api/v1/settings", map[string]any{
		"patch": map[string]any{"language": "zh-CN"}, "expectedRevision": numberAt(t, settings, "revision"),
	})

	// No installation in this harness: the answers describe a missing engine.
	if got := stringAt(t, h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/engine", nil), "status"); got != "not_configured" {
		t.Fatalf("engine = %s", got)
	}
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/engine/check", map[string]any{})

	control := fakeengine.New(engine.Callbacks{}, "control")
	h.app.ControlFactory = func(context.Context, string) (engine.Control, error) { return control, nil }
	h.expectAPI(t, http.StatusAccepted, http.MethodPost, "/api/v1/auth/login", map[string]any{})
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/auth/login/cancel", map[string]any{})
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/auth/logout", map[string]any{})

	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/models", nil)
	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/model-catalog", nil)
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/model-catalog/resolve", map[string]any{"models": []map[string]any{{"id": "gpt-5.5"}}})
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/model-catalog/apply", map[string]any{})
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/model-catalog/update", map[string]any{})
	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/provider-presets", nil)
}

func TestProviderLifeOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	_, bots := h.createTeam(t, []string{"fullstack-engineer"})
	gateway := providerGateway(t)
	endpoints := []map[string]any{{"dialect": domain.DialectOpenAIResponses, "baseUrl": gateway.URL + "/v1"}}
	models := []map[string]any{{"id": "good-model", "status": "ok"}}

	h.expectAPI(t, http.StatusCreated, http.MethodPost, "/api/v1/providers", map[string]any{
		"id": "gateway", "name": "测试网关", "kind": "custom", "authMode": "api_key",
		"endpoints": endpoints, "models": models, "apiKey": "sk-test",
	})
	path := "/api/v1/providers/gateway"
	got := h.expectAPI(t, http.StatusOK, http.MethodGet, path, nil)
	h.expectAPI(t, http.StatusOK, http.MethodPatch, path, map[string]any{
		"id": "gateway", "name": "新名字", "kind": "custom", "authMode": "api_key",
		"endpoints": endpoints, "models": models, "expectedRevision": numberAt(t, got, "revision"),
	})
	h.expectAPI(t, http.StatusOK, http.MethodPost, path+"/probe", map[string]any{})
	h.expectAPI(t, http.StatusOK, http.MethodPut, "/api/v1/bots/"+bots[1].ID+"/model", map[string]any{
		"providerId": "gateway", "model": "good-model",
	})
	h.expectAPI(t, http.StatusOK, http.MethodPut, "/api/v1/bots/"+bots[1].ID+"/model", map[string]any{
		"providerId": "", "model": "",
	})
	current := h.expectAPI(t, http.StatusOK, http.MethodGet, path, nil)
	disabled := h.expectAPI(t, http.StatusOK, http.MethodPut, path+"/enabled", map[string]any{
		"enabled": false, "expectedRevision": numberAt(t, current, "revision"),
	})
	h.deleteAt(t, path, int64(numberAt(t, disabled, "revision")))
	h.expectAPI(t, http.StatusNotFound, http.MethodGet, path, nil)
}

// The session and internal routes need the secrets the harness keeps to itself,
// so they get a server of their own over the harness's application.
func TestSessionAndInternalRoutesOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	const admin = "instance-secret"
	stopped := make(chan struct{})
	bearer := auth.NewBearerStore(time.Hour)
	server := &apihttp.Server{
		App: h.app, Paths: h.app.Paths, Sessions: auth.NewSessionStore(time.Hour), Bearer: bearer,
		Issuer: auth.NewBootstrapIssuer(time.Minute), Hub: h.app.Hub, AdminSecret: admin, Version: "test",
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		RequestShutdown: func() { close(stopped) },
	}
	api := httptest.NewServer(contract.Wrap(server.Handler()))
	t.Cleanup(api.Close)

	call := func(path string, headers map[string]string, body map[string]any, want int) map[string]any {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		method := http.MethodPost
		var reader io.Reader = bytes.NewReader(data)
		if body == nil {
			method = http.MethodGet
			reader = nil
		}
		req, err := http.NewRequest(method, api.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != want {
			t.Fatalf("%s = %d %v, want %d", path, resp.StatusCode, out, want)
		}
		result, _ := out["data"].(map[string]any)
		return result
	}
	local := map[string]string{"X-Instance-Secret": admin}

	call("/api/v1/status", nil, nil, http.StatusOK)
	call("/internal/v1/session/link", nil, map[string]any{"requestId": "no-secret"}, http.StatusUnauthorized)
	link := call("/internal/v1/session/link", local, map[string]any{"requestId": "link"}, http.StatusOK)
	secret := stringAt(t, link, "bootstrapSecret")

	session := call("/api/v1/session/bootstrap", nil, map[string]any{"requestId": "boot", "secret": secret}, http.StatusOK)
	token := stringAt(t, session, "bearerToken")
	call("/api/v1/session/bootstrap", nil, map[string]any{"requestId": "boot-again", "secret": secret}, http.StatusUnauthorized)

	signedIn := map[string]string{"Authorization": "Bearer " + token}
	call("/api/v1/session/logout", signedIn, map[string]any{"requestId": "out"}, http.StatusOK)
	call("/api/v1/session/logout", signedIn, map[string]any{"requestId": "out-again"}, http.StatusUnauthorized)

	call("/internal/v1/service/stop", local, map[string]any{"requestId": "stop"}, http.StatusAccepted)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the core was not asked to stop")
	}
}

func TestEventStreamOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	h.createTeam(t, []string{"fullstack-engineer"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.api.URL+"/api/v1/events?after=0", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events = %d", resp.StatusCode)
	}
	buffer := make([]byte, 256)
	n, err := resp.Body.Read(buffer)
	if n == 0 {
		t.Fatalf("no event: %v", err)
	}
	cancel()
}
