// Package avatars keeps this profile's copy of the avatar catalog. The
// catalog is not part of the app: it is published under assets/avatars in the
// CoBot repository and downloaded into the data root, so it can grow and
// change between releases. A sync never breaks what is cached: anything
// unexpected keeps the previous copy and is reported, never guessed around.
package avatars

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is where the published catalog lives; config.json's
// assetsBaseUrl replaces it (a mirror, the maintainer's own storage).
const DefaultBaseURL = "https://raw.githubusercontent.com/kunghim/CoBot/main/assets/"

// SchemaVersion is the catalog format this build reads. A newer index or
// series file is skipped, keeping the cached copy, instead of misread.
const SchemaVersion = 1

const (
	maxFileBytes   = 1 << 20
	requestTimeout = 15 * time.Second
)

var seriesID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// IndexEntry is one series as index.json lists it.
type IndexEntry struct {
	ID      string            `json:"id"`
	Name    map[string]string `json:"name"`
	Kind    string            `json:"kind"`
	Version int               `json:"version"`
	File    string            `json:"file"`
	SHA256  string            `json:"sha256"`
	Size    int64             `json:"size"`
}

type index struct {
	SchemaVersion int          `json:"schemaVersion"`
	Series        []IndexEntry `json:"series"`
}

// Series is a cached series: its index entry and the file's content.
type Series struct {
	IndexEntry
	Data json.RawMessage
}

// Status reports the last sync of this profile's catalog.
type Status struct {
	Syncing    bool      `json:"syncing"`
	LastSyncAt time.Time `json:"lastSyncAt,omitzero"`
	// LastError is a short reason when the last sync did not complete.
	LastError string `json:"lastError,omitempty"`
}

// Catalog is one profile's cached avatar catalog.
type Catalog struct {
	dir     string
	baseURL string
	client  *http.Client

	syncMu sync.Mutex // one sync at a time
	mu     sync.Mutex
	status Status
}

// New returns the catalog cached under dir, fetched from baseURL. The base
// must be HTTPS, except a loopback address used for tests and local mirrors.
func New(dir, baseURL string) (*Catalog, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("avatar catalog address %q is not a URL", baseURL)
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopback(parsed.Hostname())) {
		return nil, fmt.Errorf("avatar catalog address %q must use https", baseURL)
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return &Catalog{dir: dir, baseURL: baseURL, client: &http.Client{Timeout: requestTimeout}}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Status returns the state of the last sync.
func (c *Catalog) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Load returns the cached series in index order. A series whose file is
// missing or unreadable is left out; nothing is fetched.
func (c *Catalog) Load() ([]Series, error) {
	cached, err := c.readIndex()
	if err != nil {
		return nil, err
	}
	out := make([]Series, 0, len(cached.Series))
	for _, entry := range cached.Series {
		data, err := os.ReadFile(c.seriesPath(entry.ID, entry.Version))
		if err != nil || !json.Valid(data) {
			continue
		}
		out = append(out, Series{IndexEntry: entry, Data: data})
	}
	return out, nil
}

// Sync fetches the published index and downloads every series that is newer
// than the cached one. It returns whether anything changed. On any failure
// the cached copy stays as it was.
func (c *Catalog) Sync(ctx context.Context) (changed bool, err error) {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	c.setStatus(func(s *Status) { s.Syncing = true })
	defer func() {
		c.setStatus(func(s *Status) {
			s.Syncing = false
			s.LastSyncAt = time.Now().UTC()
			s.LastError = ""
			if err != nil {
				s.LastError = err.Error()
			}
		})
	}()

	raw, err := c.fetch(ctx, "avatars/index.json", maxFileBytes)
	if err != nil {
		return false, err
	}
	var published index
	if err := json.Unmarshal(raw, &published); err != nil {
		return false, fmt.Errorf("published index is not valid JSON: %w", err)
	}
	if published.SchemaVersion != SchemaVersion {
		return false, fmt.Errorf("published index uses format %d; this CoBot reads %d", published.SchemaVersion, SchemaVersion)
	}
	cached, err := c.readIndex()
	if err != nil {
		return false, err
	}
	have := map[string]IndexEntry{}
	for _, entry := range cached.Series {
		have[entry.ID] = entry
	}

	next := index{SchemaVersion: SchemaVersion}
	var problems []string
	for _, entry := range published.Series {
		if err := checkEntry(entry); err != nil {
			problems = append(problems, err.Error())
			if old, ok := have[entry.ID]; ok {
				next.Series = append(next.Series, old)
			}
			continue
		}
		if old, ok := have[entry.ID]; ok && old.Version >= entry.Version {
			next.Series = append(next.Series, old)
			continue
		}
		if err := c.download(ctx, entry); err != nil {
			problems = append(problems, err.Error())
			if old, ok := have[entry.ID]; ok {
				next.Series = append(next.Series, old)
			}
			continue
		}
		next.Series = append(next.Series, entry)
		changed = true
	}
	// A series no longer published stays usable: avatars already chosen from
	// it keep drawing.
	listed := map[string]bool{}
	for _, entry := range next.Series {
		listed[entry.ID] = true
	}
	for _, entry := range cached.Series {
		if !listed[entry.ID] {
			next.Series = append(next.Series, entry)
		}
	}
	if changed {
		if err := c.writeIndex(next); err != nil {
			return false, err
		}
		c.prune(next)
	}
	if len(problems) > 0 {
		return changed, errors.New(strings.Join(problems, "; "))
	}
	return changed, nil
}

// checkEntry rejects index entries this build cannot store safely or read.
func checkEntry(entry IndexEntry) error {
	if !seriesID.MatchString(entry.ID) {
		return fmt.Errorf("series id %q is not allowed", entry.ID)
	}
	if entry.Kind != "composed" {
		return fmt.Errorf("series %s: kind %q is not supported by this CoBot", entry.ID, entry.Kind)
	}
	if entry.Version < 1 {
		return fmt.Errorf("series %s: version must be positive", entry.ID)
	}
	clean := path.Clean(entry.File)
	if entry.File == "" || clean != entry.File || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "..") || strings.Contains(clean, ":") {
		return fmt.Errorf("series %s: file %q is not a relative path", entry.ID, entry.File)
	}
	if entry.Size <= 0 || entry.Size > maxFileBytes {
		return fmt.Errorf("series %s: size %d is out of range", entry.ID, entry.Size)
	}
	if len(entry.SHA256) != 64 {
		return fmt.Errorf("series %s: sha256 is missing", entry.ID)
	}
	return nil
}

