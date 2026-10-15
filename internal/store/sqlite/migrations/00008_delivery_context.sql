-- +goose Up
ALTER TABLE deliveries ADD COLUMN kind TEXT NOT NULL DEFAULT 'message';
ALTER TABLE deliveries ADD COLUMN source_run_id TEXT NOT NULL DEFAULT '';

-- Backfill only known task identities; user answers used run_id for a
-- question, so treating every message.run_id as a task would corrupt scope.
UPDATE deliveries SET task_id = (
    SELECT t.id FROM messages m JOIN tasks t ON t.id = m.run_id AND t.team_id = m.team_id
    WHERE m.id = deliveries.message_id AND m.kind IN ('system', 'collaboration')
) WHERE EXISTS (
    SELECT 1 FROM messages m JOIN tasks t ON t.id = m.run_id AND t.team_id = m.team_id
    WHERE m.id = deliveries.message_id AND m.kind IN ('system', 'collaboration')
);
UPDATE deliveries SET kind = 'task_assignment' WHERE task_id != '' AND EXISTS (
    SELECT 1 FROM messages m WHERE m.id = deliveries.message_id AND m.kind = 'system'
);
UPDATE deliveries SET task_id = (
    SELECT q.task_id FROM questions q WHERE q.answer_message_id = deliveries.message_id
), kind = 'question_answer' WHERE EXISTS (
    SELECT 1 FROM questions q WHERE q.answer_message_id = deliveries.message_id
);
CREATE INDEX idx_deliveries_task ON deliveries(task_id, target_bot_id, status);
CREATE INDEX idx_questions_task_open ON questions(task_id, status);

-- +goose Down
DROP INDEX idx_questions_task_open;
DROP INDEX idx_deliveries_task;
ALTER TABLE deliveries DROP COLUMN source_run_id;
ALTER TABLE deliveries DROP COLUMN kind;
