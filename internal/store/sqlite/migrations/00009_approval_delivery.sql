-- +goose Up
-- Product decisions and their notification outbox commit together. A native
-- reply has no delivery row and retains its exact engine-bound semantics.
ALTER TABLE approval_responses ADD COLUMN delivery_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_approval_response_delivery ON approval_responses(delivery_id) WHERE delivery_id != '';

-- +goose Down
DROP INDEX idx_approval_response_delivery;
ALTER TABLE approval_responses DROP COLUMN delivery_id;
