-- +goose Up
-- How freely a team's members may act: read_only | default | full. The
-- engine adapter maps it onto the engine's own approval and sandbox
-- settings at every turn. Existing teams take the default.
ALTER TABLE teams ADD COLUMN permission_mode TEXT NOT NULL DEFAULT 'default';

-- +goose Down
ALTER TABLE teams DROP COLUMN permission_mode;
