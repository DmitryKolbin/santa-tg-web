package api_test

import (
	"bytes"
	"context"
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

	"github.com/DmitryKolbin/santa-tg-web/internal/api"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	_ "github.com/uptrace/bun/driver/sqliteshim"
)

const testToken = "123:test-secret"

type game = santa.Game
type thread = santa.Thread
type message = santa.Message
type announcementSummary = santa.AnnouncementSummary
type readinessResult = santa.Readiness
type notification struct {
	user int64
	text string
}

type notifierFunc func(user int64, text string)

func (f notifierFunc) Notify(user int64, text string) { f(user, text) }

type testServer struct {
	handler   http.Handler
	locations map[string]string
}

func New(db *sql.DB, token, appURL, botName, uploads string, locations map[string]string, notifier santa.Notifier) (*testServer, error) {
	bunDB := store.New(db)
	if err := store.Migrate(context.Background(), bunDB); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(uploads, 0700); err != nil {
		return nil, err
	}
	svc := santa.New(store.NewStore(bunDB), santa.Options{Uploads: uploads, BotName: botName, Locations: locations, Notifier: notifier})
	return &testServer{handler: api.New(svc, token, appURL), locations: locations}, nil
}

func (s *testServer) Handler() http.Handler { return s.handler }

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API Suite")
}

var _ = Describe("API", func() {
	It("applies CORS only to the configured origin", func() { testCORS(GinkgoT()) })
	It("notifies only after committing", func() { testNotificationRunsAfterCommit(GinkgoT()) })
	It("serves the complete Mini App flow", func() { testMiniAppFlow(GinkgoT()) })
	It("explains shipping dead ends", func() { testReadinessExplainsShippingDeadEnds(GinkgoT()) })
	It("tracks delivery, unread messages and announcements", func() { testDeliveryUnreadAndAnnouncementSummary(GinkgoT()) })
	It("validates forced draws and uploads", func() { testForceDrawAndUploadValidation(GinkgoT()) })
	It("assigns only drawn members when an excluded member sorts first", func() { testForceDrawSkipsExcludedMembers(GinkgoT()) })
})

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
func testCORS(t FullGinkgoTInterface) {
	db, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "cors.db"))
	Expect(err).NotTo(HaveOccurred())
	defer db.Close()
	s, err := New(db, testToken, "https://user.github.io/santa/", "bot", t.TempDir(), map[string]string{"a": "A"}, nil)
	Expect(err).NotTo(HaveOccurred())
	h := s.Handler()
	for _, test := range []struct {
		origin string
		want   string
	}{{"https://user.github.io", "https://user.github.io"}, {"https://evil.example", ""}} {
		req := httptest.NewRequest(http.MethodOptions, "http://test/api/me", nil)
		req.Header.Set("Origin", test.origin)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		Expect(res.Code).To(Equal(http.StatusNoContent), "origin %q", test.origin)
		Expect(res.Header().Get("Access-Control-Allow-Origin")).To(Equal(test.want), "origin %q", test.origin)
	}
}

