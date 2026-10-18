-- +goose Up
-- Avatars the user chose: a member's own (empty follows its role) and a
-- role's (absent follows the catalog default). Both hold an avatar
-- reference as JSON, never an image.
ALTER TABLE bots ADD COLUMN avatar TEXT NOT NULL DEFAULT '';
CREATE TABLE role_avatars (
    role_id    TEXT PRIMARY KEY,
    avatar     TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE role_avatars;
ALTER TABLE bots DROP COLUMN avatar;
