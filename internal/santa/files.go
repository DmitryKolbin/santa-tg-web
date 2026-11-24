package santa

import (
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Upload struct {
	Reader io.ReadSeeker
	Name   string
	Size   int64
}

type SendMessage struct {
	Text string
	File *Upload
}

// Download is an attachment the actor may read.
type Download struct {
	Name string
	MIME string
	Path string
}

type stagedFile struct {
	key, name, mime string
}

func (s *Service) File(ctx context.Context, actor Actor, key string) (Download, error) {
	f, err := s.store.File(ctx, key)
	if err != nil {
		return Download{}, err
	}
	if actor.ID != f.Sender && actor.ID != f.Receiver {
		return Download{}, forbidden()
	}
	file := Download{Name: f.Name, MIME: f.Type, Path: filepath.Join(s.uploads, f.Key)}
	if _, _, err := mime.ParseMediaType(file.MIME); err != nil {
		file.MIME = "application/octet-stream"
	}
	return file, nil
}

// stageMessage validates a message and writes its attachment to disk before
// the transaction; the caller removes the file if the message is not saved.
func (s *Service) stageMessage(input SendMessage) (string, stagedFile, error) {
	text := strings.TrimSpace(input.Text)
	if len([]rune(text)) > MaxText {
		return "", stagedFile{}, invalid("Сообщение — до 4000 символов")
	}
	file, err := s.stageFile(input.File)
	if err != nil {
		return "", stagedFile{}, err
	}
	if text == "" && file.key == "" {
		return "", stagedFile{}, invalid("Введите сообщение или выберите файл")
	}
	return text, file, nil
}

func (s *Service) stageFile(upload *Upload) (stagedFile, error) {
	if upload == nil {
		return stagedFile{}, nil
	}
	if upload.Size > MaxUploadBytes {
		return stagedFile{}, invalid("Файл больше 20 МБ")
	}
	var sniff [512]byte
	n, err := upload.Reader.Read(sniff[:])
	if err != nil && err != io.EOF {
		return stagedFile{}, err
	}
	if _, err := upload.Reader.Seek(0, io.SeekStart); err != nil {
		return stagedFile{}, err
	}
	name := filepath.Base(strings.ReplaceAll(upload.Name, "\\", "/"))
	if len([]rune(name)) > MaxFileName {
		return stagedFile{}, invalid("Слишком длинное имя файла")
	}
	file := stagedFile{key: randomKey(), name: name, mime: safeMIME(http.DetectContentType(sniff[:n]))}
	size, err := s.writeFile(file.key, upload.Reader)
	if err == nil && size > MaxUploadBytes {
		err = invalid("Файл больше 20 МБ")
	}
	if err == nil && size == 0 {
		err = invalid("Файл пуст")
	}
	if err != nil {
		s.removeFile(file)
		return stagedFile{}, err
	}
	return file, nil
}

// safeMIME keeps media types the client may preview and downgrades the rest,
// including SVG, to a plain download.
func safeMIME(detected string) string {
	media := strings.HasPrefix(detected, "image/") || strings.HasPrefix(detected, "video/") || strings.HasPrefix(detected, "audio/")
	if !media || detected == "image/svg+xml" {
		return "application/octet-stream"
	}
	return detected
}

func (s *Service) writeFile(key string, r io.Reader) (int64, error) {
	dst, err := os.OpenFile(filepath.Join(s.uploads, key), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	size, err := io.Copy(dst, io.LimitReader(r, MaxUploadBytes+1))
	if closeErr := dst.Close(); err == nil {
		err = closeErr
	}
	return size, err
}

func (s *Service) removeFile(file stagedFile) {
	if file.key != "" {
		_ = os.Remove(filepath.Join(s.uploads, file.key))
	}
}
