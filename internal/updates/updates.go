// Package updates asks GitHub whether a newer CoBot release exists. It runs
// only when the user presses the button: nothing is sent at startup, and the
// request carries no identifier of the installation.
package updates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultURL lists the published releases, newest first. The listing is used
// rather than /releases/latest because that one skips pre-releases, and the
// published builds are still betas.
const DefaultURL = "https://api.github.com/repos/kunghim/CoBot/releases?per_page=20"

// releasePrefix is where a release page the user is sent to must live.
const releasePrefix = "https://github.com/kunghim/CoBot/"

const requestTimeout = 15 * time.Second

// State is what the check found.
type State string

const (
	// UpToDate: the installed version is the newest release or newer.
	UpToDate State = "upToDate"
	// Available: a newer release exists.
	Available State = "available"
	// Unknown: the installed version is not a release number (a development
	// build), so it cannot be compared; the newest release is still reported.
	Unknown State = "unknown"
	// Unreachable: GitHub could not be reached.
	Unreachable State = "unreachable"
	// Rejected: GitHub answered, but not with a release list.
	Rejected State = "rejected"
)

// Result is one check. Latest fields are empty when none was found.
type Result struct {
	State       State     `json:"state"`
	Current     string    `json:"current"`
	Latest      string    `json:"latest,omitempty"`
	ReleaseURL  string    `json:"releaseUrl,omitempty"`
	PublishedAt time.Time `json:"publishedAt,omitempty"`
	CheckedAt   time.Time `json:"checkedAt"`
}

// Checker compares the installed version with the newest release.
type Checker struct {
	client  *http.Client
	url     string
	current string
	now     func() time.Time
}

// New builds a checker for the installed version; an empty url means GitHub.
func New(current, listURL string) *Checker {
	if listURL == "" {
		listURL = DefaultURL
	}
	return &Checker{client: &http.Client{Timeout: requestTimeout}, url: listURL, current: current, now: time.Now}
}

type release struct {
	Tag         string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
}

// Check asks for the release list and reports how the installed version
// stands against it. A failure is a state, not an error: the page shows it.
func (c *Checker) Check(ctx context.Context) Result {
	out := Result{Current: c.current, CheckedAt: c.now().UTC()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		out.State = Unreachable
		return out
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CoBot/"+c.current)
	resp, err := c.client.Do(req)
	if err != nil {
		out.State = Unreachable
		return out
	}
	defer resp.Body.Close()
	var releases []release
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&releases) != nil {
		out.State = Rejected
		return out
	}
	newest, ok := newestRelease(releases)
	if !ok {
		out.State = Rejected
		return out
	}
	out.Latest = strings.TrimPrefix(newest.Tag, "v")
	out.ReleaseURL = newest.HTMLURL
	out.PublishedAt = newest.PublishedAt
	have, haveOK := parseVersion(c.current)
	want, _ := parseVersion(newest.Tag)
	switch {
	case !haveOK:
		out.State = Unknown
	case compare(want, have) > 0:
		out.State = Available
	default:
		out.State = UpToDate
	}
	return out
}

// newestRelease is the highest version among the published, well-formed
// releases whose page lies on the project's own repository.
func newestRelease(releases []release) (release, bool) {
	var best release
	var bestVersion version
	found := false
	for _, r := range releases {
		v, ok := parseVersion(r.Tag)
		if r.Draft || !ok || !onProject(r.HTMLURL) {
			continue
		}
		if !found || compare(v, bestVersion) > 0 {
			best, bestVersion, found = r, v, true
		}
	}
	return best, found
}

func onProject(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.User == nil && strings.HasPrefix(raw, releasePrefix)
}

// version is a semantic version: three numbers and optional pre-release parts.
type version struct {
	core [3]int
	pre  []string
}

// parseVersion reads 1.2.3 or v1.2.3-beta.2. A build suffix after "+" is
// ignored; anything else is not a release number.
func parseVersion(raw string) (version, bool) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "v")
	raw, _, _ = strings.Cut(raw, "+")
	core, pre, hasPre := strings.Cut(raw, "-")
	parts := strings.Split(core, ".")
	var v version
	if len(parts) != 3 {
		return v, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return v, false
		}
		v.core[i] = n
	}
	if hasPre {
		// A local build is stamped 0.1.0-dev or 0.1.0-beta.2-dev: it came from a
		// working tree, not from a release, so it has no place in the order.
		if pre == "" || pre == "dev" || strings.HasSuffix(pre, "-dev") {
			return v, false
		}
		v.pre = strings.Split(pre, ".")
	}
	return v, true
}

// compare orders two versions: a release is above its own pre-releases, and
// pre-release parts compare as numbers when both are numbers.
func compare(a, b version) int {
	for i := range a.core {
		if a.core[i] != b.core[i] {
			return sign(a.core[i] - b.core[i])
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := comparePart(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return sign(len(a.pre) - len(b.pre))
}

func comparePart(a, b string) int {
	na, errA := strconv.Atoi(a)
	nb, errB := strconv.Atoi(b)
	switch {
	case errA == nil && errB == nil:
		return sign(na - nb)
	case errA == nil:
		return -1 // numbers rank below words
	case errB == nil:
		return 1
	}
	return sign(strings.Compare(a, b))
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}
