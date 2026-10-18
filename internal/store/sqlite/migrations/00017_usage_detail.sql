-- +goose Up
-- A turn's usage is now the sum of all its model calls, with the cache writes
-- and reasoning tokens Codex reports; each member's latest engine usage
-- snapshot (session totals, context in use, window) is kept for the stats.
ALTER TABLE usage_ledger ADD COLUMN cache_write_input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_ledger ADD COLUMN reasoning_output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE provider_sessions ADD COLUMN usage_snapshot TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE provider_sessions DROP COLUMN usage_snapshot;
ALTER TABLE usage_ledger DROP COLUMN reasoning_output_tokens;
ALTER TABLE usage_ledger DROP COLUMN cache_write_input_tokens;
