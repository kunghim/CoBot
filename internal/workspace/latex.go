package workspace

import (
	"context"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type LatexProject struct {
	Candidates []File `json:"candidates"`
	Suggested  string `json:"suggested"`
	Truncated  bool   `json:"truncated"`
}

// DiscoverLatex suggests main files but never executes TeX or treats a textual
// directive as authority to access files outside the workspace.
func DiscoverLatex(ctx context.Context, directory, current string) (LatexProject, error) {
	result := LatexProject{Candidates: []File{}}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	selected, err := Open(directory, current)
	if err != nil {
		return result, err
	}
	if selected.Info.Kind != "latex" {
		selected.Close()
		return result, fs.ErrInvalid
	}
	selectedInfo := selected.Info
	text, err := io.ReadAll(io.LimitReader(selected.Handle, 1<<20))
	selected.Close()
	if err != nil {
		return result, err
	}
	text, err = latexUTF8Prefix(text, selectedInfo.Size > int64(len(text)))
	if err != nil {
		return result, err
	}
	files := make(map[string]File)
	if latexDocument(string(text)) {
		files[selectedInfo.Path] = selectedInfo
		result.Suggested = selectedInfo.Path
	}
	if hint := latexRoot(string(text)); hint != "" {
		target := hint
		if !filepath.IsAbs(hint) && !path.IsAbs(hint) && !strings.HasPrefix(strings.ToLower(hint), "file:") && !(len(hint) >= 2 && hint[1] == ':') {
			target = path.Join(path.Dir(selectedInfo.Path), hint)
		}
		candidate, err := Open(directory, target)
		if err != nil {
			return result, err
		}
		if candidate.Info.Kind != "latex" {
			candidate.Close()
			return result, fs.ErrInvalid
		}
		files[candidate.Info.Path] = candidate.Info
		result.Suggested = candidate.Info.Path
		candidate.Close()
	}
	queue := []string{""}
	seen := make(map[string]bool)
	visited := 0
	depthLimited := false
	budget := 8 << 20
	for len(queue) > 0 && !result.Truncated {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		next := queue[0]
		queue = queue[1:]
		first, err := List(ctx, directory, next, "", false)
		if err != nil {
			continue
		}
		if seen[first.Path] {
			continue
		}
		seen[first.Path] = true
		page := first
		for {
			for _, entry := range page.Entries {
				visited++
				if visited > 2000 {
					result.Truncated = true
					break
				}
				if entry.Kind == "directory" {
					if entry.Name != "node_modules" && entry.Name != "vendor" && entry.Name != "dist" {
						if strings.Count(entry.Path, "/") < 8 {
							queue = append(queue, entry.Path)
						} else {
							depthLimited = true
						}
					}
					continue
				}
				if !strings.EqualFold(path.Ext(entry.Name), ".tex") || entry.Kind == "blocked" {
					continue
				}
				if _, ok := files[entry.Path]; ok {
					continue
				}
				file, err := Open(directory, entry.Path)
				if err != nil {
					continue
				}
				data, err := io.ReadAll(io.LimitReader(file.Handle, int64(min(1<<20, budget))))
				file.Close()
				budget -= len(data)
				data, encodingErr := latexUTF8Prefix(data, file.Info.Size > int64(len(data)))
				if err == nil && encodingErr == nil && latexDocument(string(data)) {
					files[file.Info.Path] = file.Info
				}
				if budget <= 0 {
					result.Truncated = true
					break
				}
			}
			if result.Truncated || page.NextCursor == "" {
				break
			}
			page, err = List(ctx, directory, next, page.NextCursor, false)
			if err != nil {
				break
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Truncated = result.Truncated || depthLimited
	for _, file := range files {
		result.Candidates = append(result.Candidates, file)
	}
	sort.Slice(result.Candidates, func(i, j int) bool { return result.Candidates[i].Path < result.Candidates[j].Path })
	if result.Suggested == "" && len(result.Candidates) == 1 && !result.Truncated {
		result.Suggested = result.Candidates[0].Path
	}
	return result, nil
}

func latexRoot(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "%") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "%"))
		if !strings.HasPrefix(line, "!") {
			continue
		}
		line = strings.TrimSpace(line[1:])
		left, right, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.Join(strings.Fields(left), " "), "TEX root") {
			continue
		}
		return strings.Trim(strings.TrimSpace(right), "\"'")
	}
	return ""
}

func latexDocument(text string) bool {
	var clean strings.Builder
	comment := false
	escaped := false
	for _, char := range text {
		if comment {
			if char == '\n' || char == '\r' {
				comment = false
				clean.WriteRune(char)
			}
			continue
		}
		if char == '%' && !escaped {
			comment = true
			continue
		}
		clean.WriteRune(char)
		if char == '\\' {
			escaped = !escaped
		} else {
			escaped = false
		}
	}
	text = clean.String()
	for {
		index := strings.Index(text, `\documentclass`)
		if index < 0 {
			return false
		}
		rest := strings.TrimSpace(text[index+len(`\documentclass`):])
		if strings.HasPrefix(rest, "{") || strings.HasPrefix(rest, "[") {
			return true
		}
		text = rest
	}
}

func latexUTF8Prefix(data []byte, truncated bool) ([]byte, error) {
	if utf8.Valid(data) {
		return data, nil
	}
	if truncated {
		for tail := 1; tail <= 3 && tail < len(data); tail++ {
			if utf8.Valid(data[:len(data)-tail]) {
				return data[:len(data)-tail], nil
			}
		}
	}
	return nil, fs.ErrInvalid
}
