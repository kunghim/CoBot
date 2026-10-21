-- +goose Up
-- Every configured provider is on offer at once: a member picks a model from
-- any provider that is enabled, and a disabled one is out of every picker.
-- Providers that already exist stay usable, so they start enabled.
ALTER TABLE model_providers ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;

-- The model a member runs with is the member's own choice. Empty means the
-- member follows the default model named in the settings.
ALTER TABLE bots ADD COLUMN provider_id TEXT NOT NULL DEFAULT '';
ALTER TABLE bots ADD COLUMN model TEXT NOT NULL DEFAULT '';

-- The ChatGPT account is a provider like the others rather than something
-- the engine owns, so it is always in the list: the user signs in there and
-- can disable it. A profile that already stored one keeps its own row.
-- +goose StatementBegin
INSERT INTO model_providers (id, name, kind, auth_mode, preset_id, revision, created_at, updated_at)
SELECT 'openai', 'OpenAI', 'official', 'chatgpt', 'openai', 1, '1970-01-01T00:00:00Z', '1970-01-01T00:00:00Z'
WHERE NOT EXISTS (SELECT 1 FROM model_providers WHERE auth_mode = 'chatgpt')
  AND NOT EXISTS (SELECT 1 FROM model_providers WHERE id = 'openai');
-- +goose StatementEnd

-- +goose Down
DELETE FROM model_providers WHERE id = 'openai' AND auth_mode = 'chatgpt' AND created_at = '1970-01-01T00:00:00Z';
ALTER TABLE bots DROP COLUMN model;
ALTER TABLE bots DROP COLUMN provider_id;
ALTER TABLE model_providers DROP COLUMN enabled;
