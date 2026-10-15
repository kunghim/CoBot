-- +goose Up
-- Task board: unique owner, acceptance criteria, evidence and dependencies.
-- A task never changes owner implicitly; transfers are two-phase rows below.
CREATE TABLE tasks (
    id                  TEXT PRIMARY KEY,
    team_id             TEXT NOT NULL REFERENCES teams(id),
    owner_id            TEXT NOT NULL REFERENCES bots(id),
    parent_id           TEXT NOT NULL DEFAULT '', -- organization only, never an implicit dependency
    title               TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL, -- todo|doing|review|done|cancelled
    blocked_reason      TEXT NOT NULL DEFAULT '',
    revision            INTEGER NOT NULL DEFAULT 1,
    created_by          TEXT NOT NULL DEFAULT '', -- bot id or 'user'
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);
CREATE INDEX idx_tasks_team ON tasks (team_id, status);
CREATE INDEX idx_tasks_owner ON tasks (owner_id, status);

-- Dependencies are directed edges: task_id depends on depends_on_task_id.
-- Same team only; cycles rejected in the application inside the transaction.
CREATE TABLE task_dependencies (
    task_id             TEXT NOT NULL REFERENCES tasks(id),
    depends_on_task_id  TEXT NOT NULL REFERENCES tasks(id),
    PRIMARY KEY (task_id, depends_on_task_id)
);
CREATE INDEX idx_task_deps_dependent ON task_dependencies (depends_on_task_id);

-- Evidence rows attach to a task; the product records references and agent
-- reports, never the artifacts themselves.
CREATE TABLE task_evidence (
    id            TEXT PRIMARY KEY,
    task_id       TEXT NOT NULL REFERENCES tasks(id),
    author_id     TEXT NOT NULL, -- bot id or 'user'
    summary       TEXT NOT NULL,
    artifact_refs TEXT NOT NULL DEFAULT '[]', -- JSON array of reference strings
    candidate_ref TEXT NOT NULL DEFAULT '',   -- exact version/SHA under review
    test_result   TEXT NOT NULL DEFAULT '',
    review_result TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL
);
CREATE INDEX idx_task_evidence_task ON task_evidence (task_id, created_at);

-- Two-phase transfer: owner offers, target accepts. The owner is unchanged
-- until acceptance commits, so a task never has zero or two owners.
CREATE TABLE transfers (
    id            TEXT PRIMARY KEY,
    task_id       TEXT NOT NULL REFERENCES tasks(id),
    from_bot_id   TEXT NOT NULL REFERENCES bots(id),
    to_bot_id     TEXT NOT NULL REFERENCES bots(id),
    task_revision INTEGER NOT NULL,
    note          TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL, -- offered|accepted|cancelled|stale
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX idx_transfers_task ON transfers (task_id, status);
CREATE INDEX idx_transfers_target ON transfers (to_bot_id, status);

-- ask_user: a bot asks the user a question. Answers are delivered as user
-- messages; a question is never a security authorization.
CREATE TABLE questions (
    id               TEXT PRIMARY KEY,
    team_id          TEXT NOT NULL REFERENCES teams(id),
    requester_bot_id TEXT NOT NULL REFERENCES bots(id),
    task_id          TEXT NOT NULL DEFAULT '',
    run_id           TEXT NOT NULL DEFAULT '',
    question         TEXT NOT NULL,
    options          TEXT NOT NULL DEFAULT '[]', -- JSON array of option strings
    status           TEXT NOT NULL, -- open|answered|cancelled
    answer_text      TEXT NOT NULL DEFAULT '',
    answer_message_id TEXT NOT NULL DEFAULT '',
    revision         INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT NOT NULL,
    answered_at      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_questions_team_status ON questions (team_id, status);
CREATE INDEX idx_questions_requester ON questions (requester_bot_id, status);

-- Bot-to-bot collaboration: a collaboration message has both ends recorded.
-- User messages keep source_bot_id empty, which distinguishes them without a
-- second table.
ALTER TABLE messages ADD COLUMN source_bot_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_messages_source ON messages (source_bot_id, created_at);

-- +goose Down
DROP TABLE questions;
DROP TABLE transfers;
DROP TABLE task_evidence;
DROP TABLE task_dependencies;
DROP TABLE tasks;
