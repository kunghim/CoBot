package browser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCobotDirNeverBringsBackTheTeamFolder(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "team")
	if _, err := ensureCobotDir(gone, "browser"); err == nil {
		t.Fatal("a team folder that is gone must fail the call")
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatalf("the team folder came back: %v", err)
	}
}

func TestEnsureCobotDirCreatesWhatIsMissing(t *testing.T) {
	team := t.TempDir()
	for range 2 { // the second call finds everything in place
		dir, err := ensureCobotDir(team, "downloads")
		if err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("%s: %v", dir, err)
		}
	}
}
