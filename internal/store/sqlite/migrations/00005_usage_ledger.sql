-- +goose Up
-- The usage ledger is keyed by the engine connection that produced the
-- numbers: (epoch, thread, turn) uniquely identifies one turn, so a repeated
-- or replayed event updates the same row instead of being added twice, and a
-- thread reused by a later connection can never merge into an old turn (G06).
CREATE TABLE usage_ledger (
    id                  TEXT PRIMARY KEY,
    team_id             TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    bot_id              TEXT NOT NULL DEFAULT '',   -- empty for system sessions
    purpose             TEXT NOT NULL DEFAULT 'run', -- run|risk_eval
    engine_epoch        TEXT NOT NULL,
    thread_id           TEXT NOT NULL,
    turn_id             TEXT NOT NULL,
    input_tokens        INTEGER NOT NULL DEFAULT 0,
    output_tokens       INTEGER NOT NULL DEFAULT 0,
    cached_input_tokens INTEGER NOT NULL DEFAULT 0,
    status              TEXT NOT NULL DEFAULT 'recorded', -- recorded|unknown
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    UNIQUE (engine_epoch, thread_id, turn_id)
);
CREATE INDEX idx_usage_team ON usage_ledger (team_id);
CREATE INDEX idx_usage_bot ON usage_ledger (bot_id, purpose);

-- +goose Down
DROP TABLE IF EXISTS usage_ledger;
