-- +goose Up
CREATE TABLE chats (
    id TEXT PRIMARY KEY,
    activity_at TEXT NOT NULL,
    data TEXT NOT NULL
);
CREATE INDEX idx_chats_activity ON chats(activity_at DESC, id DESC);
CREATE TABLE chat_sessions (
    chat_id TEXT PRIMARY KEY REFERENCES chats(id) ON DELETE CASCADE,
    data TEXT NOT NULL
);
CREATE TABLE chat_messages (
    id TEXT PRIMARY KEY,
    chat_id TEXT NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL REFERENCES chat_runs(id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
    created_at TEXT NOT NULL,
    data TEXT NOT NULL
);
CREATE INDEX idx_chat_messages_page ON chat_messages(chat_id, created_at DESC, id DESC);
CREATE TABLE chat_runs (
    id TEXT PRIMARY KEY,
    chat_id TEXT NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    data TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_chat_one_live_run ON chat_runs(chat_id)
    WHERE status IN ('queued','starting','running','waiting_approval','waiting_input','unknown');
CREATE TABLE chat_run_steps (
    run_id TEXT NOT NULL REFERENCES chat_runs(id) ON DELETE CASCADE,
    item_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    data TEXT NOT NULL,
    PRIMARY KEY(run_id, item_id)
);
CREATE TABLE chat_requests (
    id TEXT PRIMARY KEY,
    chat_id TEXT NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL REFERENCES chat_runs(id) ON DELETE CASCADE,
    state TEXT NOT NULL,
    engine_id TEXT NOT NULL,
    epoch TEXT NOT NULL,
    data TEXT NOT NULL
);
CREATE INDEX idx_chat_requests ON chat_requests(chat_id, state);
-- Usage remains after deleting conversation content; it is billing evidence.
CREATE TABLE chat_usage (
    chat_id TEXT NOT NULL,
    epoch TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    turn_id TEXT NOT NULL,
    data TEXT NOT NULL,
    PRIMARY KEY(epoch, thread_id, turn_id)
);
-- +goose Down
DROP TABLE chat_usage;
DROP TABLE chat_requests;
DROP TABLE chat_run_steps;
DROP TABLE chat_runs;
DROP TABLE chat_messages;
DROP TABLE chat_sessions;
DROP TABLE chats;
