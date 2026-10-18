-- +goose Up
-- Profile metadata: stable identity of this data root.
CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Single business settings row; revision drives optimistic concurrency.
CREATE TABLE settings (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    revision       INTEGER NOT NULL DEFAULT 1,
    language       TEXT    NOT NULL DEFAULT 'zh-CN',
    default_model  TEXT    NOT NULL DEFAULT '',
    eval_model     TEXT    NOT NULL DEFAULT '',
    member_limit   INTEGER NOT NULL DEFAULT 6,
    chain_limit    INTEGER NOT NULL DEFAULT 6,
    budget_scope   TEXT    NOT NULL DEFAULT '',
    budget_tokens  INTEGER NOT NULL DEFAULT 0,
    engine_path    TEXT    NOT NULL DEFAULT '',
    updated_at     TEXT    NOT NULL
);

INSERT INTO settings (id, updated_at) VALUES (1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

-- Durable product events; seq is the global replay cursor.
CREATE TABLE events (
    seq         INTEGER PRIMARY KEY AUTOINCREMENT,
    id          TEXT    NOT NULL UNIQUE,
    instance_id TEXT    NOT NULL,
    type        TEXT    NOT NULL,
    team_id     TEXT    NOT NULL DEFAULT '',
    bot_id      TEXT    NOT NULL DEFAULT '',
    resource_id TEXT    NOT NULL DEFAULT '',
    revision    INTEGER NOT NULL DEFAULT 0,
    payload     TEXT    NOT NULL DEFAULT '{}',
    created_at  TEXT    NOT NULL
);

CREATE INDEX idx_events_team ON events (team_id, seq);
CREATE INDEX idx_events_bot ON events (bot_id, seq);

-- +goose Down
DROP TABLE events;
DROP TABLE settings;
DROP TABLE meta;
