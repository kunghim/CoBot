package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

const ChangeRunPageSize = 20
const ChangeFilePageSize = 100

// ChangeSource is chosen by the application. LegacyDetail is a bounded,
// reconstructed metadata-only snapshot when an old turn has no structured files.
type ChangeSource struct {
	Kind          string
	Seq           int
	FilesComplete bool
	LegacyDetail  json.RawMessage
}

func changeStepTable(chat bool) (table, kind, status, detail string) {
	if chat {
		return "chat_run_steps", "json_extract(data,'$.kind')", "json_extract(data,'$.status')", "json_extract(data,'$.detail')"
	}
	return "run_steps", "kind", "status", "detail"
}

func changeRunColumns(chat bool) string {
	if chat {
		return `id, '', status, json_extract(data,'$.startedAt'), COALESCE(json_extract(data,'$.endedAt'),'')`
	}
	return `id, bot_id, status, started_at, ended_at`
}

func scanChangeRun(row interface{ Scan(...any) error }) (domain.WorkspaceChangeRun, error) {
	var run domain.WorkspaceChangeRun
	var start, end string
	err := row.Scan(&run.ID, &run.BotID, &run.Status, &start, &end)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err == nil {
		run.StartedAt, _ = time.Parse(time.RFC3339Nano, start)
		run.EndedAt, _ = time.Parse(time.RFC3339Nano, end)
	}
	return run, err
}

func GetWorkspaceChangeRun(ctx context.Context, q Queryer, chat bool, owner, runID string) (domain.WorkspaceChangeRun, error) {
	table, ownerColumn := "runs", "team_id"
	if chat {
		table, ownerColumn = "chat_runs", "chat_id"
	}
	return scanChangeRun(q.QueryRowContext(ctx, `SELECT `+changeRunColumns(chat)+` FROM `+table+` WHERE id=? AND `+ownerColumn+`=?`, runID, owner))
}

