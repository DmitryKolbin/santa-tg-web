package mini

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/uptrace/bun/driver/sqliteshim"
)

const testToken = "123:test-secret"

func signed(id int64, stamp int64) string {
	values := url.Values{"auth_date": {strconv.FormatInt(stamp, 10)}, "user": {fmt.Sprintf(`{"id":%d,"first_name":"User %d"}`, id, id)}}
	var pairs []string
	for key := range values {
		pairs = append(pairs, key+"="+values.Get(key))
	}
	sort.Strings(pairs)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(testToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(pairs, "\n")))
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return values.Encode()
}
func TestInitData(t *testing.T) {
	now := time.Now()
	raw := signed(1, now.Unix())
	if _, err := validateInitData(raw, testToken, now); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", raw + "&user=x", strings.Replace(raw, "User", "Fake", 1), signed(1, now.Add(-2*time.Hour).Unix()), signed(1, now.Add(time.Minute).Unix()), signed(0, now.Unix())} {
		if _, err := validateInitData(invalid, testToken, now); err == nil {
			t.Fatal("accepted invalid initData")
		}
	}
}

func TestCORS(t *testing.T) {
	db, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "cors.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := New(db, testToken, "https://user.github.io/santa/", "bot", t.TempDir(), map[string]string{"a": "A"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	for _, test := range []struct {
		origin string
		want   string
	}{{"https://user.github.io", "https://user.github.io"}, {"https://evil.example", ""}} {
		req := httptest.NewRequest(http.MethodOptions, "http://test/api/me", nil)
		req.Header.Set("Origin", test.origin)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != http.StatusNoContent || res.Header().Get("Access-Control-Allow-Origin") != test.want {
			t.Fatalf("origin %q: status %d, allow-origin %q", test.origin, res.Code, res.Header().Get("Access-Control-Allow-Origin"))
		}
	}
}
func requestJSON(handler http.Handler, id int64, method, path string, body any) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, "http://test/api"+path, reader)
	req.Header.Set("Authorization", "tma "+signed(id, time.Now().Unix()))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}
