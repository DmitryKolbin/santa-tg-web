package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/repos"
)

type Actor struct {
	ID   int64
	Name string
}

type Notifier interface {
	Notify(int64, string)
}

type Error struct {
	Status int
	Text   string
}

func (e Error) Error() string { return e.Text }
func bad(text string) error   { return Error{Status: http.StatusBadRequest, Text: text} }
func forbidden() error        { return Error{Status: http.StatusForbidden, Text: "Нет доступа"} }

type notification struct {
	user int64
	text string
}

type effects struct {
	notifications []notification
	files         []string
}

func (e *effects) notify(user int64, text string) {
	e.notifications = append(e.notifications, notification{user: user, text: text})
}

func execute[T any](ctx context.Context, uow repos.UnitOfWork, actor Actor, notifier Notifier, uploads string, operation func(repos.Repositories, *effects) (T, error)) (result T, err error) {
	e := &effects{}
	err = uow.InTx(ctx, func(repositories repos.Repositories) error {
		if err := repositories.Users.Ensure(ctx, actor.ID, actor.Name); err != nil {
			return err
		}
		result, err = operation(repositories, e)
		return err
	})
	if err != nil {
		for _, key := range e.files {
			_ = os.Remove(filepath.Join(uploads, key))
		}
		return result, err
	}
	if notifier != nil {
		for _, note := range e.notifications {
			notifier.Notify(note.user, note.text)
		}
	}
	return result, nil
}

func randomKey() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func checkedText(text string, max int) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > max {
		return "", bad(fmt.Sprintf("Введите текст от 1 до %d символов", max))
	}
	return text, nil
}
