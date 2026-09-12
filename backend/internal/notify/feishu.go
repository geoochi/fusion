// Package notify delivers unread items to an optional Feishu custom bot.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/0x2E/fusion/internal/store"
	"golang.org/x/net/html"
)

type Worker struct {
	store   *store.Store
	webhook string
	client  *http.Client
}

func New(st *store.Store, webhook string) *Worker {
	return &Worker{store: st, webhook: webhook, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Run uses one serial sender to limit delivery to at most one message per second.
func (w *Worker) Run(ctx context.Context) error {
	if w.webhook == "" {
		<-ctx.Done()
		return nil
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if err := w.deliver(ctx); err != nil && ctx.Err() == nil {
			slog.Error("notification queue failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (w *Worker) deliver(ctx context.Context) error {
	pending, err := w.store.PendingNotifications(time.Now().Unix())
	if err != nil {
		return err
	}
	for _, n := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		unread, err := w.store.NotificationStillUnread(n.ItemID)
		if err != nil {
			return err
		}
		if !unread {
			continue
		}
		if err = w.send(ctx, n); err != nil {
			delay := time.Minute
			for i := 0; i < n.Attempts && delay < time.Hour; i++ {
				delay *= 2
			}
			if delay > time.Hour {
				delay = time.Hour
			}
			if saveErr := w.store.RetryNotification(n.ItemID, time.Now().Add(delay).Unix()); saveErr != nil {
				return saveErr
			}
			slog.Warn("Feishu notification failed; retry scheduled", "item_id", n.ItemID, "error", err)
		} else {
			if err = w.store.CompleteNotification(n.ItemID, time.Now().Unix()); err != nil {
				return err
			}
			slog.Info("Feishu notification sent", "item_id", n.ItemID)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func (w *Worker) send(ctx context.Context, n store.Notification) error {
	payload := map[string]any{"msg_type": "text", "content": map[string]string{"text": message(n)}}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for len(body) > 19000 {
		content := payload["content"].(map[string]string)
		content["text"] = truncate(content["text"], len([]rune(content["text"]))*3/4)
		body, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.webhook, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("invalid webhook request")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := w.client.Do(req)
	// Transport errors can contain the secret webhook URL; never log them verbatim.
	if err != nil {
		return fmt.Errorf("webhook transport failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook HTTP %d", res.StatusCode)
	}
	var result struct {
		Code       *int `json:"code"`
		StatusCode *int `json:"StatusCode"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&result); err != nil {
		return fmt.Errorf("invalid webhook response")
	}
	if result.Code != nil {
		if *result.Code == 0 {
			return nil
		}
		return fmt.Errorf("webhook code %d", *result.Code)
	}
	if result.StatusCode != nil && *result.StatusCode == 0 {
		return nil
	}
	return fmt.Errorf("webhook did not acknowledge delivery")
}

func message(n store.Notification) string {
	text, media := extract(n.Content, n.Link)
	result := ""
	if link := safeURL(n.Link, ""); link != "" {
		result = "原文：" + link + "\n"
	}
	result += "Fusion · " + truncate(n.FeedName, 80)
	if title := strings.TrimSpace(n.Title); title != "" {
		result += "\n" + truncate(title, 80)
	}
	if text != "" {
		result += "\n" + truncate(text, 200)
	}
	for _, m := range media {
		result += "\n" + m
	}
	return result
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func safeURL(raw, base string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if b, err := url.Parse(base); err == nil {
		u = b.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(u.String()) > 2048 {
		return ""
	}
	return u.String()
}

// extract preserves visible text and emits safe media links without downloading media.
func extract(content, base string) (string, []string) {
	doc, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return "", nil
	}
	var text strings.Builder
	var media []string
	seen := map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
			text.WriteByte(' ')
		}
		if n.Type == html.ElementNode && (n.Data == "img" || n.Data == "video" || n.Data == "source") {
			for _, a := range n.Attr {
				if a.Key == "src" || a.Key == "poster" {
					u := safeURL(a.Val, base)
					if u != "" && !seen[u] && len(media) < 6 {
						label := "图片："
						if n.Data == "video" || n.Data == "source" {
							label = "视频/媒体："
						}
						if a.Key == "poster" {
							label = "封面："
						}
						media = append(media, label+u)
						seen[u] = true
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return strings.Join(strings.Fields(text.String()), " "), media
}
