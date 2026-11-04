package integration

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"runtime"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
)

// Avatars, previews and the update check moved out of internal/app. These hold
// still what the interface sees of them, through the real handlers, so the
// interface comparison in tests/contract covers them too.

func (h *m2Harness) callAPI(t *testing.T, method, path string, body map[string]any) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, h.api.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (h *m2Harness) expectAPI(t *testing.T, want int, method, path string, body map[string]any) map[string]any {
	t.Helper()
	if body != nil {
		body["requestId"] = domain.NewID()
	}
	status, out := h.callAPI(t, method, path, body)
	if status != want {
		t.Fatalf("%s %s = %d, want %d: %v", method, path, status, want, out)
	}
	data, _ := out["data"].(map[string]any)
	return data
}

func TestAvatarEndpointsOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	_, bots := h.createTeam(t, []string{"fullstack-engineer"})

	catalog := h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/avatars/catalog", nil)
	if series, ok := catalog["series"].([]any); !ok || len(series) != 0 {
		t.Fatalf("a profile without a catalog serves an empty one: %v", catalog)
	}
	h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/avatars/catalog/sync", map[string]any{})

	h.expectAPI(t, http.StatusBadRequest, http.MethodPost, "/api/v1/avatars/uploads", map[string]any{"data": "not base64!"})
	h.expectAPI(t, http.StatusBadRequest, http.MethodPost, "/api/v1/avatars/uploads", map[string]any{"data": base64.StdEncoding.EncodeToString(pngOf(t, 32))})
	saved := h.expectAPI(t, http.StatusCreated, http.MethodPost, "/api/v1/avatars/uploads", map[string]any{"data": base64.StdEncoding.EncodeToString(pngOf(t, 256))})
	file, _ := saved["file"].(string)
	if file == "" {
		t.Fatalf("upload returned no file: %v", saved)
	}
	h.expectAPI(t, http.StatusOK, http.MethodGet, "/api/v1/avatars/uploads/"+file, nil)
	h.expectAPI(t, http.StatusNotFound, http.MethodGet, "/api/v1/avatars/uploads/missing.png", nil)

	picture := map[string]any{"kind": domain.AvatarKindUpload, "file": file}
	missing := map[string]any{"kind": domain.AvatarKindUpload, "file": "0000000000000000000000000000000000000000000000000000000000000000.png"}
	h.expectAPI(t, http.StatusBadRequest, http.MethodPut, "/api/v1/roles/fullstack-engineer/avatar", map[string]any{"avatar": missing})
	h.expectAPI(t, http.StatusNotFound, http.MethodPut, "/api/v1/roles/no-such-role/avatar", map[string]any{"avatar": picture})
	h.expectAPI(t, http.StatusOK, http.MethodPut, "/api/v1/roles/fullstack-engineer/avatar", map[string]any{"avatar": picture})
	h.expectAPI(t, http.StatusOK, http.MethodPut, "/api/v1/roles/fullstack-engineer/avatar", map[string]any{"avatar": nil})

	member := bots[len(bots)-1]
	updated := h.expectAPI(t, http.StatusOK, http.MethodPatch, "/api/v1/bots/"+member.ID, map[string]any{"avatar": picture, "expectedRevision": member.Revision})
	if got, _ := updated["avatar"].(map[string]any); got["file"] != file {
		t.Fatalf("member avatar = %v", updated["avatar"])
	}
	h.expectAPI(t, http.StatusOK, http.MethodPatch, "/api/v1/bots/"+member.ID, map[string]any{"avatar": nil, "expectedRevision": updated["revision"]})
	h.expectAPI(t, http.StatusConflict, http.MethodPatch, "/api/v1/bots/"+member.ID, map[string]any{"avatar": nil, "expectedRevision": 999})
}

func TestPreviewAndUpdateEndpointsOverHTTP(t *testing.T) {
	h := newM2Harness(t)
	team, _ := h.createTeam(t, nil)

	checked := h.expectAPI(t, http.StatusOK, http.MethodPost, "/api/v1/updates/check", map[string]any{})
	if checked["state"] == "" || checked["state"] == nil {
		t.Fatalf("an update check always reports a state: %v", checked)
	}

	command := "ping -n 30 127.0.0.1"
	if runtime.GOOS != "windows" {
		command = "sleep 30"
	}
	base := "/api/v1/teams/" + team.ID + "/preview/"
	h.expectAPI(t, http.StatusBadRequest, http.MethodPost, base+"start", map[string]any{"command": "a\nb"})
	h.expectAPI(t, http.StatusNotFound, http.MethodPost, "/api/v1/teams/no-such-team/preview/start", map[string]any{"command": command})
	started := h.expectAPI(t, http.StatusOK, http.MethodPost, base+"start", map[string]any{"command": command})
	if started["running"] != true {
		t.Fatalf("start = %v", started)
	}
	h.expectAPI(t, http.StatusOK, http.MethodPost, base+"start", map[string]any{"command": command})
	h.expectAPI(t, http.StatusOK, http.MethodPost, base+"stop", map[string]any{"command": command})
	h.expectAPI(t, http.StatusOK, http.MethodPost, base+"stop", map[string]any{})
}
