// Package wiretrace records, while a developer has switched it on, every raw
// message that crosses one of CoBot's process boundaries: the Codex
// app-server's stdin, stdout and stderr, the collaboration bridge's MCP
// stream, native hook calls, and every HTTP request and response of the
// core. Nothing is parsed, filtered or redacted; a record is the bytes as
// they went over the wire plus when, where and in which direction.
//
// Recording is a developer tool with no UI: `cobot trace on` writes
// <data root>/traces/active.json naming a session folder, and every CoBot
// process of that profile notices within a second and appends to its own
// <session>/<proc>-<pid>.jsonl. `cobot trace off` removes the marker. While
// no session is active a Recorder costs one cached check per call.
package wiretrace

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// TracesDir is where a profile keeps its recordings.
func TracesDir(dataRoot string) string { return filepath.Join(dataRoot, "traces") }

func markerFile(dataRoot string) string { return filepath.Join(TracesDir(dataRoot), "active.json") }

// Session is the recording currently switched on.
type Session struct {
	Name      string    `json:"session"`
	StartedAt time.Time `json:"startedAt"`
}

// Dir is the session's folder.
func (s Session) Dir(dataRoot string) string { return filepath.Join(TracesDir(dataRoot), s.Name) }

// ActiveSession reports the recording that is switched on, if any.
func ActiveSession(dataRoot string) (Session, bool) {
	data, err := os.ReadFile(markerFile(dataRoot))
	if err != nil {
		return Session{}, false
	}
	var s Session
	if json.Unmarshal(data, &s) != nil || s.Name == "" || filepath.Base(s.Name) != s.Name {
		return Session{}, false
	}
	return s, true
}

// Start switches recording on in a new session folder named after now.
func Start(dataRoot string, now time.Time) (Session, error) {
	if s, ok := ActiveSession(dataRoot); ok {
		return s, fmt.Errorf("recording %s is already on", s.Name)
	}
	s := Session{Name: now.Format("2006-01-02_15-04-05"), StartedAt: now}
	if err := os.MkdirAll(s.Dir(dataRoot), 0o700); err != nil {
		return Session{}, err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return Session{}, err
	}
	tmp := markerFile(dataRoot) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return Session{}, err
	}
	return s, os.Rename(tmp, markerFile(dataRoot))
}

// Stop switches recording off and reports the session that ended.
func Stop(dataRoot string) (Session, error) {
	s, ok := ActiveSession(dataRoot)
	if !ok {
		return Session{}, errors.New("no recording is on")
	}
	return s, os.Remove(markerFile(dataRoot))
}

// Entry is one line of a recording.
type Entry struct {
	Seq     int64  `json:"seq"`
	Time    string `json:"t"`
	Proc    string `json:"proc"`
	PID     int    `json:"pid"`
	Channel string `json:"channel"`
	Dir     string `json:"dir"`
	// Peer names the other end when one process talks to several (which
	// app-server, which HTTP request).
	Peer string `json:"peer,omitempty"`
	// Raw holds the bytes exactly as sent; bytes that are not valid UTF-8
	// go to RawBase64 instead, so nothing is ever altered.
	Raw       string `json:"raw"`
	RawBase64 string `json:"rawBase64,omitempty"`
}

// recheckEvery bounds how stale a process's view of the switch may be.
const recheckEvery = time.Second

// Recorder appends one process's entries to the active session. A nil
// Recorder records nothing, so callers never need to check.
type Recorder struct {
	dataRoot string
	proc     string
	pid      int
	seq      atomic.Int64

	mu        sync.Mutex
	checkedAt time.Time
	session   string
	file      *os.File
}

// New returns the recorder of one process ("core", "mcp", "hook").
func New(dataRoot, proc string) *Recorder {
	if dataRoot == "" {
		return nil
	}
	return &Recorder{dataRoot: dataRoot, proc: proc, pid: os.Getpid()}
}

// Active reports whether a recording is on (checked at most once a second).
func (r *Recorder) Active() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refreshLocked(time.Now())
}

func (r *Recorder) refreshLocked(now time.Time) bool {
	if now.Sub(r.checkedAt) < recheckEvery && !r.checkedAt.IsZero() {
		return r.session != ""
	}
	r.checkedAt = now
	s, ok := ActiveSession(r.dataRoot)
	name := ""
	if ok {
		name = s.Name
	}
	if name != r.session {
		if r.file != nil {
			r.file.Close()
			r.file = nil
		}
		r.session = name
	}
	return r.session != ""
}

// Record appends one raw message. It never fails the caller: a recording
// that cannot be written is simply missing that entry.
func (r *Recorder) Record(channel, dir, peer string, raw []byte) {
	if r == nil {
		return
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.refreshLocked(now) {
		return
	}
	if r.file == nil {
		folder := filepath.Join(TracesDir(r.dataRoot), r.session)
		if err := os.MkdirAll(folder, 0o700); err != nil {
			return
		}
		file, err := os.OpenFile(filepath.Join(folder, fmt.Sprintf("%s-%d.jsonl", r.proc, r.pid)), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return
		}
		r.file = file
	}
	entry := Entry{Seq: r.seq.Add(1), Time: now.Format(time.RFC3339Nano), Proc: r.proc, PID: r.pid, Channel: channel, Dir: dir, Peer: peer}
	if utf8.Valid(raw) {
		entry.Raw = string(raw)
	} else {
		entry.RawBase64 = base64.StdEncoding.EncodeToString(raw)
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = r.file.Write(append(line, '\n'))
}

// Close releases the open file.
func (r *Recorder) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		r.file.Close()
		r.file = nil
	}
}
