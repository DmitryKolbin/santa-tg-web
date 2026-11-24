package santa

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/DmitryKolbin/santa-tg-web/internal/store"
)

// Limits shared by validation and the API.
const (
	MaxMembers     = 50
	MaxGameName    = 120
	MaxText        = 4000
	MaxAddress     = 2000
	MaxTracking    = 200
	MaxFileName    = 200
	MaxUploadBytes = 20 << 20
)

type Actor struct {
	ID   int64
	Name string
}

type Notifier interface {
	Notify(user int64, text string)
}

type ErrorKind int

const (
	Invalid ErrorKind = iota + 1
	Forbidden
	Conflict
	Unprocessable
)

// Error is a business rule violation with a message for the user.
type Error struct {
	Kind ErrorKind
	Text string
}

func (e Error) Error() string { return e.Text }

func invalid(text string) error  { return Error{Kind: Invalid, Text: text} }
func conflict(text string) error { return Error{Kind: Conflict, Text: text} }
func forbidden() error           { return Error{Kind: Forbidden, Text: "Нет доступа"} }

type Options struct {
	Uploads   string
	BotName   string
	Locations map[string]string
	Notifier  Notifier
}

type Service struct {
	store     *store.Store
	uploads   string
	botName   string
	locations map[string]string
	notifier  Notifier
}

func New(st *store.Store, opts Options) *Service {
	return &Service{store: st, uploads: opts.Uploads, botName: opts.BotName, locations: opts.Locations, notifier: opts.Notifier}
}

func (s *Service) Ping(ctx context.Context) error { return s.store.DB().PingContext(ctx) }

// EnsureUser creates the user on first contact and keeps the Telegram name current.
func (s *Service) EnsureUser(ctx context.Context, actor Actor) error {
	return s.store.EnsureUser(ctx, actor.ID, actor.Name)
}

func (s *Service) Locations() map[string]string { return s.locations }

type notification struct {
	user int64
	text string
}

type notes []notification

func (n *notes) add(user int64, text string) { *n = append(*n, notification{user: user, text: text}) }

// write runs fn in a transaction and sends the collected notifications only
// after it commits.
func (s *Service) write(ctx context.Context, fn func(tx *store.Store, n *notes) error) error {
	var n notes
	if err := s.store.Tx(ctx, func(tx *store.Store) error { return fn(tx, &n) }); err != nil {
		return err
	}
	if s.notifier != nil {
		for _, note := range n {
			s.notifier.Notify(note.user, note.text)
		}
	}
	return nil
}

func notifyMembers(ctx context.Context, tx *store.Store, n *notes, game int64, text string) error {
	ids, err := tx.MemberIDs(ctx, game)
	if err != nil {
		return err
	}
	for _, id := range ids {
		n.add(id, text)
	}
	return nil
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
		return "", invalid(fmt.Sprintf("Введите текст от 1 до %d символов", max))
	}
	return text, nil
}
