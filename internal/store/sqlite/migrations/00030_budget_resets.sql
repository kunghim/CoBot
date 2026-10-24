-- +goose Up
-- When a team's or a member's count of today's spend was last started again by
-- hand. The budget counts from the later of local midnight and this moment; the
-- ledger itself is never touched.
CREATE TABLE budget_resets (
    kind     TEXT NOT NULL CHECK (kind IN ('team', 'bot')),
    id       TEXT NOT NULL,
    reset_at TEXT NOT NULL,
    PRIMARY KEY (kind, id)
);

-- +goose Down
DROP TABLE budget_resets;
