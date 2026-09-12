CREATE TABLE feishu_notifications (
    item_id INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'sent', 'skipped')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL DEFAULT 0,
    sent_at INTEGER NOT NULL DEFAULT 0
);
-- Enabling notifications must not flood the destination with historical items.
INSERT INTO feishu_notifications (item_id, status) SELECT id, 'skipped' FROM items;
CREATE INDEX idx_feishu_pending ON feishu_notifications(next_attempt_at) WHERE status = 'pending';
CREATE TRIGGER items_feishu_ai AFTER INSERT ON items BEGIN
    INSERT INTO feishu_notifications(item_id) VALUES (new.id);
END;
