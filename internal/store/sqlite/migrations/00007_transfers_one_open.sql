-- +goose Up
-- At most one open hand-over per task, enforced by the database itself so two
-- concurrent offers cannot both land (review B4).
CREATE UNIQUE INDEX idx_transfers_one_open ON transfers (task_id) WHERE status = 'offered';

-- +goose Down
DROP INDEX IF EXISTS idx_transfers_one_open;
