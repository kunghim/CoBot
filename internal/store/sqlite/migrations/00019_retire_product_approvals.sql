-- +goose Up
-- Members can no longer ask for a merge or a new member, and these cards can
-- no longer be decided. One still pending is closed; decided ones stay as
-- history.
UPDATE approvals SET request_state = 'invalidated', revision = revision + 1
WHERE type IN ('merge', 'recruitment') AND request_state = 'pending';

-- +goose Down
-- The closed cards stay closed: nothing can decide them any more.
SELECT 1;
