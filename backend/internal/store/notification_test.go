package store

import "testing"

func TestNotificationsPersistAndRespectUnread(t *testing.T) {
	s, path := setupTestDB(t)
	f := mustCreateFeed(t, s, 1, "Feed", "https://example.com/rss", "", "")
	a := mustCreateItem(t, s, f.ID, "a", "A", "https://example.com/a", "", 1)
	b := mustCreateItem(t, s, f.ID, "b", "B", "https://example.com/b", "", 2)
	if err := s.UpdateItemUnread(b.ID, false); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingNotifications(100)
	if err != nil || len(pending) != 1 || pending[0].ItemID != a.ID {
		t.Fatalf("%v %v", pending, err)
	}
	if err := s.RetryNotification(a.ID, 200); err != nil {
		t.Fatal(err)
	}
	pending, err = s.PendingNotifications(100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("%v %v", pending, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(t, s)
	pending, err = s.PendingNotifications(200)
	if err != nil || len(pending) != 1 || pending[0].Attempts != 1 {
		t.Fatalf("%v %v", pending, err)
	}
	if err := s.CompleteNotification(a.ID, 201); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateItemUnread(a.ID, true); err != nil {
		t.Fatal(err)
	}
	pending, err = s.PendingNotifications(300)
	if err != nil || len(pending) != 0 {
		t.Fatalf("duplicate: %v %v", pending, err)
	}
	item, err := s.GetItem(a.ID)
	if err != nil || !item.Unread {
		t.Fatal("delivery changed unread state")
	}
}
