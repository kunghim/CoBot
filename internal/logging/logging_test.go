package logging

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLogFileRotationKeepsBoundedFootprint(t *testing.T) {
	dir := t.TempDir()
	rf, closeFn, err := newRotatingFile(filepath.Join(dir, "cobot.log"), 1024, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()

	line := strings.Repeat("x", 200) + "\n"
	for i := 0; i < 50; i++ {
		if _, err := rf.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := closeFn(); err != nil {
		t.Fatal(err)
	}

	// active + 2 rotated siblings at most
	entries, err := filepath.Glob(filepath.Join(dir, "cobot.log*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 3 {
		t.Fatalf("rotation must keep at most 3 files, got %d: %v", len(entries), entries)
	}
}
