-- +goose Up
-- A causal chain groups the work caused by one user message. Every bot-caused
-- wake inside it shares one wake budget, so a runaway collaboration cannot
-- keep re-waking members forever. The limit is snapshotted at chain creation;
-- a user approval may grant exactly one more wake on that same chain.
CREATE TABLE chains (
    id                TEXT PRIMARY KEY,
    team_id           TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    origin_message_id TEXT NOT NULL,
    origin_bot_id     TEXT NOT NULL DEFAULT '',
    epoch             INTEGER NOT NULL DEFAULT 1,
    status            TEXT NOT NULL DEFAULT 'active', -- active|awaiting_approval|stopped
    wake_count        INTEGER NOT NULL DEFAULT 0,
    limit_count       INTEGER NOT NULL,
    risk_start        INTEGER NOT NULL,
    stop_reason       TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);
CREATE INDEX idx_chains_team_status ON chains (team_id, status);

-- One row per counted wake. The unique key is what makes a redelivery of the
-- same dispatchKey a no-op instead of a second counted wake (G01).
CREATE TABLE chain_wakes (
    id            TEXT PRIMARY KEY,
    chain_id      TEXT NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
    dispatch_key  TEXT NOT NULL,
    delivery_id   TEXT NOT NULL,
    woken_bot_id  TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    UNIQUE (chain_id, dispatch_key)
);
CREATE INDEX idx_chain_wakes_chain ON chain_wakes (chain_id, created_at);

-- run_id links a delivery to the run that claimed it. Chain attribution needs
-- it (a bot's outbound message inherits the chain of the run it sent during),
-- and startup reconciliation later uses it to classify unfinished runs.
ALTER TABLE deliveries ADD COLUMN run_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_deliveries_run ON deliveries (run_id);

-- +goose Down
DROP TABLE IF EXISTS chain_wakes;
DROP TABLE IF EXISTS chains;
