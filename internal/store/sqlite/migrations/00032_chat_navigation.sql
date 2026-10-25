-- +goose Up
CREATE TABLE chat_navigation (
    id INTEGER PRIMARY KEY CHECK (id=1),
    data TEXT NOT NULL
);
INSERT INTO chat_navigation(id,data) VALUES(1,'{"revision":1,"sort":"recent","grouping":"sections","sections":[]}');
UPDATE chats SET data=json_set(data,'$.pinned',json('false'),'$.manualOrder',rowid);
CREATE INDEX idx_chats_archived ON chats(COALESCE(json_extract(data,'$.archivedAt'),''));

-- +goose Down
DROP INDEX idx_chats_archived;
DROP TABLE chat_navigation;