func (c *Catalog) download(ctx context.Context, entry IndexEntry) error {
	data, err := c.fetch(ctx, "avatars/"+entry.File, entry.Size)
	if err != nil {
		return fmt.Errorf("series %s: %w", entry.ID, err)
	}
	if int64(len(data)) != entry.Size {
		return fmt.Errorf("series %s: got %d bytes, index says %d", entry.ID, len(data), entry.Size)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != entry.SHA256 {
		return fmt.Errorf("series %s: content does not match its sha256", entry.ID)
	}
	var head struct {
		SchemaVersion int    `json:"schemaVersion"`
		ID            string `json:"id"`
		Version       int    `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return fmt.Errorf("series %s: not valid JSON", entry.ID)
	}
	if head.SchemaVersion != SchemaVersion || head.ID != entry.ID || head.Version != entry.Version {
		return fmt.Errorf("series %s: file does not match its index entry", entry.ID)
	}
	return writeAtomic(c.seriesPath(entry.ID, entry.Version), data)
}

// fetch reads one published file, refusing anything larger than limit.
func (c *Catalog) fetch(ctx context.Context, rel string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+rel, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rel, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", rel, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rel, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s: larger than %d bytes", rel, limit)
	}
	return data, nil
}

func (c *Catalog) seriesPath(id string, version int) string {
	return filepath.Join(c.dir, id, strconv.Itoa(version)+".json")
}

func (c *Catalog) readIndex() (index, error) {
	data, err := os.ReadFile(filepath.Join(c.dir, "index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return index{SchemaVersion: SchemaVersion}, nil
	}
	if err != nil {
		return index{}, fmt.Errorf("read cached avatar index: %w", err)
	}
	var cached index
	if err := json.Unmarshal(data, &cached); err != nil || cached.SchemaVersion != SchemaVersion {
		// An unreadable cache is replaced by the next good sync.
		return index{SchemaVersion: SchemaVersion}, nil
	}
	return cached, nil
}

func (c *Catalog) writeIndex(next index) error {
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(c.dir, "index.json"), append(data, '\n'))
}

// prune keeps the current and the previous version of every series.
func (c *Catalog) prune(current index) {
	for _, entry := range current.Series {
		files, err := os.ReadDir(filepath.Join(c.dir, entry.ID))
		if err != nil {
			continue
		}
		var versions []int
		for _, file := range files {
			if v, err := strconv.Atoi(strings.TrimSuffix(file.Name(), ".json")); err == nil && strings.HasSuffix(file.Name(), ".json") {
				versions = append(versions, v)
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(versions)))
		kept := 0
		for _, v := range versions {
			if v > entry.Version {
				continue // not current; never delete what the index may point to next
			}
			kept++
			if kept > 2 {
				_ = os.Remove(c.seriesPath(entry.ID, v))
			}
		}
	}
}

func (c *Catalog) setStatus(change func(*Status)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	change(&c.status)
}

func writeAtomic(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, target); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
