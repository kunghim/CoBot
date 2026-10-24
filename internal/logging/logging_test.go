package logging

import (
	"errors"
	"io"
	"os"
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

type brokenSink struct{}

func (brokenSink) Write([]byte) (int, error) { return 0, errors.New("invalid handle") }

// A desktop build has no console. Its stdout fails every write, and that must
// not keep the line out of the log file.
func TestAMissingConsoleDoesNotKeepLinesOutOfTheFile(t *testing.T) {
	dir := t.TempDir()
	file, closeFn, err := newRotatingFile(filepath.Join(dir, "cobot.log"), 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	w := io.MultiWriter(file, bestEffort{brokenSink{}})
	if _, err := w.Write([]byte("line\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "cobot.log"))
	if err != nil || string(data) != "line\n" {
		t.Fatalf("log file holds %q (%v)", data, err)
	}
}
