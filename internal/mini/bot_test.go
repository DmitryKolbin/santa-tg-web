package mini

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type localTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t localTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	copyURL := *req.URL
	clone.URL = &copyURL
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	return t.base.RoundTrip(clone)
}
func TestBotConfigurationAndFailedDelivery(t *testing.T) {
	delivered := make(chan map[string]any, 1)
	var mu sync.Mutex
	polls := 0
	configured := map[string]bool{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		switch method {
		case "getMe":
			io.WriteString(w, `{"ok":true,"result":{"username":"test_bot"}}`)
		case "setChatMenuButton", "setMyCommands":
			mu.Lock()
			configured[method] = true
			mu.Unlock()
			if method == "setChatMenuButton" {
				button := body["menu_button"].(map[string]any)
				if button["type"] != "web_app" {
					t.Error("wrong menu type")
				}
			}
			io.WriteString(w, `{"ok":true,"result":true}`)
		case "getUpdates":
			mu.Lock()
			polls++
			first := polls == 1
			mu.Unlock()
			if first {
				io.WriteString(w, `{"ok":true,"result":[{"update_id":1,"message":{"text":"/start","chat":{"id":42,"type":"private"}}}]}`)
			} else {
				<-r.Context().Done()
			}
		case "sendMessage":
			delivered <- body
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"ok":false,"error_code":403}`)
		default:
			t.Error("unexpected Telegram method", method)
		}
	}))
	defer remote.Close()
	target, _ := url.Parse(remote.URL)
	bot := NewBot(testToken, "https://app.example")
	bot.client = &http.Client{Transport: localTransport{target, http.DefaultTransport}, Timeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	name, err := bot.Configure(ctx)
	if err != nil || name != "test_bot" {
		t.Fatal(name, err)
	}
	mu.Lock()
	ready := configured["setChatMenuButton"] && configured["setMyCommands"]
	mu.Unlock()
	if !ready {
		t.Fatal("incomplete bot setup")
	}
	done := make(chan struct{})
	go func() { bot.Run(ctx); close(done) }()
	select {
	case body := <-delivered:
		if body["chat_id"] != float64(42) || body["reply_markup"] == nil {
			t.Fatal(body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("start did not open app")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("polling did not stop")
	}
	// Failed notifications are independent from saved API actions.
	db, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := New(db, testToken, "https://app.example", "bot", t.TempDir(), map[string]string{"a": "A"}, bot.Notify)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	g := decodeResponse[game](t, requestJSON(h, 1, "POST", "/games", map[string]any{"name": "Persist"}), 200)
	token := strings.Split(g.InviteURL, "startapp=")[1]
	decodeResponse[any](t, requestJSON(h, 2, "POST", "/invites/"+token+"/join", map[string]any{}), 200)
	path := fmt.Sprintf("/games/%d/announcements", g.ID)
	decodeResponse[any](t, requestJSON(h, 1, "POST", path, map[string]any{"text": "Saved even with bot offline"}), 200)
	messages := decodeResponse[[]message](t, requestJSON(h, 2, "GET", path, nil), 200)
	if len(messages) != 1 {
		t.Fatal("announcement not saved")
	}
}
