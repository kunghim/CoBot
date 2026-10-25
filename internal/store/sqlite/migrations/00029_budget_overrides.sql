-- +goose Up
-- A daily token limit a team or a member set for itself. Without a row the
-- team or member follows the default in the settings; a row of 0 is an
-- explicit "no limit". The settings keep the default, this table the
-- exceptions, so removing a team or member leaves nothing the default
-- depends on.
CREATE TABLE budget_overrides (
    kind         TEXT    NOT NULL CHECK (kind IN ('team', 'bot')),
    id           TEXT    NOT NULL,
    daily_tokens INTEGER NOT NULL CHECK (daily_tokens >= 0),
    updated_at   TEXT    NOT NULL,
    PRIMARY KEY (kind, id)
);

-- +goose Down
DROP TABLE budget_overrides;