func testNotificationRunsAfterCommit(t FullGinkgoTInterface) {
	db, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "notifications.db"))
	Expect(err).NotTo(HaveOccurred())
	defer db.Close()
	notified := make(chan bool, 1)
	notifier := notifierFunc(func(int64, string) {
		var count int
		err := db.QueryRow(`SELECT count(*) FROM messages WHERE text='Saved before notification'`).Scan(&count)
		select {
		case notified <- err == nil && count == 1:
		default:
		}
	})
	s, err := New(db, testToken, "https://app.example", "bot", t.TempDir(), map[string]string{"a": "A"}, notifier)
	Expect(err).NotTo(HaveOccurred())
	h := s.Handler()
	g := decodeResponse[game](t, requestJSON(h, 1, "POST", "/games", map[string]any{"name": "Persist"}), 200)
	token := strings.Split(g.InviteURL, "startapp=")[1]
	decodeResponse[any](t, requestJSON(h, 2, "POST", "/invites/"+token+"/join", map[string]any{}), 200)
	path := fmt.Sprintf("/games/%d/announcements", g.ID)
	decodeResponse[any](t, requestJSON(h, 1, "POST", path, map[string]any{"text": "Saved before notification"}), 200)
	var committed bool
	Eventually(notified, time.Second).Should(Receive(&committed))
	Expect(committed).To(BeTrue(), "notification ran before commit")
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
func decodeResponse[T any](t FullGinkgoTInterface, res *httptest.ResponseRecorder, status int) T {
	t.Helper()
	Expect(res.Code).To(Equal(status), res.Body.String())
	var result T
	Expect(json.Unmarshal(res.Body.Bytes(), &result)).To(Succeed())
	return result
}
func testMiniAppFlow(t FullGinkgoTInterface) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "santa.db")
	db, err := sql.Open("sqliteshim", dbPath)
	Expect(err).NotTo(HaveOccurred())
	defer db.Close()
	s, err := New(db, testToken, "https://test.example", "test_bot", filepath.Join(root, "files"), map[string]string{"a": "A", "b": "B"}, nil)
	Expect(err).NotTo(HaveOccurred())
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
	Expect(report.Ready).To(BeTrue(), "report: %#v", report)
	decodeResponse[any](t, requestJSON(h, 7, "PATCH", path, map[string]any{"single_cycle": true}), 200)
	report = decodeResponse[readinessResult](t, requestJSON(h, 7, "POST", path+"/readiness", map[string]any{}), 200)
	Expect(report.Ready).To(BeFalse(), "disconnected graph accepted for single cycle")
	Expect(report.Problems).NotTo(BeEmpty())
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
	Expect(counts).To(HaveKeyWithValue(200, 1))
	Expect(counts).To(HaveKeyWithValue(409, 1))
	decodeResponse[any](t, requestJSON(h, 7, "PATCH", path, map[string]any{"single_cycle": true}), 409)
	var receiver int64
	Expect(db.QueryRow(`SELECT receiver FROM assignments WHERE sender=1`).Scan(&receiver)).To(Succeed())
	recipient := decodeResponse[map[string]string](t, requestJSON(h, 1, "GET", path+"/receiver", nil), 200)
	Expect(recipient).To(HaveKeyWithValue("name", fmt.Sprintf("User %d", receiver)))
	threads := decodeResponse[[]thread](t, requestJSON(h, 1, "GET", path+"/threads", nil), 200)
	Expect(threads).To(HaveLen(2))
	var key string
	for _, th := range threads {
		Expect(th.GameName).To(Equal("Winter"))
		if th.Side == "receiver" {
			key = th.Key
			Expect(th.CounterpartName).To(Equal(fmt.Sprintf("User %d", receiver)))
		} else {
			Expect(th.CounterpartName).To(BeEmpty(), "santa identity leaked")
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
	Expect(messages).To(HaveLen(1))
	Expect(messages[0].Mine).To(BeFalse())
	Expect(messages[0].FileKey).NotTo(BeEmpty())
	Expect(res.Body.String()).NotTo(ContainSubstring("sender"))
	Expect(res.Body.String()).NotTo(ContainSubstring("User 1"))
	filePath := "/files/" + messages[0].FileKey
	decodeResponse[any](t, requestJSON(h, 7, "GET", filePath, nil), 403)
	res = requestJSON(h, receiver, "GET", filePath, nil)
	Expect(res.Code).To(Equal(200))
	Expect(res.Body.String()).To(Equal("hello gift"))
	Expect(res.Header().Get("Content-Disposition")).To(HavePrefix("attachment"))
	// Updating a profile must not remove membership or assignments.
	decodeResponse[any](t, requestJSON(h, 1, "PUT", "/me", map[string]any{"address": "New address", "wishlist": "New wishlist", "location": "a", "destinations": []string{"a"}}), 200)
	current := decodeResponse[game](t, requestJSON(h, 1, "GET", path, nil), 200)
	Expect(current.IsMember).To(BeTrue())
	Expect(current.Count).To(Equal(6))
	decodeResponse[any](t, requestJSON(h, 7, "POST", path+"/announcements", map[string]any{"text": "Send gifts soon"}), 200)
	decodeResponse[any](t, requestJSON(h, 8, "GET", path+"/announcements", nil), 403)
	decodeResponse[any](t, requestJSON(h, 7, "POST", path+"/reset", map[string]any{}), 200)
	old := decodeResponse[[]thread](t, requestJSON(h, 1, "GET", path+"/threads", nil), 200)
	Expect(old[0].ReadOnly).To(BeTrue(), "old thread writable")
	decodeResponse[any](t, requestJSON(h, 1, "POST", threadPath, nil), 409)
	decodeResponse[any](t, requestJSON(h, 1, "GET", path+"/receiver", nil), 409)
	// Reopen the actual on-disk database and check archived files and messages.
	Expect(db.Close()).To(Succeed())
	db2, err := sql.Open("sqliteshim", dbPath)
	Expect(err).NotTo(HaveOccurred())
	defer db2.Close()
	s2, err := New(db2, testToken, "https://test.example", "test_bot", filepath.Join(root, "files"), s.locations, nil)
	Expect(err).NotTo(HaveOccurred())
	res = requestJSON(s2.Handler(), receiver, "GET", filePath, nil)
	Expect(res.Code).To(Equal(200))
	Expect(res.Body.String()).To(Equal("hello gift"), "file lost on restart")
}

func testReadinessExplainsShippingDeadEnds(t FullGinkgoTInterface) {
	db, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "shipping.db"))
	Expect(err).NotTo(HaveOccurred())
	defer db.Close()
	s, err := New(db, testToken, "https://test.example", "bot", t.TempDir(), map[string]string{"a": "Локация A", "b": "Локация B"}, nil)
	Expect(err).NotTo(HaveOccurred())
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
	Expect(report.Ready).To(BeFalse())
	Expect(problems).To(ContainSubstring("User 3"))
	Expect(problems).To(ContainSubstring("Локация B"))
}

