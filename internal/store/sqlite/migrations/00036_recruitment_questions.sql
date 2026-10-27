-- +goose Up
-- The project manager recruits missing roles through request_member. Each
-- request is a question card the user decides; the requested role ids ride
-- with the question so the decision can add exactly what was asked, even
-- after a restart.
ALTER TABLE questions ADD COLUMN role_ids TEXT NOT NULL DEFAULT '[]'; -- JSON array of role IDs

-- +goose Down
ALTER TABLE questions DROP COLUMN role_ids;
