// Package eventsink is the local durable event ledger (H08): live events
// are appended to per-instance JSONL files as they are recorded, drained
// into the events table by id (never twice), quarantined when corrupt, and
// pruned only after import. Un-imported evidence is never silently deleted.
package eventsink

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// Capacity and retention policy: one profile's event files never grow past
// MaxBytes without a gap being recorded, and imported files are kept for
// RetainDays as evidence before being pruned.
const (
	MaxBytes         int64 = 20 << 20
	RetainDays             = 7
	livePrefix             = "live-"
	importedPrefix         = "imported-"
	quarantinePrefix       = "quarantine-"
)

// ErrOverCapacity means the sink refuses to append: the profile is past its
// file budget with evidence that has not been imported yet, so the caller
// records a gap instead of deleting un-imported facts.
var ErrOverCapacity = errors.New("event files over capacity with un-imported evidence")

// Sink appends live events to one JSONL file per instance inside
// <root>/events. All methods are safe for concurrent use.
type Sink struct {
	dir        string
	instanceID string
	limit      int64
	mu         chan struct{} // 1-buffered mutex: serializes appends
}

// Open prepares the event directory of a profile root.
func Open(root, instanceID string) (*Sink, error) {
	return OpenWithLimit(root, instanceID, MaxBytes)
}

// OpenWithLimit is Open with a custom capacity; production uses MaxBytes,
// tests use a small limit to exercise the gap path quickly.
func OpenWithLimit(root, instanceID string, limit int64) (*Sink, error) {
	dir := filepath.Join(root, "events")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("events dir: %w", err)
	}
	return &Sink{dir: dir, instanceID: instanceID, mu: make(chan struct{}, 1), limit: limit}, nil
}

type sinkFile struct {
	path string
	info os.FileInfo
}

func (s *Sink) listPrefix(prefix string) ([]sinkFile, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []sinkFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, sinkFile{path: filepath.Join(s.dir, entry.Name()), info: info})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].info.ModTime().Before(out[j].info.ModTime()) })
	return out, nil
}

// TotalBytes is the current size of every file in the event directory.
func (s *Sink) TotalBytes() (int64, error) {
	files, err := s.listPrefix("")
	if err != nil {
		return 0, err
	}
	var total int64
	for _, f := range files {
		total += f.info.Size()
	}
	return total, nil
}

// Append writes one event line to this instance's live file. Past the
// capacity budget it refuses rather than deleting un-imported evidence.
func (s *Sink) Append(ev domain.Event) error {
	s.mu <- struct{}{}
	defer func() { <-s.mu }()
	total, err := s.totalBytesLocked()
	if err != nil {
		return err
	}
	if total >= s.limit {
		return ErrOverCapacity
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, livePrefix+sanitize(s.instanceID)+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func (s *Sink) totalBytesLocked() (int64, error) {
	files, err := s.listPrefix("")
	if err != nil {
		return 0, err
	}
	var total int64
	for _, f := range files {
		total += f.info.Size()
	}
	return total, nil
}

// LiveFiles lists the not-yet-imported event files, oldest first.
func (s *Sink) LiveFiles() ([]string, error) {
	files, err := s.listPrefix(livePrefix)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.path)
	}
	return out, nil
}

// MarkImported renames a drained live file so it can never be imported
// twice; imported files are retention-kept evidence.
func (s *Sink) MarkImported(livePath string) error {
	name := filepath.Base(livePath)
	if !strings.HasPrefix(name, livePrefix) {
		return errors.New("not a live event file")
	}
	return os.Rename(livePath, filepath.Join(s.dir, importedPrefix+strings.TrimPrefix(name, livePrefix)))
}

// Quarantine moves a corrupt file aside, preserving it as evidence instead
// of deleting the only copy of whatever it contained.
func (s *Sink) Quarantine(path string) error {
	name := filepath.Base(path)
	stripped := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(name, livePrefix), importedPrefix), quarantinePrefix)
	return os.Rename(path, filepath.Join(s.dir, quarantinePrefix+stripped+"."+time.Now().UTC().Format("20060102T150405")))
}

// PruneImported enforces retention and capacity against imported files only:
// files older than RetainDays go first, then the oldest imported files until
// the directory fits the budget again. It returns how many files were pruned
// and whether un-imported evidence still exceeds the budget (the caller
// records a gap in that case — the files themselves stay).
func (s *Sink) PruneImported(now time.Time) (pruned int, overCapacity bool, err error) {
	files, err := s.listPrefix(importedPrefix)
	if err != nil {
		return 0, false, err
	}
	total, err := s.totalBytesLocked()
	if err != nil {
		return 0, false, err
	}
	cutoff := now.AddDate(0, 0, -RetainDays)
	for _, f := range files {
		if !f.info.ModTime().Before(cutoff) {
			continue
		}
		if rmErr := os.Remove(f.path); rmErr != nil {
			return pruned, false, rmErr
		}
		total -= f.info.Size()
		pruned++
	}
	for _, f := range files {
		if total <= s.limit {
			break
		}
		if _, statErr := os.Stat(f.path); statErr != nil {
			continue // already pruned by age
		}
		if rmErr := os.Remove(f.path); rmErr != nil {
			return pruned, false, rmErr
		}
		total -= f.info.Size()
		pruned++
	}
	if total > s.limit {
		// Only un-imported evidence is left over budget: it stays put.
		return pruned, true, nil
	}
	return pruned, false, nil
}

// ReadLines returns the parsed events of one file; a corrupt line is
// reported with its 1-based number so the caller can quarantine the file as
// evidence after importing what is valid.
func ReadLines(path string) (events []domain.Event, corrupt int, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var ev domain.Event
		if jsonErr := json.Unmarshal(line, &ev); jsonErr != nil || ev.ID == "" {
			corrupt++
			continue
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return events, corrupt, err
	}
	return events, corrupt, nil
}

func sanitize(instanceID string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, instanceID)
	if out == "" {
		return "default"
	}
	return out
}