func decodeResponse[T any](t *testing.T, res *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if res.Code != status {
		t.Fatalf("status %d, want %d: %s", res.Code, status, res.Body.String())
	}
	var result T
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestMiniAppFlow(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "santa.db")
	db, err := sql.Open("sqliteshim", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := New(db, testToken, "https://test.example", "test_bot", filepath.Join(root, "files"), map[string]string{"a": "A", "b": "B"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	// Login and profiles: six players in two geographically disconnected circles.
	for id := int64(1); id <= 7; id++ {
		loc := "a"
		if id >= 4 {
			loc = "b"
		}
		decodeResponse[any](t, requestJSON(h, id, "PUT", "/me", map[string]any{"address": "Address", "wishlist": "Wishlist", "location": loc, "destinations": []string{loc}}), 200)
	}
	g := decodeResponse[game](t, requestJSON(h, 7, "POST", "/games", map[string]any{"name": "Winter", "single_cycle": false, "allow_mutual_pairs": false}), 200)
	token := strings.Split(g.InviteURL, "startapp=")[1]
	for id := int64(1); id <= 6; id++ {
		decodeResponse[any](t, requestJSON(h, id, "POST", "/invites/"+token+"/join", map[string]any{}), 200)
	}
	decodeResponse[any](t, requestJSON(h, 1, "POST", "/invites/"+token+"/join", map[string]any{}), 200)
	path := fmt.Sprintf("/games/%d", g.ID)
	decodeResponse[any](t, requestJSON(h, 8, "GET", path, nil), 403)
	decodeResponse[any](t, requestJSON(h, 1, "PATCH", path, map[string]any{"single_cycle": true}), 403)
	decodeResponse[any](t, requestJSON(h, 1, "POST", path+"/reset", map[string]any{}), 403)
	report := decodeResponse[readinessResult](t, requestJSON(h, 7, "POST", path+"/readiness", map[string]any{"force": false}), 200)
	if !report.Ready {
		t.Fatal(report)
	}
	decodeResponse[any](t, requestJSON(h, 7, "PATCH", path, map[string]any{"single_cycle": true}), 200)
	report = decodeResponse[readinessResult](t, requestJSON(h, 7, "POST", path+"/readiness", map[string]any{}), 200)
	if report.Ready || len(report.Problems) == 0 {
		t.Fatal("disconnected graph accepted for single cycle")
	}
	decodeResponse[any](t, requestJSON(h, 7, "PATCH", path, map[string]any{"single_cycle": false}), 200)
	// Concurrent draws: exactly one succeeds and no duplicate assignments appear.
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); statuses <- requestJSON(h, 7, "POST", path+"/draw", map[string]any{}).Code }()
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	decodeResponse[any](t, requestJSON(h, 7, "PATCH", path, map[string]any{"single_cycle": true}), 409)
	var receiver int64
	if err := db.QueryRow(`SELECT receiver FROM assignments WHERE sender=1`).Scan(&receiver); err != nil {
		t.Fatal(err)
	}
	recipient := decodeResponse[map[string]string](t, requestJSON(h, 1, "GET", path+"/receiver", nil), 200)
	if recipient["name"] != fmt.Sprintf("User %d", receiver) {
		t.Fatal(recipient)
	}
	threads := decodeResponse[[]thread](t, requestJSON(h, 1, "GET", path+"/threads", nil), 200)
	if len(threads) != 2 {
		t.Fatal(threads)
	}
	var key string
	for _, th := range threads {
		if th.GameName != "Winter" {
			t.Fatal("missing game name", th)
		}
		if th.Side == "receiver" {
			key = th.Key
			if th.CounterpartName != fmt.Sprintf("User %d", receiver) {
				t.Fatal("missing receiver name", th)
			}
		} else if th.CounterpartName != "" {
			t.Fatal("santa identity leaked", th)
		}
	}
	threadPath := "/threads/" + key + "/messages"
	decodeResponse[any](t, requestJSON(h, 7, "GET", threadPath, nil), 403)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writer.WriteField("text", "What do you like?")
	file, _ := writer.CreateFormFile("file", "gift.txt")
	file.Write([]byte("hello gift"))
	writer.Close()
	req := httptest.NewRequest("POST", "http://test/api"+threadPath, &body)
	req.Header.Set("Authorization", "tma "+signed(1, time.Now().Unix()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	decodeResponse[any](t, res, 200)
	messages := decodeResponse[[]message](t, requestJSON(h, receiver, "GET", threadPath, nil), 200)
	if len(messages) != 1 || messages[0].Mine || messages[0].FileKey == "" {
		t.Fatal(messages)
	}
	if strings.Contains(res.Body.String(), "sender") || strings.Contains(res.Body.String(), "User 1") {
		t.Fatal("identity leak")
	}
	filePath := "/files/" + messages[0].FileKey
	decodeResponse[any](t, requestJSON(h, 7, "GET", filePath, nil), 403)
	res = requestJSON(h, receiver, "GET", filePath, nil)
	if res.Code != 200 || res.Body.String() != "hello gift" {
		t.Fatal(res.Code, res.Body.String())
	}
	if !strings.HasPrefix(res.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("unsafe file response")
	}
	// Updating a profile must not remove membership or assignments.
	decodeResponse[any](t, requestJSON(h, 1, "PUT", "/me", map[string]any{"address": "New address", "wishlist": "New wishlist", "location": "a", "destinations": []string{"a"}}), 200)
	current := decodeResponse[game](t, requestJSON(h, 1, "GET", path, nil), 200)
	if !current.IsMember || current.Count != 6 {
		t.Fatal(current)
	}
	decodeResponse[any](t, requestJSON(h, 7, "POST", path+"/announcements", map[string]any{"text": "Send gifts soon"}), 200)
	decodeResponse[any](t, requestJSON(h, 8, "GET", path+"/announcements", nil), 403)
	decodeResponse[any](t, requestJSON(h, 7, "POST", path+"/reset", map[string]any{}), 200)
	old := decodeResponse[[]thread](t, requestJSON(h, 1, "GET", path+"/threads", nil), 200)
	if !old[0].ReadOnly {
		t.Fatal("old thread writable")
	}
	decodeResponse[any](t, requestJSON(h, 1, "POST", threadPath, nil), 409)
	decodeResponse[any](t, requestJSON(h, 1, "GET", path+"/receiver", nil), 409)
	// Reopen the actual on-disk database and check archived files and messages.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := sql.Open("sqliteshim", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	s2, err := New(db2, testToken, "https://test.example", "test_bot", filepath.Join(root, "files"), s.locations, nil)
	if err != nil {
		t.Fatal(err)
	}
	res = requestJSON(s2.Handler(), receiver, "GET", filePath, nil)
	if res.Code != 200 || res.Body.String() != "hello gift" {
		t.Fatal("file lost on restart")
	}
}

func TestReadinessExplainsShippingDeadEnds(t *testing.T) {
	db, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "shipping.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := New(db, testToken, "https://test.example", "bot", t.TempDir(), map[string]string{"a": "Локация A", "b": "Локация B"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	game := decodeResponse[game](t, requestJSON(h, 10, "POST", "/games", map[string]any{"name": "Shipping"}), 200)
	token := strings.Split(game.InviteURL, "startapp=")[1]
	for id := int64(1); id <= 3; id++ {
		location := "a"
		if id == 3 {
			location = "b"
		}
		decodeResponse[any](t, requestJSON(h, id, "PUT", "/me", map[string]any{"address": "Address", "location": location, "destinations": []string{location}}), 200)
		decodeResponse[any](t, requestJSON(h, id, "POST", "/invites/"+token+"/join", map[string]any{}), 200)
	}
	report := decodeResponse[readinessResult](t, requestJSON(h, 10, "POST", fmt.Sprintf("/games/%d/readiness", game.ID), map[string]any{}), 200)
	problems := strings.Join(report.Problems, " ")
	if report.Ready || !strings.Contains(problems, "User 3") || !strings.Contains(problems, "Локация B") {
		t.Fatal(report)
	}
}

func TestDeliveryUnreadAndAnnouncementSummary(t *testing.T) {
	db, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	notes := []notification{}
	s, err := New(db, testToken, "https://test.example", "bot", t.TempDir(), map[string]string{"a": "A"}, func(user int64, text string) {
		notes = append(notes, notification{user, text})
	})
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	game := decodeResponse[game](t, requestJSON(h, 10, "POST", "/games", map[string]any{"name": "Delivery"}), 200)
	token := strings.Split(game.InviteURL, "startapp=")[1]
	for id := int64(1); id <= 2; id++ {
		decodeResponse[any](t, requestJSON(h, id, "PUT", "/me", map[string]any{"address": "Address", "location": "a", "destinations": []string{"a"}}), 200)
		decodeResponse[any](t, requestJSON(h, id, "POST", "/invites/"+token+"/join", map[string]any{}), 200)
	}
	gamePath := fmt.Sprintf("/games/%d", game.ID)
	decodeResponse[any](t, requestJSON(h, 10, "POST", gamePath+"/draw", map[string]any{}), 200)
	var threadKey string
	if err := db.QueryRow(`SELECT thread FROM assignments WHERE game=? AND sender=1`, game.ID).Scan(&threadKey); err != nil {
		t.Fatal(err)
	}
	trackingPath := "/threads/" + threadKey + "/tracking"
	receivedPath := "/threads/" + threadKey + "/received"
	decodeResponse[any](t, requestJSON(h, 2, "PUT", trackingPath, map[string]any{"tracking_number": "NO"}), 403)
	decodeResponse[any](t, requestJSON(h, 1, "PUT", trackingPath, map[string]any{"tracking_number": "TRACK-1"}), 200)
	decodeResponse[any](t, requestJSON(h, 1, "POST", receivedPath, map[string]any{"received": true}), 403)
	decodeResponse[any](t, requestJSON(h, 2, "POST", receivedPath, map[string]any{"received": true}), 200)
	decodeResponse[any](t, requestJSON(h, 1, "PUT", trackingPath, map[string]any{"tracking_number": "TRACK-2"}), 409)
	decodeResponse[any](t, requestJSON(h, 2, "POST", receivedPath, map[string]any{"received": false}), 200)
	decodeResponse[any](t, requestJSON(h, 10, "POST", gamePath+"/reset", map[string]any{}), 200)
	decodeResponse[any](t, requestJSON(h, 1, "PUT", trackingPath, map[string]any{"tracking_number": "TRACK-2"}), 200)
	decodeResponse[any](t, requestJSON(h, 2, "POST", receivedPath, map[string]any{"received": true}), 200)
	trackingNotice, receivedNotice, canceledNotice := false, false, false
	for _, note := range notes {
		trackingNotice = trackingNotice || note.user == 2 && strings.Contains(note.text, "TRACK-2")
		receivedNotice = receivedNotice || note.user == 1 && strings.Contains(note.text, "подтвердил(а)")
		canceledNotice = canceledNotice || note.user == 1 && strings.Contains(note.text, "отменил(а)")
	}
	if !trackingNotice || !receivedNotice || !canceledNotice {
		t.Fatal("delivery notifications", notes)
	}
	if _, err := db.Exec(`INSERT INTO messages(thread,game,round,sender,receiver,text) VALUES(?,?,?,?,?,?)`, threadKey, game.ID, 1, 1, 2, "Unread"); err != nil {
		t.Fatal(err)
	}
	threads := decodeResponse[[]thread](t, requestJSON(h, 2, "GET", gamePath+"/threads", nil), 200)
	var unread int
	for _, item := range threads {
		if item.Key == threadKey {
			unread = item.UnreadCount
			if item.TrackingNumber != "TRACK-2" || !item.Received {
				t.Fatal("delivery state", item)
			}
		}
	}
	if unread != 1 {
		t.Fatal("unread thread", threads)
	}
	for _, item := range decodeResponse[[]thread](t, requestJSON(h, 1, "GET", gamePath+"/threads", nil), 200) {
		if item.Key == threadKey && item.UnreadCount != 0 {
			t.Fatal("own message counted unread", item)
		}
	}
	decodeResponse[[]message](t, requestJSON(h, 2, "GET", "/threads/"+threadKey+"/messages", nil), 200)
	threads = decodeResponse[[]thread](t, requestJSON(h, 2, "GET", gamePath+"/threads", nil), 200)
	for _, item := range threads {
		if item.Key == threadKey && item.UnreadCount != 0 {
			t.Fatal("thread remained unread", item)
		}
	}
	decodeResponse[any](t, requestJSON(h, 10, "POST", gamePath+"/announcements", map[string]any{"text": "Important news"}), 200)
	summary := decodeResponse[announcementSummary](t, requestJSON(h, 2, "GET", gamePath+"/announcements/latest", nil), 200)
	if summary.UnreadCount != 1 || summary.Latest == nil || summary.Latest.Text != "Important news" {
		t.Fatal(summary)
	}
	ownerSummary := decodeResponse[announcementSummary](t, requestJSON(h, 10, "GET", gamePath+"/announcements/latest", nil), 200)
	if ownerSummary.UnreadCount != 0 {
		t.Fatal("own announcement counted unread", ownerSummary)
	}
	summary = decodeResponse[announcementSummary](t, requestJSON(h, 2, "GET", gamePath+"/announcements/latest", nil), 200)
	if summary.UnreadCount != 1 {
		t.Fatal("summary marked announcement read", summary)
	}
	decodeResponse[[]message](t, requestJSON(h, 2, "GET", gamePath+"/announcements", nil), 200)
	summary = decodeResponse[announcementSummary](t, requestJSON(h, 2, "GET", gamePath+"/announcements/latest", nil), 200)
	if summary.UnreadCount != 0 {
		t.Fatal("announcement remained unread", summary)
	}
}

func TestForceDrawAndUploadValidation(t *testing.T) {
	db, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "force.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	uploads := t.TempDir()
	s, err := New(db, testToken, "https://test.example", "bot", uploads, map[string]string{"a": "A"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	g := decodeResponse[game](t, requestJSON(h, 10, "POST", "/games", map[string]any{"name": "Force"}), 200)
	if !g.SingleCycle || !g.AllowMutualPairs {
		t.Fatal("wrong defaults")
	}
	token := strings.Split(g.InviteURL, "startapp=")[1]
	path := fmt.Sprintf("/games/%d", g.ID)
	for id := int64(1); id <= 3; id++ {
		decodeResponse[any](t, requestJSON(h, id, "POST", "/invites/"+token+"/join", map[string]any{}), 200)
	}
	decodeResponse[any](t, requestJSON(h, 10, "POST", path+"/draw", map[string]any{"force": true}), 422)
	for id := int64(1); id <= 2; id++ {
		decodeResponse[any](t, requestJSON(h, id, "PUT", "/me", map[string]any{"address": "Address", "location": "a", "destinations": []string{"a"}}), 200)
	}
	decodeResponse[any](t, requestJSON(h, 1, "PUT", "/me", map[string]any{"address": "Address", "location": "unknown", "destinations": []string{"a"}}), 400)
	decodeResponse[any](t, requestJSON(h, 10, "POST", path+"/draw", map[string]any{}), 422)
	decodeResponse[any](t, requestJSON(h, 10, "PATCH", path, map[string]any{"allow_mutual_pairs": false}), 200)
	decodeResponse[any](t, requestJSON(h, 10, "POST", path+"/draw", map[string]any{"force": true}), 422)
	decodeResponse[any](t, requestJSON(h, 10, "PATCH", path, map[string]any{"allow_mutual_pairs": true}), 200)
	ready := decodeResponse[readinessResult](t, requestJSON(h, 10, "POST", path+"/readiness", map[string]any{"force": true}), 200)
	decodeResponse[any](t, requestJSON(h, 10, "POST", path+"/draw", map[string]any{"force": true, "exclusions": "stale"}), 409)
	report := decodeResponse[readinessResult](t, requestJSON(h, 10, "POST", path+"/draw", map[string]any{"force": true, "exclusions": ready.Exclusions}), 200)
	if len(report.Excluded) != 1 || report.Excluded[0] != "User 3" {
		t.Fatal(report)
	}
	decodeResponse[any](t, requestJSON(h, 3, "GET", path+"/receiver", nil), 404)
	decodeResponse[any](t, requestJSON(h, 4, "POST", "/invites/"+token+"/join", map[string]any{}), 409)
	// In a mutual pair there are two separate conversations, one in each direction.
	threads := decodeResponse[[]thread](t, requestJSON(h, 1, "GET", path+"/threads", nil), 200)
	if len(threads) != 2 || threads[0].Side == threads[1].Side {
		t.Fatal(threads)
	}
	var key string
	for _, th := range threads {
		if th.Side == "receiver" {
			key = th.Key
		}
	}
	threadPath := "/threads/" + key + "/messages"
	upload := func(id int64, text string, size int, count int) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		writer.WriteField("text", text)
		for range count {
			part, _ := writer.CreateFormFile("file", "test.txt")
			part.Write(bytes.Repeat([]byte("a"), size))
		}
		writer.Close()
		req := httptest.NewRequest("POST", "http://test/api"+threadPath, &body)
		req.Header.Set("Authorization", "tma "+signed(id, time.Now().Unix()))
		req.Header.Set("Content-Type", writer.FormDataContentType())
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	decodeResponse[any](t, upload(1, "", 0, 0), 400)
	decodeResponse[any](t, upload(1, "", 0, 1), 400)
	decodeResponse[any](t, upload(1, "", 1, 2), 400)
	decodeResponse[any](t, upload(1, "", (20<<20)+1, 1), 400)
	decodeResponse[any](t, upload(3, "intruder", 1, 1), 403)
	entries, err := os.ReadDir(uploads)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected uploads left files", entries, err)
	}
	decodeResponse[any](t, upload(1, "Question", 0, 0), 200)
	decodeResponse[any](t, upload(2, "Answer", 0, 0), 200)
	messages := decodeResponse[[]message](t, requestJSON(h, 1, "GET", threadPath, nil), 200)
	if len(messages) != 2 || !messages[0].Mine || messages[1].Mine {
		t.Fatal(messages)
	}
	// Assignment identity is never serialized, including to the recipient.
	raw := requestJSON(h, 2, "GET", path+"/threads", nil).Body.String()
	for _, field := range []string{`"sender"`, `"receiver"`, `"creator"`, `"user"`} {
		if strings.Contains(raw, field+":") {
			t.Fatal("identity field exposed", raw)
		}
	}
	// A file staged before an insert failure is removed with the rolled-back message.
	if _, err := db.Exec(`CREATE TRIGGER reject_message BEFORE INSERT ON messages BEGIN SELECT RAISE(ABORT,'simulated write failure'); END`); err != nil {
		t.Fatal(err)
	}
	decodeResponse[any](t, upload(1, "", 10, 1), 500)
	entries, err = os.ReadDir(uploads)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed insert left files", entries, err)
	}
	// Missing and expired Telegram data are rejected before entering the API.
	for _, raw := range []string{"", signed(1, time.Now().Add(-2*time.Hour).Unix())} {
		req := httptest.NewRequest("GET", "http://test/api/me", nil)
		req.Header.Set("Authorization", "tma "+raw)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		decodeResponse[any](t, res, 401)
	}
}