func testDeliveryUnreadAndAnnouncementSummary(t FullGinkgoTInterface) {
	db, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "delivery.db"))
	Expect(err).NotTo(HaveOccurred())
	defer db.Close()
	notes := []notification{}
	notifier := notifierFunc(func(user int64, text string) {
		notes = append(notes, notification{user, text})
	})
	s, err := New(db, testToken, "https://test.example", "bot", t.TempDir(), map[string]string{"a": "A"}, notifier)
	Expect(err).NotTo(HaveOccurred())
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
	Expect(db.QueryRow(`SELECT thread FROM assignments WHERE game=? AND sender=1`, game.ID).Scan(&threadKey)).To(Succeed())
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
	Expect(trackingNotice).To(BeTrue(), "delivery notifications: %#v", notes)
	Expect(receivedNotice).To(BeTrue(), "delivery notifications: %#v", notes)
	Expect(canceledNotice).To(BeTrue(), "delivery notifications: %#v", notes)
	_, err = db.Exec(`INSERT INTO messages(thread,game,round,sender,receiver,text) VALUES(?,?,?,?,?,?)`, threadKey, game.ID, 1, 1, 2, "Unread")
	Expect(err).NotTo(HaveOccurred())
	threads := decodeResponse[[]thread](t, requestJSON(h, 2, "GET", gamePath+"/threads", nil), 200)
	var unread int
	for _, item := range threads {
		if item.Key == threadKey {
			unread = item.UnreadCount
			Expect(item.TrackingNumber).To(Equal("TRACK-2"))
			Expect(item.Received).To(BeTrue())
		}
	}
	Expect(unread).To(Equal(1), "threads: %#v", threads)
	for _, item := range decodeResponse[[]thread](t, requestJSON(h, 1, "GET", gamePath+"/threads", nil), 200) {
		if item.Key == threadKey {
			Expect(item.UnreadCount).To(BeZero(), "own message counted unread")
		}
	}
	decodeResponse[[]message](t, requestJSON(h, 2, "GET", "/threads/"+threadKey+"/messages", nil), 200)
	threads = decodeResponse[[]thread](t, requestJSON(h, 2, "GET", gamePath+"/threads", nil), 200)
	for _, item := range threads {
		if item.Key == threadKey {
			Expect(item.UnreadCount).To(BeZero(), "thread remained unread")
		}
	}
	decodeResponse[any](t, requestJSON(h, 10, "POST", gamePath+"/announcements", map[string]any{"text": "Important news"}), 200)
	summary := decodeResponse[announcementSummary](t, requestJSON(h, 2, "GET", gamePath+"/announcements/latest", nil), 200)
	Expect(summary.UnreadCount).To(Equal(1))
	Expect(summary.Latest).NotTo(BeNil())
	Expect(summary.Latest.Text).To(Equal("Important news"))
	ownerSummary := decodeResponse[announcementSummary](t, requestJSON(h, 10, "GET", gamePath+"/announcements/latest", nil), 200)
	Expect(ownerSummary.UnreadCount).To(BeZero(), "own announcement counted unread")
	summary = decodeResponse[announcementSummary](t, requestJSON(h, 2, "GET", gamePath+"/announcements/latest", nil), 200)
	Expect(summary.UnreadCount).To(Equal(1), "summary marked announcement read")
	decodeResponse[[]message](t, requestJSON(h, 2, "GET", gamePath+"/announcements", nil), 200)
	summary = decodeResponse[announcementSummary](t, requestJSON(h, 2, "GET", gamePath+"/announcements/latest", nil), 200)
	Expect(summary.UnreadCount).To(BeZero(), "announcement remained unread")
}

