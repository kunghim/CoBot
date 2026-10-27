-- +goose Up
ALTER TABLE settings ADD COLUMN model_switch_prompt TEXT NOT NULL DEFAULT 'always';

-- +goose Down
ALTER TABLE settings DROP COLUMN model_switch_prompt;
