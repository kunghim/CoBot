-- +goose Up
-- What a member's engine thread has already been told (team roster, own
-- tasks, memories), so a turn sends only what changed instead of repeating
-- the whole context every time.
ALTER TABLE provider_sessions ADD COLUMN context_digest TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE provider_sessions DROP COLUMN context_digest;
