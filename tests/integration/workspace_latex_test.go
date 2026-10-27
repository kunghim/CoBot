package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

func TestWorkspaceLatexScopeAndUnavailableRuntime(t *testing.T) {
	h := newM2Harness(t)
	team, _ := h.createTeam(t, nil)
	if err := os.WriteFile(filepath.Join(team.Directory, "main.tex"), []byte(`\documentclass{article}`), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := h.app.DiscoverWorkspaceLatex(context.Background(), h.user, "team", team.ID, "main.tex")
	if err != nil || project.Available || project.Suggested != "main.tex" || len(project.Candidates) != 1 {
		t.Fatalf("discovery: %+v %v", project, err)
	}
	for _, operation := range []func(auth.Principal, string) error{
		func(p auth.Principal, path string) error {
			_, err := h.app.DiscoverWorkspaceLatex(context.Background(), p, "team", team.ID, path)
			return err
		},
		func(p auth.Principal, path string) error {
			_, err := h.app.CompileWorkspaceLatex(context.Background(), p, "team", team.ID, path)
			return err
		},
	} {
		if err := operation(auth.NewPrincipalBot("bot", team.ID), "main.tex"); domain.AsError(err).Code != domain.CodeForbidden {
			t.Fatal(err)
		}
		if err := operation(h.user, "../outside.tex"); domain.AsError(err).Code != domain.CodeForbidden {
			t.Fatal(err)
		}
	}
	if _, err := h.app.CompileWorkspaceLatex(context.Background(), h.user, "team", team.ID, "main.tex"); domain.AsError(err).Code != domain.CodeCapabilityMissing {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, h.api.URL+"/api/v1/workspaces/team/"+team.ID+"/latex?path=main.tex", nil)
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope struct {
		Data struct {
			Suggested string `json:"suggested"`
			Available bool   `json:"available"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || envelope.Data.Suggested != "main.tex" || envelope.Data.Available || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(response.Status, envelope)
	}
}
