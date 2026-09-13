package mini

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type thread struct {
	Key      string `json:"key"`
	Game     int64  `json:"-"`
	Round    int    `json:"round"`
	Sender   int64  `json:"-"`
	Receiver int64  `json:"-"`
	Side     string `json:"side"`
	ReadOnly bool   `json:"read_only"`
}

func (q *request) thread() (thread, error) {
	var t thread
	var round int
	var started bool
	err := q.tx.QueryRowContext(q.r.Context(), `SELECT a.thread,a.game,a.round,a.sender,a.receiver,g.round,g.started FROM assignments a JOIN games g ON g.id=a.game WHERE a.thread=?`, q.r.PathValue("thread")).Scan(&t.Key, &t.Game, &t.Round, &t.Sender, &t.Receiver, &round, &started)
	if err != nil {
		return t, err
	}
	if q.user != t.Sender && q.user != t.Receiver {
		return t, forbidden()
	}
	t.ReadOnly = !started || round != t.Round
	return t, nil
}
func listThreads(q *request) (any, error) {
	g, err := q.game()
	if err != nil {
		return nil, err
	}
	rows, err := q.tx.QueryContext(q.r.Context(), `SELECT thread,round,sender FROM assignments WHERE game=? AND (sender=? OR receiver=?) ORDER BY round DESC,thread`, g.ID, q.user, q.user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []thread{}
	for rows.Next() {
		var t thread
		if err := rows.Scan(&t.Key, &t.Round, &t.Sender); err != nil {
			return nil, err
		}
		t.Side = "santa"
		if t.Sender == q.user {
			t.Side = "receiver"
		}
		t.ReadOnly = !g.Started || t.Round != g.Round
		result = append(result, t)
	}
	return result, rows.Err()
}

type message struct {
	ID       int64  `json:"id"`
	Text     string `json:"text"`
	Mine     bool   `json:"mine"`
	FileKey  string `json:"file_key,omitempty"`
	FileName string `json:"file_name,omitempty"`
	FileType string `json:"file_type,omitempty"`
	Created  string `json:"created"`
}

func (q *request) messages(where string, args ...any) (any, error) {
	after := int64(0)
	if raw := q.r.URL.Query().Get("after"); raw != "" {
		var err error
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			return nil, bad("Некорректный курсор")
		}
	}
	args = append([]any{q.user}, args...)
	args = append(args, after)
	rows, err := q.tx.QueryContext(q.r.Context(), `SELECT id,text,sender=?,file_key,file_name,file_type,created FROM messages WHERE `+where+` AND id>? ORDER BY id LIMIT 100`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []message{}
	for rows.Next() {
		var m message
		if err := rows.Scan(&m.ID, &m.Text, &m.Mine, &m.FileKey, &m.FileName, &m.FileType, &m.Created); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
func listMessages(q *request) (any, error) {
	t, err := q.thread()
	if err != nil {
		return nil, err
	}
	return q.messages(`thread=?`, t.Key)
}
func listAnnouncements(q *request) (any, error) {
	g, err := q.game()
	if err != nil {
		return nil, err
	}
	return q.messages(`game=? AND thread IS NULL`, g.ID)
}
func announce(q *request) (any, error) {
	g, err := q.game()
	if err != nil {
		return nil, err
	}
	if !g.IsOwner {
		return nil, forbidden()
	}
	var input struct {
		Text string `json:"text"`
	}
	if err := q.decode(&input); err != nil {
		return nil, err
	}
	text, err := checkedText(input.Text, 4000)
	if err != nil {
		return nil, err
	}
	if err := q.exec(`INSERT INTO messages(game,round,sender,text) VALUES(?,?,?,?)`, g.ID, g.Round, q.user, text); err != nil {
		return nil, err
	}
	return nil, q.notifyMembers(g.ID, "Новое объявление в игре «"+g.Name+"».")
}
func sendMessage(q *request) (any, error) {
	t, err := q.thread()
	if err != nil {
		return nil, err
	}
	if t.ReadOnly {
		return nil, apiError{409, "Этот диалог доступен только для чтения"}
	}
	if err := q.r.ParseMultipartForm(1 << 20); err != nil {
		return nil, bad("Не удалось загрузить сообщение. Максимальный размер файла — 20 МБ")
	}
	text := strings.TrimSpace(q.r.FormValue("text"))
	if len([]rune(text)) > 4000 {
		return nil, bad("Сообщение — до 4000 символов")
	}
	key, name, kind := "", "", ""
	count := 0
	for field, files := range q.r.MultipartForm.File {
		if field != "file" {
			return nil, bad("Неизвестное поле файла")
		}
		count += len(files)
	}
	if count > 1 {
		return nil, bad("Можно приложить один файл")
	}
	if count == 1 {
		file, header, err := q.r.FormFile("file")
		if err != nil {
			return nil, bad("Не удалось прочитать файл")
		}
		defer file.Close()
		if header.Size > 20<<20 {
			return nil, bad("Файл больше 20 МБ")
		}
		var sniff [512]byte
		n, err := file.Read(sniff[:])
		if err != nil && err != io.EOF {
			return nil, err
		}
		kind = http.DetectContentType(sniff[:n])
		if _, err := file.Seek(0, 0); err != nil {
			return nil, err
		}
		name = filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
		if len([]rune(name)) > 200 {
			return nil, bad("Слишком длинное имя файла")
		}
		// Documents are downloads only; never serve user-controlled HTML or SVG inline.
		if !strings.HasPrefix(kind, "image/") && !strings.HasPrefix(kind, "video/") && !strings.HasPrefix(kind, "audio/") {
			kind = "application/octet-stream"
		}
		if kind == "image/svg+xml" {
			kind = "application/octet-stream"
		}
		key = randomKey()
		q.files = append(q.files, key)
		dst, err := os.OpenFile(filepath.Join(q.s.uploads, key), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		size, copyErr := io.Copy(dst, io.LimitReader(file, (20<<20)+1))
		closeErr := dst.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if size > 20<<20 {
			return nil, bad("Файл больше 20 МБ")
		}
		if size == 0 {
			return nil, bad("Файл пуст")
		}
	}
	if text == "" && key == "" {
		return nil, bad("Введите сообщение или выберите файл")
	}
	receiver := t.Receiver
	if q.user == receiver {
		receiver = t.Sender
	}
	if err := q.exec(`INSERT INTO messages(thread,game,round,sender,receiver,text,file_key,file_name,file_type) VALUES(?,?,?,?,?,?,?,?,?)`, t.Key, t.Game, t.Round, q.user, receiver, text, key, name, kind); err != nil {
		return nil, err
	}
	q.message(receiver, "Новое сообщение в Secret Santa. Откройте приложение, чтобы прочитать.")
	return nil, nil
}
func getFile(q *request) (any, error) {
	key := q.r.PathValue("key")
	var file download
	var sender, receiver int64
	err := q.tx.QueryRowContext(q.r.Context(), `SELECT file_key,file_name,file_type,sender,receiver FROM messages WHERE file_key=? AND file_key<>''`, key).Scan(&file.key, &file.name, &file.mime, &sender, &receiver)
	if err != nil {
		return nil, err
	}
	if q.user != sender && q.user != receiver {
		return nil, forbidden()
	}
	if _, _, err := mime.ParseMediaType(file.mime); err != nil {
		file.mime = "application/octet-stream"
	}
	return file, nil
}
