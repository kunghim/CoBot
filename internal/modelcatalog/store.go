package modelcatalog

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

//go:embed bundled.json
var bundled []byte

// maxDownload bounds what is read of a download; the dataset is a few megabytes.
const maxDownload = 64 << 20

const requestTimeout = 60 * time.Second

// Status says which catalog is in use.
type Status struct {
	// Source is "bundled" (the one shipped with the program) or "downloaded".
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updatedAt"`
	Models    int       `json:"models"`
	From      string    `json:"from"`
}

// Failure is why an update did not happen.
type Failure string

const (
	// Unreachable: the download did not complete.
	Unreachable Failure = "unreachable"
	// Rejected: the other side answered with something that is not the dataset.
	Rejected Failure = "rejected"
)

// UpdateError is an update that did not happen; the catalog in use is unchanged.
type UpdateError struct{ Kind Failure }

func (e *UpdateError) Error() string { return "model catalog update failed: " + string(e.Kind) }

// Store holds the catalog in use and the file a downloaded one is kept in.
type Store struct {
	mu      sync.RWMutex
	file    string
	url     string
	client  *http.Client
	current *Catalog
	source  string
}

// Open loads the downloaded catalog if the file holds a readable one, and the
// bundled one otherwise. A file that cannot be read is not an error: the
// bundled catalog is always there.
func Open(file string) (*Store, error) {
	base, err := Parse(bundled)
	if err != nil {
		return nil, fmt.Errorf("the bundled model catalog is unreadable: %w", err)
	}
	store := &Store{file: file, url: SourceURL, client: &http.Client{Timeout: requestTimeout}, current: base, source: "bundled"}
	if file != "" {
		if raw, err := os.ReadFile(file); err == nil {
			if downloaded, err := Parse(raw); err == nil {
				store.current, store.source = downloaded, "downloaded"
			}
		}
	}
	return store, nil
}

// SetSourceURL points updates somewhere else; tests use it.
func (s *Store) SetSourceURL(url string) { s.url = url }

// Match finds a model in the catalog in use.
func (s *Store) Match(id string) (Entry, bool) {
	if s == nil {
		return Entry{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.Match(id)
}

// Status describes the catalog in use.
func (s *Store) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Status{Source: s.source, UpdatedAt: s.current.FetchedAt, Models: len(s.current.Models), From: s.current.Source}
}

// Update downloads the dataset, converts it and keeps it. Until the new
// catalog is converted and written, the old one stays in use, and a failure at
// any step leaves it so.
func (s *Store) Update(ctx context.Context, now time.Time) (Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return s.Status(), &UpdateError{Kind: Unreachable}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CoBot")
	resp, err := s.client.Do(req)
	if err != nil {
		return s.Status(), &UpdateError{Kind: Unreachable}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return s.Status(), &UpdateError{Kind: Rejected}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload))
	if err != nil {
		return s.Status(), &UpdateError{Kind: Unreachable}
	}
	next, err := Convert(raw, now)
	if err != nil {
		return s.Status(), &UpdateError{Kind: Rejected}
	}
	if s.file != "" {
		encoded, err := next.Encode()
		if err != nil {
			return s.Status(), err
		}
		if err := writeAtomically(s.file, encoded); err != nil {
			return s.Status(), err
		}
	}
	s.mu.Lock()
	s.current, s.source = next, "downloaded"
	s.mu.Unlock()
	return s.Status(), nil
}

// writeAtomically replaces the file whole or not at all.
func writeAtomically(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".catalog-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// IsUpdateError reports whether err is a failed update and which kind.
func IsUpdateError(err error) (Failure, bool) {
	var update *UpdateError
	if errors.As(err, &update) {
		return update.Kind, true
	}
	return "", false
}
