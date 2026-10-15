-- +goose Up
-- Memories store working methods and preferences per member. Deletion keeps
-- a tombstone row so the same owner/source/body can never be recreated
-- automatically (G04), while retrieval simply ignores tombstones.
CREATE TABLE memories (
    id             TEXT PRIMARY KEY,
    team_id        TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    bot_id         TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    body           TEXT NOT NULL,
    source_run_id  TEXT NOT NULL DEFAULT '',
    source_task_id TEXT NOT NULL DEFAULT '',
    norm_key       TEXT NOT NULL,
    deleted_at     TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL
);
-- Ownership scope for retrieval: filter by member first, always.
CREATE INDEX idx_memories_owner ON memories (team_id, bot_id, created_at);
-- Same owner, same source, same normalized body: one row forever, even as a
-- tombstone. This is the dedupe that blocks automatic re-creation.
CREATE UNIQUE INDEX idx_memories_dedupe ON memories (team_id, bot_id, source_run_id, norm_key);

-- +goose Down
DROP TABLE IF EXISTS memories;
