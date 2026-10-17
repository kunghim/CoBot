-- +goose Up
-- Data of features that are gone: the risk checks (their evaluation model
-- setting and token rows), the recruitment and merge approvals with the
-- feedback they sent back, and the steps of the collaboration tools no
-- member is offered any more.
ALTER TABLE settings DROP COLUMN eval_model;

DELETE FROM usage_ledger WHERE purpose = 'risk_eval';

DELETE FROM approval_responses WHERE approval_id IN (SELECT id FROM approvals WHERE type IN ('recruitment', 'merge', 'collaboration'));
DELETE FROM approval_decisions WHERE approval_id IN (SELECT id FROM approvals WHERE type IN ('recruitment', 'merge', 'collaboration'));
DELETE FROM approvals WHERE type IN ('recruitment', 'merge', 'collaboration');

DELETE FROM chain_wakes WHERE delivery_id IN (SELECT id FROM deliveries WHERE kind = 'approval_result');
CREATE TEMP TABLE retired_messages AS SELECT message_id AS id FROM deliveries WHERE kind = 'approval_result';
DELETE FROM deliveries WHERE kind = 'approval_result';
DELETE FROM messages WHERE id IN (SELECT id FROM retired_messages);
DROP TABLE retired_messages;

DELETE FROM run_steps WHERE kind = 'tool' AND json_extract(detail, '$.tool') IN (
    'get_context', 'list_tasks', 'get_task', 'list_roles', 'propose_task', 'assign_task',
    'update_task', 'submit_evidence', 'report_completion', 'confirm_task', 'offer_transfer',
    'accept_transfer', 'request_member', 'request_merge', 'remember', 'search_memory');

-- +goose Down
-- Deleted rows cannot come back; only the column returns, empty.
ALTER TABLE settings ADD COLUMN eval_model TEXT NOT NULL DEFAULT '';
