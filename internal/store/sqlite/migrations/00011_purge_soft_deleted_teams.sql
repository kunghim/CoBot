-- +goose Up
-- Deleting a team is now complete. Teams soft-deleted by earlier builds are
-- removed the same way: every row of theirs, children before parents
-- because foreign keys are enforced.
DELETE FROM approval_decisions WHERE approval_id IN (SELECT id FROM approvals WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted'));
DELETE FROM approval_responses WHERE approval_id IN (SELECT id FROM approvals WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted'));
DELETE FROM approvals WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM chain_wakes WHERE chain_id IN (SELECT id FROM chains WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted'));
DELETE FROM chains WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM deliveries WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM task_dependencies
 WHERE task_id IN (SELECT id FROM tasks WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted'))
    OR depends_on_task_id IN (SELECT id FROM tasks WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted'));
DELETE FROM task_evidence WHERE task_id IN (SELECT id FROM tasks WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted'));
DELETE FROM transfers WHERE task_id IN (SELECT id FROM tasks WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted'));
DELETE FROM tasks WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM questions WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM memories WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM messages WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM runs WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM usage_ledger WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM events WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM provider_sessions WHERE bot_id IN (SELECT id FROM bots WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted'));
DELETE FROM directory_bindings WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM bots WHERE team_id IN (SELECT id FROM teams WHERE status = 'deleted');
DELETE FROM teams WHERE status = 'deleted';

-- +goose Down
-- Purged rows cannot be restored; nothing to undo.
SELECT 1;
