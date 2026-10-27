-- +goose Up
-- The Anthropic Messages dialect is no longer supported: CoBot speaks the
-- OpenAI Responses protocol only. Providers that declared a Claude endpoint
-- lose that endpoint; a provider left without any endpoint spoke nothing
-- else (the Anthropic official preset was one), so it is marked disabled and
-- the settings list asks the user to remove it. No provider row and no key
-- is deleted here: dropping them would silently take the user's stored
-- credential with it.
UPDATE model_providers
SET endpoints_json = (
    SELECT COALESCE(json_group_array(json(value)), '[]')
    FROM json_each(model_providers.endpoints_json)
    WHERE json_extract(value, '$.dialect') <> 'anthropic-messages'
)
WHERE EXISTS (
    SELECT 1 FROM json_each(model_providers.endpoints_json)
    WHERE json_extract(value, '$.dialect') = 'anthropic-messages'
);

UPDATE model_providers
SET enabled = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE auth_mode = 'api_key' AND endpoints_json = '[]';

-- +goose Down
-- The deleted endpoints cannot be brought back; the disabled providers are
-- switched on again, which is the state they were in before this migration.
UPDATE model_providers
SET enabled = 1
WHERE auth_mode = 'api_key' AND endpoints_json = '[]';
