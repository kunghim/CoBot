-- +goose Up
-- Work moves as a relay. A requirement carries the document the project
-- manager wrote and the route the user confirmed: an ordered list of stages,
-- each a card for one member (or a confirmation for the user). Cards stay in
-- the tasks table; a card knows its requirement, its stage and, for rework,
-- the stage it returns to. The earlier task model (free task creation,
-- dependencies, transfers, review notices) is gone with its data.
CREATE TABLE requirements (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL REFERENCES teams(id),
    title       TEXT NOT NULL,
    document    TEXT NOT NULL,
    stages      TEXT NOT NULL DEFAULT '[]', -- JSON [{name, kind, ownerId, reviewType, criteria}]
    status      TEXT NOT NULL,              -- active|accepting|done|cancelled
    created_by  TEXT NOT NULL,
    chain_id    TEXT NOT NULL DEFAULT '',
    revision    INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
CREATE INDEX idx_requirements_team ON requirements (team_id, status);

DROP TABLE task_dependencies;
DROP TABLE transfers;
DELETE FROM task_evidence;
DELETE FROM deliveries WHERE task_id != ''
    OR kind IN ('task_assignment', 'review_notice', 'review_request', 'review_result',
                'review_escalation', 'delivery_report', 'dependency_cancelled');
DELETE FROM questions WHERE task_id != '' OR kind != '';
DELETE FROM tasks;

ALTER TABLE tasks ADD COLUMN stage_index INTEGER NOT NULL DEFAULT -1;
ALTER TABLE tasks ADD COLUMN stage_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN return_to INTEGER NOT NULL DEFAULT -1;
ALTER TABLE tasks DROP COLUMN reviewer_id;
ALTER TABLE tasks DROP COLUMN reject_count;

-- A handoff record: how the card left (done, back, escalate, resume), the
-- rework mode of a send-back, suggestions passed on, and what the engine
-- reported was actually run and changed.
ALTER TABLE task_evidence ADD COLUMN outcome TEXT NOT NULL DEFAULT '';
ALTER TABLE task_evidence ADD COLUMN mode TEXT NOT NULL DEFAULT '';
ALTER TABLE task_evidence ADD COLUMN suggestions TEXT NOT NULL DEFAULT '';
ALTER TABLE task_evidence ADD COLUMN execution TEXT NOT NULL DEFAULT '';

-- A confirmation belongs to one stage of a requirement.
ALTER TABLE questions ADD COLUMN stage_index INTEGER NOT NULL DEFAULT -1;

-- +goose Down
CREATE TABLE task_dependencies (
    task_id             TEXT NOT NULL REFERENCES tasks(id),
    depends_on_task_id  TEXT NOT NULL REFERENCES tasks(id),
    PRIMARY KEY (task_id, depends_on_task_id)
);
CREATE INDEX idx_task_deps_dependent ON task_dependencies (depends_on_task_id);
CREATE TABLE transfers (
    id            TEXT PRIMARY KEY,
    task_id       TEXT NOT NULL REFERENCES tasks(id),
    from_bot_id   TEXT NOT NULL REFERENCES bots(id),
    to_bot_id     TEXT NOT NULL REFERENCES bots(id),
    task_revision INTEGER NOT NULL,
    note          TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX idx_transfers_task ON transfers (task_id, status);
CREATE INDEX idx_transfers_target ON transfers (to_bot_id, status);
ALTER TABLE questions DROP COLUMN stage_index;
ALTER TABLE task_evidence DROP COLUMN execution;
ALTER TABLE task_evidence DROP COLUMN suggestions;
ALTER TABLE task_evidence DROP COLUMN mode;
ALTER TABLE task_evidence DROP COLUMN outcome;
ALTER TABLE tasks ADD COLUMN reject_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN reviewer_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks DROP COLUMN return_to;
ALTER TABLE tasks DROP COLUMN stage_kind;
ALTER TABLE tasks DROP COLUMN stage_index;
DROP TABLE requirements;
