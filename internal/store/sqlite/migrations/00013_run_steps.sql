-- +goose Up
-- The visible steps of each run (thinking, commands, file changes, tool
-- calls, engine notices), so a conversation's process survives a reload.
-- Streamed text deltas are never stored; a step row holds its final form.
CREATE TABLE run_steps (
    run_id     TEXT NOT NULL REFERENCES runs(id),
    item_id    TEXT NOT NULL,
    team_id    TEXT NOT NULL,
    bot_id     TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    kind       TEXT NOT NULL,
    status     TEXT NOT NULL,
    detail     TEXT NOT NULL,
    started_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (run_id, item_id)
);
CREATE INDEX idx_run_steps_bot ON run_steps (bot_id, run_id, seq);

-- +goose Down
DROP TABLE run_steps;
