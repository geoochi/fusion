package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0x2E/fusion/internal/store"
)

func TestMessageExtractsMediaAndUntitledPosts(t *testing.T) {
	n := store.Notification{FeedName: "Tao", Link: "https://example.com/post", Content: `<p>Hello &amp; world</p><script>secret</script><img src="/photo.jpg"><img src="/photo.jpg"><video src="/clip.mp4"></video><img src="javascript:alert(1)">`}
	msg := message(n)
	for _, part := range []string{"Hello & world", "https://example.com/photo.jpg", "https://example.com/clip.mp4", "原文：https://example.com/post"} {
		if !strings.Contains(msg, part) {
			t.Fatalf("missing %q in %q", part, msg)
		}
	}
	if strings.Contains(msg, "secret") || strings.Contains(msg, "javascript:") || strings.Count(msg, "photo.jpg") != 1 {
		t.Fatal(msg)
	}
}

func TestSendChecksFeishuAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		ok     bool
	}{
		{`{"code":0}`, 200, true}, {`{"StatusCode":0}`, 200, true}, {`{"code":19024}`, 200, false}, {`{}`, 200, false}, {`oops`, 200, false}, {`{"code":0}`, 429, false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload["msg_type"] != "text" {
					t.Error(payload)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			err := New(nil, server.URL).send(context.Background(), store.Notification{Title: "test"})
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestMessageLinkFirstAndShortSummary(t *testing.T) {
	msg := message(store.Notification{FeedName: "Tao", Link: "https://example.com/post", Content: strings.Repeat("文", 250)})
	lines := strings.Split(msg, "\n")
	if lines[0] != "原文：https://example.com/post" || len(lines) != 3 {
		t.Fatal(msg)
	}
	if len([]rune(lines[2])) != 201 || !strings.HasSuffix(lines[2], "…") {
		t.Fatal(msg)
	}
	short := message(store.Notification{Content: "short text"})
	if strings.Contains(short, "…") {
		t.Fatal(short)
	}
}
