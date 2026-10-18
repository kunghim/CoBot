-- +goose Up
-- How the interface looks for the user: light, dark or following the system,
-- and which palette it uses.
ALTER TABLE settings ADD COLUMN color_mode TEXT NOT NULL DEFAULT 'system';
ALTER TABLE settings ADD COLUMN theme TEXT NOT NULL DEFAULT 'graphite';

-- +goose Down
ALTER TABLE settings DROP COLUMN theme;
ALTER TABLE settings DROP COLUMN color_mode;