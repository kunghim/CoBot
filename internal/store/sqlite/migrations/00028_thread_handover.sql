-- +goose Up
-- Where the engine keeps each member thread's full record, and the thread a
-- new one took over from. A member moved to another vendor's model starts a
-- fresh thread (one vendor's history is not another's input); its first
-- turn is told where the previous record is, so it reads what it needs.
ALTER TABLE provider_sessions ADD COLUMN transcript_path TEXT NOT NULL DEFAULT '';
ALTER TABLE provider_sessions ADD COLUMN handover TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE provider_sessions DROP COLUMN handover;
ALTER TABLE provider_sessions DROP COLUMN transcript_path;