func testForceDrawAndUploadValidation(t FullGinkgoTInterface) {
	db, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "force.db"))
	Expect(err).NotTo(HaveOccurred())
	defer db.Close()
	uploads := t.TempDir()
	s, err := New(db, testToken, "https://test.example", "bot", uploads, map[string]string{"a": "A"}, nil)
	Expect(err).NotTo(HaveOccurred())
	h := s.Handler()
	g := decodeResponse[game](t, requestJSON(h, 10, "POST", "/games", map[string]any{"name": "Force"}), 200)
	Expect(g.SingleCycle).To(BeTrue())
	Expect(g.AllowMutualPairs).To(BeTrue())
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
	Expect(report.Excluded).To(Equal([]string{"User 3"}))
	decodeResponse[any](t, requestJSON(h, 3, "GET", path+"/receiver", nil), 404)
	decodeResponse[any](t, requestJSON(h, 4, "POST", "/invites/"+token+"/join", map[string]any{}), 409)
	// In a mutual pair there are two separate conversations, one in each direction.
	threads := decodeResponse[[]thread](t, requestJSON(h, 1, "GET", path+"/threads", nil), 200)
	Expect(threads).To(HaveLen(2))
	Expect(threads[0].Side).NotTo(Equal(threads[1].Side))
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
	Expect(err).NotTo(HaveOccurred())
	Expect(entries).To(BeEmpty(), "rejected uploads left files")
	decodeResponse[any](t, upload(1, "Question", 0, 0), 200)
	decodeResponse[any](t, upload(2, "Answer", 0, 0), 200)
	messages := decodeResponse[[]message](t, requestJSON(h, 1, "GET", threadPath, nil), 200)
	Expect(messages).To(HaveLen(2))
	Expect(messages[0].Mine).To(BeTrue())
	Expect(messages[1].Mine).To(BeFalse())
	// Assignment identity is never serialized, including to the recipient.
	raw := requestJSON(h, 2, "GET", path+"/threads", nil).Body.String()
	for _, field := range []string{`"sender"`, `"receiver"`, `"creator"`, `"user"`} {
		Expect(raw).NotTo(ContainSubstring(field+":"), "identity field exposed")
	}
	// A file staged before an insert failure is removed with the rolled-back message.
	_, err = db.Exec(`CREATE TRIGGER reject_message BEFORE INSERT ON messages BEGIN SELECT RAISE(ABORT,'simulated write failure'); END`)
	Expect(err).NotTo(HaveOccurred())
	decodeResponse[any](t, upload(1, "", 10, 1), 500)
	entries, err = os.ReadDir(uploads)
	Expect(err).NotTo(HaveOccurred())
	Expect(entries).To(BeEmpty(), "failed insert left files")
	// Missing and expired Telegram data are rejected before entering the API.
	for _, raw := range []string{"", signed(1, time.Now().Add(-2*time.Hour).Unix())} {
		req := httptest.NewRequest("GET", "http://test/api/me", nil)
		req.Header.Set("Authorization", "tma "+raw)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		decodeResponse[any](t, res, 401)
	}
}

func testForceDrawSkipsExcludedMembers(t FullGinkgoTInterface) {
	db, err := sql.Open("sqliteshim", filepath.Join(t.TempDir(), "excluded.db"))
	Expect(err).NotTo(HaveOccurred())
	defer db.Close()
	s, err := New(db, testToken, "https://test.example", "bot", t.TempDir(), map[string]string{"a": "A"}, nil)
	Expect(err).NotTo(HaveOccurred())
	h := s.Handler()
	g := decodeResponse[game](t, requestJSON(h, 10, "POST", "/games", map[string]any{"name": "Excluded"}), 200)
	token := strings.Split(g.InviteURL, "startapp=")[1]
	path := fmt.Sprintf("/games/%d", g.ID)
	// User 1 has the lowest id and no questionnaire, so it is excluded.
	for id := int64(1); id <= 3; id++ {
		if id > 1 {
			decodeResponse[any](t, requestJSON(h, id, "PUT", "/me", map[string]any{"address": "Address", "location": "a", "destinations": []string{"a"}}), 200)
		}
		decodeResponse[any](t, requestJSON(h, id, "POST", "/invites/"+token+"/join", map[string]any{}), 200)
	}
	ready := decodeResponse[readinessResult](t, requestJSON(h, 10, "POST", path+"/readiness", map[string]any{"force": true}), 200)
	Expect(ready.Excluded).To(Equal([]string{"User 1"}))
	decodeResponse[any](t, requestJSON(h, 10, "POST", path+"/draw", map[string]any{"force": true, "exclusions": ready.Exclusions}), 200)
	rows, err := db.Query(`SELECT sender, receiver FROM assignments WHERE game=?`, g.ID)
	Expect(err).NotTo(HaveOccurred())
	defer rows.Close()
	pairs := map[int64]int64{}
	for rows.Next() {
		var sender, receiver int64
		Expect(rows.Scan(&sender, &receiver)).To(Succeed())
		pairs[sender] = receiver
	}
	Expect(pairs).To(Equal(map[int64]int64{2: 3, 3: 2}))
}
