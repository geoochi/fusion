package store

import "database/sql"

// Notification is an unread item waiting for delivery to the configured webhook.
type Notification struct {
	ItemID   int64
	FeedName string
	Title    string
	Link     string
	Content  string
	Attempts int
}

func (s *Store) PendingNotifications(now int64) ([]Notification, error) {
	rows, err := s.db.Query(`SELECT i.id, f.name, i.title, i.link, i.content, n.attempts
 FROM feishu_notifications n JOIN items i ON i.id = n.item_id JOIN feeds f ON f.id = i.feed_id
 WHERE n.status = 'pending' AND n.next_attempt_at <= :now AND i.unread = 1
 ORDER BY i.id LIMIT 20`, sql.Named("now", now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ItemID, &n.FeedName, &n.Title, &n.Link, &n.Content, &n.Attempts); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

func (s *Store) NotificationStillUnread(id int64) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT count(*) FROM items i JOIN feishu_notifications n ON n.item_id=i.id
 WHERE i.id=:id AND i.unread=1 AND n.status='pending'`, sql.Named("id", id)).Scan(&count)
	return count > 0, err
}

func (s *Store) CompleteNotification(id, now int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE feishu_notifications SET status='sent', sent_at=:now WHERE item_id=:id AND status='pending'`, sql.Named("now", now), sql.Named("id", id))
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 {
		if _, err = tx.Exec(`UPDATE items SET unread=0 WHERE id=:id`, sql.Named("id", id)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) RetryNotification(id, next int64) error {
	_, err := s.db.Exec(`UPDATE feishu_notifications SET attempts=attempts+1, next_attempt_at=:next WHERE item_id=:id`, sql.Named("next", next), sql.Named("id", id))
	return err
}
