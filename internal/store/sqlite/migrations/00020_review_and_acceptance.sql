-- +goose Up
-- A task the leader marks for review goes to a reviewer member, whose verdict
-- decides it; reject_count counts the reviewer's automatic send-backs.
-- requirement_id groups the tasks one confirmed plan created (the causal chain
-- of the user's message), so the batch can be handed to the user as a whole.
ALTER TABLE tasks ADD COLUMN review_type TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN reviewer_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN reject_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN requirement_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_tasks_requirement ON tasks (requirement_id);

-- How many times a reviewer may send one task back before the leader decides.
ALTER TABLE settings ADD COLUMN review_reject_limit INTEGER NOT NULL DEFAULT 3;

-- "acceptance" marks the user's acceptance of a finished requirement; plain
-- ask_user questions keep the empty kind.
ALTER TABLE questions ADD COLUMN kind TEXT NOT NULL DEFAULT '';
ALTER TABLE questions ADD COLUMN requirement_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE questions DROP COLUMN requirement_id;
ALTER TABLE questions DROP COLUMN kind;
ALTER TABLE settings DROP COLUMN review_reject_limit;
DROP INDEX idx_tasks_requirement;
ALTER TABLE tasks DROP COLUMN requirement_id;
ALTER TABLE tasks DROP COLUMN reject_count;
ALTER TABLE tasks DROP COLUMN reviewer_id;
ALTER TABLE tasks DROP COLUMN review_type;
