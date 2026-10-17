-- +goose Up
-- How hard a team's members think on each turn (Codex reasoning effort);
-- empty follows the model's own default.
ALTER TABLE teams ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE teams DROP COLUMN reasoning_effort;
