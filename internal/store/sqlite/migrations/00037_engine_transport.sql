-- +goose Up
-- How the engine reaches the official account: auto lets Codex try a
-- WebSocket first and fall back to HTTPS; http never opens a WebSocket, for
-- networks where the handshake only times out.
ALTER TABLE settings ADD COLUMN engine_transport TEXT NOT NULL DEFAULT 'auto';

-- +goose Down
ALTER TABLE settings DROP COLUMN engine_transport;
