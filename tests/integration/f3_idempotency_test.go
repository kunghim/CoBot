package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/events"
)

func TestConcurrentTeamReservationRetryReturnsSameTeam(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	dir := filepath.Join(filepath.Dir(h.teamDir), "retry-team")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	input := app.CreateTeamInput{Name: "one team", Directory: dir, InitialRoleIDs: []string{"backend-dev"}, RequestID: "team-reservation"}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			team, _, err := h.app.CreateTeam(ctx, h.user, input)
			if err != nil {
				failures <- err
				return
			}
			ids <- team.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate reservation")
		}
	}
	var count int
	if err := h.app.DB.QueryRow(`SELECT count(*) FROM teams`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("teams=%d err=%v", count, err)
	}
	input.Name = "changed"
	if _, _, err := h.app.CreateTeam(ctx, h.user, input); err == nil {
		t.Fatal("key accepted changed body")
	}
}

func TestHTTPRequiresRevisionAndDeleteHeaders(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"backend-dev"})
	for _, test := range []struct {
		method, path string
		body         map[string]any
	}{
		{"PATCH", "/api/v1/teams/" + team.ID, map[string]any{"name": "bad write"}},
		{"POST", "/api/v1/teams/" + team.ID + "/archive", nil},
		{"POST", "/api/v1/approvals/missing/decisions", map[string]any{"optionId": "accept"}},
		{"POST", "/api/v1/transfers/missing/accept", nil},
	} {
		body := test.body
		if body == nil {
			body = map[string]any{}
		}
		body["requestId"] = domain.NewID()
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(test.method, h.api.URL+test.path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+h.bearer)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatalf("missing version %s => %d", test.path, res.StatusCode)
		}
	}
	req, _ := http.NewRequest("DELETE", h.api.URL+"/api/v1/bots/"+bots[1].ID, nil)
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	req.Header.Set("Idempotency-Key", domain.NewID())
	req.Header.Set("If-Match", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("DELETE header contract => %d", res.StatusCode)
	}
}

func TestConcurrentHTTPTaskAndMemberRetriesCreateOneResource(t *testing.T) {
	for _, action := range []string{"tasks", "bots"} {
		t.Run(action, func(t *testing.T) {
			h := newM2Harness(t)
			team, bots := h.createTeam(t, []string{"backend-dev"})
			body := map[string]any{"requestId": "same-operation", "roleId": "qa-engineer"}
			if action == "tasks" {
				body = map[string]any{"requestId": "same-operation", "ownerId": bots[1].ID, "title": "one task", "acceptanceCriteria": "exactly once"}
			}
			raw, _ := json.Marshal(body)
			var wg sync.WaitGroup
			ids := make(chan string, 12)
			failures := make(chan string, 12)
			for range 12 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					req, _ := http.NewRequest(http.MethodPost, h.api.URL+"/api/v1/teams/"+team.ID+"/"+action, bytes.NewReader(raw))
					req.Header.Set("Authorization", "Bearer "+h.bearer)
					req.Header.Set("Content-Type", "application/json")
					res, err := http.DefaultClient.Do(req)
					if err != nil {
						failures <- err.Error()
						return
					}
					defer res.Body.Close()
					var response struct {
						Data struct {
							ID string `json:"id"`
						} `json:"data"`
					}
					err = json.NewDecoder(res.Body).Decode(&response)
					if res.StatusCode != 201 || err != nil || response.Data.ID == "" {
						failures <- fmt.Sprintf("%d / %v / %+v", res.StatusCode, err, response)
						return
					}
					ids <- response.Data.ID
				}()
			}
			wg.Wait()
			close(ids)
			close(failures)
			for failure := range failures {
				t.Error(failure)
			}
			first := ""
			for id := range ids {
				if first == "" {
					first = id
				}
				if id != first {
					t.Fatal("retry created another resource")
				}
			}
			var count int
			if action == "tasks" {
				h.app.DB.QueryRow("SELECT count(*) FROM tasks").Scan(&count)
				if count != 1 {
					t.Fatal("duplicated tasks", count)
				}
			} else {
				h.app.DB.QueryRow("SELECT count(*) FROM bots").Scan(&count)
				if count != 3 {
					t.Fatal("duplicated members", count)
				}
			}
		})
	}
}

func TestReplayRecordFailureRollsBackBusinessAndDelivery(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	_, err := h.app.DB.Exec(`CREATE TRIGGER fail_replay BEFORE INSERT ON idempotency BEGIN SELECT RAISE(ABORT,'simulated replay persistence failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	input := app.CreateTaskInput{TeamID: team.ID, OwnerID: bots[1].ID, Title: "atomic task", AcceptanceCriteria: "no orphan writes", RequestID: "atomic-key"}
	if _, err := h.app.CreateTask(ctx, h.user, false, input); err == nil {
		t.Fatal("write accepted without its durable retry record")
	}
	for _, table := range []string{"tasks", "messages", "deliveries"} {
		var count int
		h.app.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count)
		if count != 0 {
			t.Fatalf("rollback left %d %s", count, table)
		}
	}
	if _, err := h.app.DB.Exec(`DROP TRIGGER fail_replay`); err != nil {
		t.Fatal(err)
	}
	created, err := h.app.CreateTask(ctx, h.user, false, input)
	if err != nil {
		t.Fatal(err)
	}
	// A different App has no in-memory retry locks or cache. It still replays
	// the committed fact, including when the original response was lost.
	fresh := &app.App{DB: h.app.DB, Paths: h.app.Paths, Hub: events.NewHub(), Logger: h.app.Logger}
	replayed, err := fresh.CreateTask(ctx, h.user, false, input)
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("durable replay failed: %+v / %v", replayed, err)
	}
	input.OwnerID = bots[0].ID
	if _, err := fresh.CreateTask(ctx, h.user, false, input); err == nil || domain.AsError(err).Code != domain.CodeIdempotencyConflict {
		t.Fatal("changed target reused retry key", err)
	}
}
