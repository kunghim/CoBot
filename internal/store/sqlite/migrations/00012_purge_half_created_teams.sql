-- +goose Up
-- Creating a team is now one transaction, so a team is never left half
-- created. Teams earlier builds left as creating or error are removed with
-- every row of theirs; their folders are not touched.
DELETE FROM approval_decisions WHERE approval_id IN (SELECT id FROM approvals WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error')));
DELETE FROM approval_responses WHERE approval_id IN (SELECT id FROM approvals WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error')));
DELETE FROM approvals WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM chain_wakes WHERE chain_id IN (SELECT id FROM chains WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error')));
DELETE FROM chains WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM deliveries WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM task_dependencies
 WHERE task_id IN (SELECT id FROM tasks WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error')))
    OR depends_on_task_id IN (SELECT id FROM tasks WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error')));
DELETE FROM task_evidence WHERE task_id IN (SELECT id FROM tasks WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error')));
DELETE FROM transfers WHERE task_id IN (SELECT id FROM tasks WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error')));
DELETE FROM tasks WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM questions WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM memories WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM messages WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM runs WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM usage_ledger WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM events WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM provider_sessions WHERE bot_id IN (SELECT id FROM bots WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error')));
DELETE FROM directory_bindings WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM bots WHERE team_id IN (SELECT id FROM teams WHERE status IN ('creating', 'error'));
DELETE FROM teams WHERE status IN ('creating', 'error');

-- +goose Down
-- Purged rows cannot be restored; nothing to undo.
SELECT 1;
