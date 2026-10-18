package wiretrace

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

//go:embed viewer.html
var viewerPage []byte

// Group sorts a row into one of the switches of the viewer's filter bar.
func Group(row Row) string {
	switch {
	case row.Channel == "codex.stdio":
		return "codex"
	case strings.HasPrefix(row.Channel, "mcp."):
		return "mcp"
	case strings.HasPrefix(row.Channel, "hook."):
		return "hook"
	case row.Channel == "codex.stderr":
		return "log"
	case row.Channel == "http":
		return "http"
	case row.Channel == "codex.rollout":
		return "rollout"
	default:
		return "process"
	}
}

// Viewer serves one recording to a local browser page: the overview, a
// filtered time line paged on demand, and any row's raw bytes.
type Viewer struct {
	idx     *Index
	summary Summary

	mu    sync.Mutex
	lists map[string][]listItem
}

// listItem is one line of a filtered time line: a row, or a run of
// consecutive streaming deltas folded into one line.
type listItem struct {
	row     int
	members []int
}

// NewViewer prepares a viewer for an index and its overview.
func NewViewer(idx *Index, summary Summary) *Viewer {
	return &Viewer{idx: idx, summary: summary, lists: map[string][]listItem{}}
}

type filter struct {
	groups  map[string]bool
	who     string
	query   string
	fold    bool
	anomaly bool
	key     string
}

func readFilter(r *http.Request) filter {
	q := r.URL.Query()
	f := filter{groups: map[string]bool{}, who: q.Get("who"), query: strings.ToLower(q.Get("q")), fold: q.Get("fold") == "1", anomaly: q.Get("anomaly") == "1"}
	for _, g := range strings.Split(q.Get("groups"), ",") {
		if g != "" {
			f.groups[g] = true
		}
	}
	f.key = strings.Join([]string{q.Get("groups"), f.who, f.query, q.Get("fold"), q.Get("anomaly")}, "\x00")
	return f
}

func (f filter) match(row Row) bool {
	if !f.groups[Group(row)] {
		return false
	}
	if f.who != "" && row.Who != f.who {
		return false
	}
	if f.anomaly && row.Anomaly == "" {
		return false
	}
	if f.query != "" {
		text := strings.ToLower(row.Kind + " " + row.Summary + " " + row.From + " " + row.To)
		if !strings.Contains(text, f.query) {
			return false
		}
	}
	return true
}

func (v *Viewer) list(f filter) []listItem {
	v.mu.Lock()
	defer v.mu.Unlock()
	if list, ok := v.lists[f.key]; ok {
		return list
	}
	var list []listItem
	for i, row := range v.idx.Rows {
		if !f.match(row) {
			continue
		}
		if f.fold && row.Fold != "" && len(list) > 0 {
			last := &list[len(list)-1]
			if v.idx.Rows[last.row].Fold == row.Fold {
				last.members = append(last.members, i)
				continue
			}
		}
		item := listItem{row: i}
		if f.fold && row.Fold != "" {
			item.members = []int{i}
		}
		list = append(list, item)
	}
	if len(v.lists) > 32 {
		v.lists = map[string][]listItem{}
	}
	v.lists[f.key] = list
	return list
}

type listRow struct {
	Row
	Group  string `json:"group"`
	Folded int    `json:"folded,omitempty"`
}

func (v *Viewer) rowOut(item listItem) listRow {
	row := v.idx.Rows[item.row]
	out := listRow{Row: row, Group: Group(row)}
	if len(item.members) > 1 {
		// A folded run reads as the text its deltas spell out together.
		out.Folded = len(item.members)
		var text strings.Builder
		for _, member := range item.members {
			text.WriteString(v.idx.Rows[member].Summary)
			if text.Len() > 400 {
				break
			}
		}
		out.Summary = clip(text.String(), 160)
	}
	return out
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}

// Handler serves the page and its API. Requests must name the viewer's own
// loopback address, so no other site can read a recording through it.
func (v *Viewer) Handler(host string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(viewerPage)
	})
	mux.HandleFunc("GET /api/summary", func(w http.ResponseWriter, r *http.Request) {
		groups := map[string]int{}
		for _, row := range v.idx.Rows {
			groups[Group(row)]++
		}
		writeJSON(w, map[string]any{"summary": v.summary, "groups": groups})
	})
	mux.HandleFunc("GET /api/rows", func(w http.ResponseWriter, r *http.Request) {
		list := v.list(readFilter(r))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 500 {
			limit = 200
		}
		offset = max(0, min(offset, len(list)))
		end := min(len(list), offset+limit)
		rows := make([]listRow, 0, end-offset)
		for _, item := range list[offset:end] {
			rows = append(rows, v.rowOut(item))
		}
		writeJSON(w, map[string]any{"total": len(list), "offset": offset, "rows": rows})
	})
	mux.HandleFunc("GET /api/locate", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.URL.Query().Get("id"))
		position := -1
		for i, item := range v.list(readFilter(r)) {
			if item.row == id || (len(item.members) > 1 && containsInt(item.members, id)) {
				position = i
				break
			}
		}
		writeJSON(w, map[string]any{"index": position})
	})
	mux.HandleFunc("GET /api/group", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.URL.Query().Get("id"))
		var rows []listRow
		for _, item := range v.list(readFilter(r)) {
			if item.row == id {
				for _, member := range item.members {
					rows = append(rows, v.rowOut(listItem{row: member}))
				}
				break
			}
		}
		writeJSON(w, map[string]any{"rows": rows})
	})
	mux.HandleFunc("GET /api/raw", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.URL.Query().Get("id"))
		raw, err := v.idx.Raw(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		out := map[string]any{"row": v.rowOut(listItem{row: id}), "bytes": len(raw)}
		if utf8.Valid(raw) {
			out["raw"] = string(raw)
			var pretty bytes.Buffer
			if json.Indent(&pretty, raw, "", "  ") == nil {
				out["pretty"] = pretty.String()
			}
		} else {
			out["binary"] = true
		}
		writeJSON(w, out)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func containsInt(list []int, value int) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
