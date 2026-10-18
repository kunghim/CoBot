package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTeamPathOnlyOpensFilesInsideTheTeam(t *testing.T) {
	root := t.TempDir()
	team := filepath.Join(root, "team")
	if err := os.MkdirAll(filepath.Join(team, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(team, "src", "index.html")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(file)

	slashed := "/" + filepath.ToSlash(file)
	for _, raw := range []string{
		file,
		"src/index.html",
		"src/index.html:12",
		"src/index.html#L3-L9",
		"file://" + slashed,
		strings.ReplaceAll(slashed, "index", "%69ndex"),
	} {
		got, ok := resolveTeamPath(team, raw)
		if !ok || !strings.EqualFold(got, want) {
			t.Errorf("%q -> %q, %v", raw, got, ok)
		}
	}
	for _, raw := range []string{outside, "../secret.txt", "src/missing.html", "", root} {
		if got, ok := resolveTeamPath(team, raw); ok {
			t.Errorf("%q must be refused, got %q", raw, got)
		}
	}
}
