-- +goose Up
-- Teams and their directory binding. The binding table is the single source
-- of truth; the in-directory .cobot/team.json marker is identification only.
CREATE TABLE teams (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    profile_instance TEXT NOT NULL,
    leader_id      TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL, -- creating|active|archived|deleted|directory_missing|error
    status_detail  TEXT NOT NULL DEFAULT '',
    revision       INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

CREATE TABLE directory_bindings (
    team_id        TEXT PRIMARY KEY REFERENCES teams(id),
    directory_key  TEXT NOT NULL UNIQUE, -- canonical comparison key
    directory      TEXT NOT NULL,        -- display (native, resolved)
    created_at     TEXT NOT NULL
);

-- Members. One bot belongs to exactly one team; role repeats are distinct
-- instances with distinct ids and display names.
CREATE TABLE bots (
    id             TEXT PRIMARY KEY,
    team_id        TEXT NOT NULL REFERENCES teams(id),
    name           TEXT NOT NULL,
    role_id        TEXT NOT NULL,
    role_version   TEXT NOT NULL DEFAULT '',
    is_leader      INTEGER NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT 'idle', -- run-fact status, never optimistic
    current_run_id TEXT NOT NULL DEFAULT '',
    revision       INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL,
    deleted_at     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_bots_team ON bots (team_id, deleted_at);

-- Bot <-> provider thread mapping. Rebuilding a provider thread never
-- deletes product messages.
CREATE TABLE provider_sessions (
    bot_id             TEXT PRIMARY KEY REFERENCES bots(id),
    provider_thread_id TEXT NOT NULL,
    engine_epoch       TEXT NOT NULL,
    model              TEXT NOT NULL DEFAULT '',
    model_provider     TEXT NOT NULL DEFAULT '',
    updated_at         TEXT NOT NULL
);

-- Messages are written before any dispatch; accepted != delivered.
CREATE TABLE messages (
    id              TEXT PRIMARY KEY,
    team_id         TEXT NOT NULL REFERENCES teams(id),
    bot_id          TEXT NOT NULL REFERENCES bots(id),
    kind            TEXT NOT NULL, -- user|assistant|collaboration|system
    blocks          TEXT NOT NULL, -- JSON block array; user text is preserved verbatim
    request_id      TEXT NOT NULL DEFAULT '',
    delivery_status TEXT NOT NULL DEFAULT '', -- queued|dispatching|accepted|cancelled|unknown ('' = not applicable)
    run_id          TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL
);
CREATE INDEX idx_messages_bot ON messages (bot_id, created_at);
CREATE UNIQUE INDEX idx_messages_request ON messages (team_id, request_id) WHERE request_id != '';

-- Deliveries drive dispatch; a queue decision never waits on the model.
CREATE TABLE deliveries (
    id             TEXT PRIMARY KEY,
    message_id     TEXT NOT NULL REFERENCES messages(id),
    team_id        TEXT NOT NULL REFERENCES teams(id),
    target_bot_id  TEXT NOT NULL REFERENCES bots(id),
    source_bot_id  TEXT NOT NULL DEFAULT '',
    task_id        TEXT NOT NULL DEFAULT '',
    chain_id       TEXT NOT NULL DEFAULT '',
    epoch          INTEGER NOT NULL DEFAULT 1,
    status         TEXT NOT NULL, -- waiting_dependency|queued|dispatching|accepted|cancelled|unknown
    dispatch_key   TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX idx_deliveries_target ON deliveries (target_bot_id, status);

-- Runs track one dispatched turn against the engine.
CREATE TABLE runs (
    id                  TEXT PRIMARY KEY,
    team_id             TEXT NOT NULL REFERENCES teams(id),
    bot_id              TEXT NOT NULL REFERENCES bots(id),
    provider_session_id TEXT NOT NULL DEFAULT '',
    provider_turn_id    TEXT NOT NULL DEFAULT '',
    engine_epoch        TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL, -- starting|running|waiting_approval|waiting_input|completed|failed|interrupted|unknown
    error_key           TEXT NOT NULL DEFAULT '',
    started_at          TEXT NOT NULL,
    ended_at            TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_runs_bot ON runs (bot_id, status);

-- Approvals: one engine request, projected to both the requesting member's
-- and the leader's session; decisions and response delivery are separate.
CREATE TABLE approvals (
    id               TEXT PRIMARY KEY,
    team_id          TEXT NOT NULL REFERENCES teams(id),
    requester_bot_id TEXT NOT NULL REFERENCES bots(id),
    run_id           TEXT NOT NULL DEFAULT '',
    engine_request_id TEXT NOT NULL, -- original JSON id, preserved type
    engine_epoch     TEXT NOT NULL,
    type             TEXT NOT NULL, -- native_tool|collaboration|recruitment|recovery|merge
    method           TEXT NOT NULL DEFAULT '',
    summary          TEXT NOT NULL,
    reason           TEXT NOT NULL DEFAULT '',
    options          TEXT NOT NULL, -- JSON snapshot: [{id,messageKey,text}]
    request_state    TEXT NOT NULL DEFAULT 'pending', -- pending|decided|invalidated
    created_at       TEXT NOT NULL,
    revision         INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_approvals_team_state ON approvals (team_id, request_state);

CREATE TABLE approval_decisions (
    approval_id TEXT PRIMARY KEY REFERENCES approvals(id),
    option_id   TEXT NOT NULL,
    comment     TEXT NOT NULL DEFAULT '',
    actor       TEXT NOT NULL,
    decided_at  TEXT NOT NULL
);

CREATE TABLE approval_responses (
    approval_id TEXT PRIMARY KEY REFERENCES approvals(id),
    state       TEXT NOT NULL, -- queued|sent|confirmed|unknown|expired
    detail      TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL
);

-- requestId idempotency for write APIs: same key + same body replays the
-- stored response; same key + different body is a conflict.
CREATE TABLE idempotency (
    scope        TEXT NOT NULL,
    principal    TEXT NOT NULL,
    key          TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response     TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    PRIMARY KEY (scope, principal, key)
);

-- +goose Down
DROP TABLE idempotency;
DROP TABLE approval_responses;
DROP TABLE approval_decisions;
DROP TABLE approvals;
DROP TABLE runs;
DROP TABLE deliveries;
DROP TABLE messages;
DROP TABLE provider_sessions;
DROP TABLE bots;
DROP TABLE directory_bindings;
DROP TABLE teams;
