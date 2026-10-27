-- +goose Up
-- Owner/member prefixes retain rowid ordering for keyset history pages.
CREATE INDEX idx_runs_workspace_changes ON runs(team_id, bot_id);
CREATE INDEX idx_chat_runs_workspace_changes ON chat_runs(chat_id);
CREATE INDEX idx_run_steps_changes ON run_steps(run_id, kind, status, seq);
CREATE INDEX idx_chat_run_steps_changes ON chat_run_steps(
    run_id, json_extract(data, '$.kind'), json_extract(data, '$.status'), seq
);

-- +goose Down
DROP INDEX idx_chat_run_steps_changes;
DROP INDEX idx_run_steps_changes;
DROP INDEX idx_chat_runs_workspace_changes;
DROP INDEX idx_runs_workspace_changes;
