package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/platform"
)

// The folder can vanish after the check that saw it. Preparing it then fails;
// it must not bring back a folder the user has just deleted.
func TestPreparingAFolderThatIsGoneDoesNotRecreateIt(t *testing.T) {
	root := t.TempDir()
	paths, err := platform.Resolve(platform.EnvDevelopment, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "team")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	canon, err := platform.CanonicalDirFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	a := &App{Paths: paths, InstanceID: "instance"}
	if err := a.prepareTeamResources(context.Background(), domain.Team{ID: "team", Name: "团队"}, canon); err == nil {
		t.Fatal("preparing a folder that is gone must fail")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the folder came back: %v", err)
	}
}
