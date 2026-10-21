-- +goose Up
-- Model providers: the vendors or gateways CoBot can send requests to. A
-- provider holds the dialects it offers (endpoints) and the models the user
-- chose to keep — not the vendor's full catalogue. The credential is stored
-- elsewhere, encrypted; only its reference lives here.
CREATE TABLE model_providers (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    kind           TEXT NOT NULL,               -- official | custom
    auth_mode      TEXT NOT NULL,               -- chatgpt | api_key
    credential_ref TEXT NOT NULL DEFAULT '',
    endpoints_json TEXT NOT NULL DEFAULT '[]',  -- [{dialect, baseUrl}]
    models_json    TEXT NOT NULL DEFAULT '[]',  -- [{id, displayName, status, failure, ...}]
    default_model  TEXT NOT NULL DEFAULT '',
    retries_json   TEXT NOT NULL DEFAULT '{}',
    preset_id      TEXT NOT NULL DEFAULT '',
    revision       INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

-- Which provider the profile runs with. Empty means the engine's own
-- official provider, which is what a fresh profile uses.
ALTER TABLE settings ADD COLUMN default_provider TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE settings DROP COLUMN default_provider;
DROP TABLE model_providers;
