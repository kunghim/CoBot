-- +goose Up
-- Which model provider a team runs with. Empty means the team follows the
-- profile's default, which is what a fresh profile uses. The provider is
-- named here rather than read from the team's threads, because a thread
-- records the provider it had, not the one it should have.
ALTER TABLE teams ADD COLUMN provider_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE teams DROP COLUMN provider_id;
