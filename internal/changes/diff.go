// Package changes describes recorded patches without reading the current disk
// or relying on Git. Statistics describe the complete patch received by CoBot.
package changes

import (
	"regexp"
	"strconv"
	"strings"
)

type Stats struct {
	Additions     int  `json:"additions"`
	Deletions     int  `json:"deletions"`
	StatsComplete bool `json:"statsComplete"`
	Binary        bool `json:"binary,omitempty"`
}

// File offsets locate the file's portion of a turn diff before storage clipping.
type File struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	MovePath      string `json:"movePath,omitempty"`
	DiffTruncated bool   `json:"diffTruncated"`
	Start         int    `json:"start"`
	End           int    `json:"end"`
	Stats
}

var hunkHeader = regexp.MustCompile(`^@@ -[0-9]+(?:,([0-9]+))? \+[0-9]+(?:,([0-9]+))? @@`)

// Count validates hunk lengths, including lines whose actual content begins
// with ++/--. Only headers outside hunks are excluded from line counts.
func Count(diff string) Stats {
	stats := Stats{StatsComplete: true}
	structured := strings.HasPrefix(diff, "diff --git ") || strings.HasPrefix(diff, "@@ ") || strings.Contains(diff, "\n@@ ")
	inHunk, old, next := false, 0, 0
	for rest := diff; rest != ""; {
		line, tail, _ := strings.Cut(rest, "\n")
		rest = tail
		line = strings.TrimSuffix(line, "\r")
		if match := hunkHeader.FindStringSubmatch(line); match != nil {
			if inHunk && (old != 0 || next != 0) {
				stats.StatsComplete = false
			}
			old, next = 1, 1
			if match[1] != "" {
				old, _ = strconv.Atoi(match[1])
			}
			if match[2] != "" {
				next, _ = strconv.Atoi(match[2])
			}
			inHunk = true
			continue
		}
		if strings.HasPrefix(line, "Binary files ") || line == "GIT binary patch" {
			stats.Binary, stats.StatsComplete = true, false
			continue
		}
		if stats.Binary {
			continue
		}
		if strings.HasPrefix(line, "\\ No newline at end of file") {
			continue
		}
		if !inHunk && (line == "" || isHeader(line)) {
			continue
		}
		if !inHunk && (strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ")) {
			if structured {
				continue
			}
			// A headerless patch can contain literal ++/-- source text, but
			// an isolated file header has the same spelling. Keep it unknown.
			stats.StatsComplete = false
		}
		switch {
		case strings.HasPrefix(line, "+"):
			stats.Additions++
			if inHunk {
				next--
			}
		case strings.HasPrefix(line, "-"):
			stats.Deletions++
			if inHunk {
				old--
			}
		case strings.HasPrefix(line, " "):
			if inHunk {
				old--
				next--
			}
		default:
			stats.StatsComplete = false
		}
		if inHunk && (old < 0 || next < 0) {
			stats.StatsComplete = false
		}
	}
	if inHunk && (old != 0 || next != 0) {
		stats.StatsComplete = false
	}
	return stats
}

func isHeader(line string) bool {
	for _, prefix := range []string{"diff --git ", "index ", "new file mode ", "deleted file mode ", "old mode ", "new mode ", "similarity index ", "dissimilarity index ", "rename from ", "rename to ", "copy from ", "copy to "} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// ParseTurn preserves byte offsets and quoted Git paths. It never treats an
// arbitrary filename mentioned in patch contents as a file header.
func ParseTurn(diff string) ([]File, bool) {
	files := []File{}
	complete := true
	start := -1
	finish := func(end int) {
		if start < 0 {
			return
		}
		section := diff[start:end]
		file := parseFile(section)
		file.Start, file.End, file.Stats = start, end, Count(section)
		if file.Path == "" {
			complete = false
			return
		}
		files = append(files, file)
	}
	for position := 0; position < len(diff); {
		line, _, found := strings.Cut(diff[position:], "\n")
		if strings.HasPrefix(line, "diff --git ") {
			finish(position)
			start = position
		} else if start < 0 && strings.TrimSpace(line) != "" {
			complete = false
		}
		position += len(line)
		if found {
			position++
		}
	}
	finish(len(diff))
	return files, complete
}

func parseFile(diff string) File {
	file := File{Kind: "update"}
	var before, after string
	copied := false
	for rest := diff; rest != ""; {
		line, tail, _ := strings.Cut(rest, "\n")
		rest = tail
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "@@") {
			break
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			before, after = headerPaths(strings.TrimPrefix(line, "diff --git "))
		case strings.HasPrefix(line, "--- "):
			before = gitPath(strings.TrimPrefix(line, "--- "), true)
		case strings.HasPrefix(line, "+++ "):
			after = gitPath(strings.TrimPrefix(line, "+++ "), true)
		case strings.HasPrefix(line, "rename from "):
			before = gitPath(strings.TrimPrefix(line, "rename from "), false)
		case strings.HasPrefix(line, "rename to "):
			after = gitPath(strings.TrimPrefix(line, "rename to "), false)
		case strings.HasPrefix(line, "copy from "):
			before, copied = gitPath(strings.TrimPrefix(line, "copy from "), false), true
		case strings.HasPrefix(line, "copy to "):
			after, copied = gitPath(strings.TrimPrefix(line, "copy to "), false), true
		case strings.HasPrefix(line, "new file mode "):
			file.Kind = "add"
		case strings.HasPrefix(line, "deleted file mode "):
			file.Kind = "delete"
		}
	}
	if before == "/dev/null" {
		file.Kind, before = "add", ""
	}
	if after == "/dev/null" {
		file.Kind, after = "delete", ""
	}
	file.Path = before
	if file.Path == "" {
		file.Path = after
	}
	if copied {
		file.Path, file.Kind = after, "add"
		return file
	}
	if before != "" && after != "" && before != after {
		file.MovePath = after
	}
	return file
}

func gitPath(value string, prefixed bool) string {
	if strings.HasPrefix(value, `"`) {
		if quoted, err := strconv.QuotedPrefix(value); err == nil {
			if decoded, err := strconv.Unquote(quoted); err == nil {
				value = decoded
			}
		}
	} else if prefixed {
		value, _, _ = strings.Cut(value, "\t")
	}
	if prefixed && (strings.HasPrefix(value, "a/") || strings.HasPrefix(value, "b/")) {
		return value[2:]
	}
	return value
}

func headerPaths(value string) (string, string) {
	if strings.HasPrefix(value, `"`) {
		if quoted, err := strconv.QuotedPrefix(value); err == nil {
			return gitPath(quoted, true), gitPath(strings.TrimSpace(value[len(quoted):]), true)
		}
	}
	for offset := 0; offset < len(value); {
		index := strings.Index(value[offset:], " b/")
		if index < 0 {
			break
		}
		index += offset
		before, after := gitPath(value[:index], true), gitPath(value[index+1:], true)
		if before == after {
			return before, after
		}
		offset = index + 1
	}
	if index := strings.Index(value, " b/"); index >= 0 {
		return gitPath(value[:index], true), gitPath(value[index+1:], true)
	}
	return "", ""
}
