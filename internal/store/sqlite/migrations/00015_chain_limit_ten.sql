-- +goose Up
-- Queued member messages now merge into one turn and a user's continue grants
-- a fresh budget, so the default collaboration limit rises from 6 to 10. A
-- limit the user set themselves is kept.
UPDATE settings SET chain_limit = 10 WHERE chain_limit = 6;

-- +goose Down
UPDATE settings SET chain_limit = 6 WHERE chain_limit = 10;