func ListWorkspaceChangeRuns(ctx context.Context, q Queryer, chat bool, owner, botID, cursor string) ([]domain.WorkspaceChangeRun, error) {
	table, ownerColumn := "runs", "team_id"
	if chat {
		table, ownerColumn = "chat_runs", "chat_id"
	}
	where, args := ownerColumn+`=?`, []any{owner}
	if !chat {
		where += ` AND bot_id=?`
		args = append(args, botID)
	}
	var before int64
	if cursor != "" {
		lookup := append(append([]any{}, args...), cursor)
		if err := q.QueryRowContext(ctx, `SELECT rowid FROM `+table+` WHERE `+where+` AND id=?`, lookup...).Scan(&before); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	steps, kind, status, detail := changeStepTable(chat)
	query := `SELECT ` + changeRunColumns(chat) + ` FROM ` + table + ` WHERE ` + where
	if cursor != "" {
		query += ` AND rowid<?`
		args = append(args, before)
	}
	query += ` AND EXISTS (SELECT 1 FROM ` + steps + ` WHERE run_id=` + table + `.id AND ` + status + `='completed' AND ` + kind + ` IN ('file_change','diff') AND
		(COALESCE(json_array_length(` + detail + `,'$.changes'),0)>0 OR (` + kind + `='diff' AND length(trim(COALESCE(json_extract(` + detail + `,'$.diff'),'')))>0))) ORDER BY rowid DESC LIMIT ?`
	args = append(args, ChangeRunPageSize+1)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []domain.WorkspaceChangeRun{}
	for rows.Next() {
		run, err := scanChangeRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func LatestWorkspaceTurnDiff(ctx context.Context, q Queryer, chat bool, runID string) (seq int, metadata, complete bool, err error) {
	table, kind, status, detail := changeStepTable(chat)
	err = q.QueryRowContext(ctx, `SELECT seq, COALESCE(json_type(`+detail+`,'$.changes')='array',0), COALESCE(json_extract(`+detail+`,'$.filesComplete'),0) FROM `+table+` WHERE run_id=? AND `+kind+`='diff' AND `+status+`='completed' ORDER BY seq DESC LIMIT 1`, runID).Scan(&seq, &metadata, &complete)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

func HasWorkspaceFileChanges(ctx context.Context, q Queryer, chat bool, runID string) (bool, error) {
	table, kind, status, detail := changeStepTable(chat)
	var found bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE run_id=? AND `+kind+`='file_change' AND `+status+`='completed' AND COALESCE(json_array_length(`+detail+`,'$.changes'),0)>0)`, runID).Scan(&found)
	return found, err
}

// The metadata CTE deliberately removes diffs before sorting/grouping. Paging
// historical files must not pull every command or patch body into application RAM.
func changeFilesCTE(chat bool, runID string, source ChangeSource) (string, []any) {
	table, kind, status, detail := changeStepTable(chat)
	selected := `SELECT seq, ` + detail + ` AS detail FROM ` + table + ` WHERE run_id=? AND ` + status + `='completed' AND ` + kind + `=?`
	kindValue := "file_change"
	if source.Kind == "turn_diff" {
		kindValue = "diff"
	}
	args := []any{runID, kindValue}
	if source.Kind == "turn_diff" {
		selected += ` AND seq=?`
		args = append(args, source.Seq)
	}
	if source.LegacyDetail != nil {
		selected = `SELECT 0 AS seq, ? AS detail`
		args = []any{string(source.LegacyDetail)}
	}
	cte := `WITH selected AS (` + selected + `), raw AS MATERIALIZED (
		SELECT s.seq, j.key AS item, json_remove(j.value,'$.diff') AS value FROM selected s, json_each(s.detail,'$.changes') j
		WHERE json_type(j.value,'$.path')='text' AND json_extract(j.value,'$.path')<>''
	), ranked AS (
		SELECT *, json_extract(value,'$.path') AS path,
		ROW_NUMBER() OVER (PARTITION BY json_extract(value,'$.path') ORDER BY seq DESC,item DESC) AS recent FROM raw
	), grouped AS (
		SELECT path, SUM(MAX(0,CAST(COALESCE(json_extract(value,'$.additions'),0) AS INTEGER))) AS additions,
		SUM(MAX(0,CAST(COALESCE(json_extract(value,'$.deletions'),0) AS INTEGER))) AS deletions,
		MIN(COALESCE(json_extract(value,'$.statsComplete'),0)=1) AS complete,
		MAX(COALESCE(json_extract(value,'$.binary'),0)) AS binary,
		MAX(COALESCE(json_extract(value,'$.diffTruncated'),0)) AS truncated FROM ranked GROUP BY path
	) `
	return cte, args
}

func WorkspaceChangeTotals(ctx context.Context, q Queryer, chat bool, runID string, source ChangeSource) (count, additions, deletions int, complete bool, err error) {
	cte, args := changeFilesCTE(chat, runID, source)
	err = q.QueryRowContext(ctx, cte+`SELECT COUNT(*), COALESCE(SUM(additions),0), COALESCE(SUM(deletions),0), COALESCE(MIN(complete),1) FROM grouped`, args...).Scan(&count, &additions, &deletions, &complete)
	return
}

func ListWorkspaceChangeFiles(ctx context.Context, q Queryer, chat bool, runID string, source ChangeSource, cursor string) ([]domain.WorkspaceChangeFile, error) {
	cte, args := changeFilesCTE(chat, runID, source)
	args = append(args, cursor, ChangeFilePageSize+1)
	rows, err := q.QueryContext(ctx, cte+`SELECT g.path, COALESCE(json_extract(r.value,'$.kind'),'update'), COALESCE(json_extract(r.value,'$.movePath'),''), g.additions,g.deletions,g.complete,g.binary,g.truncated FROM grouped g JOIN ranked r ON r.path=g.path AND r.recent=1 WHERE g.path>? ORDER BY g.path LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []domain.WorkspaceChangeFile{}
	for rows.Next() {
		var file domain.WorkspaceChangeFile
		if err := rows.Scan(&file.Path, &file.Kind, &file.MovePath, &file.Additions, &file.Deletions, &file.StatsComplete, &file.Binary, &file.DiffTruncated); err != nil {
			return nil, err
		}
		if file.Kind != "add" && file.Kind != "delete" {
			file.Kind = "update"
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

func GetWorkspaceChangeFile(ctx context.Context, q Queryer, chat bool, runID string, source ChangeSource, path string) (domain.WorkspaceChangeFile, int, int, error) {
	cte, args := changeFilesCTE(chat, runID, source)
	args = append(args, path)
	var file domain.WorkspaceChangeFile
	var start, end int
	err := q.QueryRowContext(ctx, cte+`SELECT g.path, COALESCE(json_extract(r.value,'$.kind'),'update'), COALESCE(json_extract(r.value,'$.movePath'),''), g.additions,g.deletions,g.complete,g.binary,g.truncated,COALESCE(json_extract(r.value,'$.start'),0),COALESCE(json_extract(r.value,'$.end'),0) FROM grouped g JOIN ranked r ON r.path=g.path AND r.recent=1 WHERE g.path=?`, args...).Scan(&file.Path, &file.Kind, &file.MovePath, &file.Additions, &file.Deletions, &file.StatsComplete, &file.Binary, &file.DiffTruncated, &start, &end)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if file.Kind != "add" && file.Kind != "delete" {
		file.Kind = "update"
	}
	return file, start, end, err
}

func ReadWorkspaceTurnDiff(ctx context.Context, q Queryer, chat bool, runID string, seq int) (string, bool, error) {
	table, _, _, detail := changeStepTable(chat)
	var diff string
	var truncated bool
	err := q.QueryRowContext(ctx, `SELECT COALESCE(json_extract(`+detail+`,'$.diff'),''),COALESCE(json_extract(`+detail+`,'$.diffTruncated'),0) FROM `+table+` WHERE run_id=? AND seq=?`, runID, seq).Scan(&diff, &truncated)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return diff, truncated || strings.HasSuffix(diff, "\n…"), err
}

// ReadWorkspaceFilePatches limits a file's preview even when a turn contains
// thousands of individual patch steps. Statistics are queried independently.
func ReadWorkspaceFilePatches(ctx context.Context, q Queryer, chat bool, runID, path string) (string, bool, error) {
	table, kind, status, detail := changeStepTable(chat)
	rows, err := q.QueryContext(ctx, `SELECT COALESCE(json_extract(j.value,'$.diff'),''),COALESCE(json_extract(j.value,'$.diffTruncated'),0) FROM `+table+` s,json_each(`+detail+`,'$.changes') j WHERE run_id=? AND `+kind+`='file_change' AND `+status+`='completed' AND json_extract(j.value,'$.path')=? ORDER BY seq,j.key`, runID, path)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	var out strings.Builder
	truncated := false
	for count := 0; rows.Next(); count++ {
		var diff string
		var clipped bool
		if err := rows.Scan(&diff, &clipped); err != nil {
			return "", false, err
		}
		if count >= 1000 || out.Len()+len(diff)+1 > 256<<10 {
			truncated = true
			break
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(diff)
		truncated = truncated || clipped || strings.HasSuffix(diff, "\n…")
	}
	return out.String(), truncated, rows.Err()
}
