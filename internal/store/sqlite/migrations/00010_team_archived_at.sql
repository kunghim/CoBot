-- +goose Up
-- When a team was archived, for the archived-teams list in Settings (sorting
-- by archive time). Empty while the team is not archived.
ALTER TABLE teams ADD COLUMN archived_at TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE teams DROP COLUMN archived_at;
