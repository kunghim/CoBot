-- +goose Up
-- A database that somehow already holds two open offers for one task keeps
-- the newest and marks the rest stale, so the uniqueness guard can build on
-- every historical database (review B).
UPDATE transfers SET status = 'stale'
WHERE status = 'offered'
  AND id NOT IN (
    SELECT id FROM (
      SELECT id, ROW_NUMBER() OVER (PARTITION BY task_id ORDER BY created_at DESC, id DESC) AS rn
      FROM transfers WHERE status = 'offered'
    ) WHERE rn = 1
  );

-- At most one open hand-over per task, enforced by the database itself so two
-- concurrent offers cannot both land (review B4).
CREATE UNIQUE INDEX idx_transfers_one_open ON transfers (task_id) WHERE status = 'offered';

-- +goose Down
DROP INDEX IF EXISTS idx_transfers_one_open;
