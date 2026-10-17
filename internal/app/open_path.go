package app

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/opener"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// lineSuffix matches the position members append to file links:
// "#L12", "#L12-L20", ":12" or ":12:5".
var lineSuffix = regexp.MustCompile(`(#L\d+(-L?\d+)?|:\d+(:\d+)?)$`)

// OpenTeamPath opens (or reveals) a file a member linked in the
// conversation. Only paths inside the team's own folder are opened; the
// link text comes from the model, so everything else is refused.
func (a *App) OpenTeamPath(ctx context.Context, principal auth.Principal, teamID, raw string, reveal bool) (map[string]any, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	team, _, err := sqlite.GetTeam(ctx, a.DB, teamID)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	path, ok := resolveTeamPath(team.Directory, raw)
	if !ok {
		return nil, domain.InvalidRequest("error.path.outside_team", nil)
	}
	launch := a.OpenLocal
	if launch == nil {
		launch = func(path string, reveal bool) error {
			if reveal {
				return opener.Reveal(path)
			}
			return opener.Open(path)
		}
	}
	if err := launch(path, reveal); err != nil {
		return nil, domain.CapabilityMissing("error.path.open_failed", nil, err)
	}
	return map[string]any{"path": path}, nil
}

// resolveTeamPath turns a link target into an existing path inside dir.
func resolveTeamPath(dir, raw string) (string, bool) {
	target := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(target), "file://") {
		if parsed, err := url.Parse(target); err == nil {
			target = parsed.Path
		}
	} else if decoded, err := url.PathUnescape(target); err == nil {
		target = decoded
	}
	// Markdown links write Windows paths as "/E:/dir/file".
	if len(target) >= 3 && target[0] == '/' && target[2] == ':' {
		target = target[1:]
	}
	if target == "" || dir == "" {
		return "", false
	}
	candidates := []string{target}
	if stripped := lineSuffix.ReplaceAllString(target, ""); stripped != target {
		candidates = append(candidates, stripped)
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	for _, candidate := range candidates {
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(dir, candidate)
		}
		real, err := filepath.EvalSymlinks(filepath.Clean(candidate))
		if err != nil {
			continue
		}
		if _, err := os.Stat(real); err != nil {
			continue
		}
		if within(root, real) {
			return real, true
		}
		return "", false
	}
	return "", false
}

func within(root, path string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		root, path = strings.ToLower(root), strings.ToLower(path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
