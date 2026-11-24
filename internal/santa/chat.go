package santa

import (
	"context"

	"github.com/DmitryKolbin/santa-tg-web/internal/store"
)

// Thread is the anonymous conversation of one assignment, seen by one side.
// Side "receiver" means the actor is the santa writing to their receiver.
type Thread struct {
	Key             string `json:"key"`
	GameName        string `json:"game_name,omitempty"`
	Round           int    `json:"round"`
	Side            string `json:"side"`
	CounterpartName string `json:"counterpart_name,omitempty"`
	TrackingNumber  string `json:"tracking_number"`
	Received        bool   `json:"received"`
	UnreadCount     int    `json:"unread_count"`
	ReadOnly        bool   `json:"read_only"`
}

type Message struct {
	ID       int64  `json:"id"`
	Text     string `json:"text"`
	Mine     bool   `json:"mine"`
	FileKey  string `json:"file_key,omitempty"`
	FileName string `json:"file_name,omitempty"`
	FileType string `json:"file_type,omitempty"`
	Created  string `json:"created"`
}

type AnnouncementSummary struct {
	Latest      *Message `json:"latest,omitempty"`
	UnreadCount int      `json:"unread_count"`
}

type Announcement struct {
	Text string `json:"text"`
}

func toMessage(v store.MessageView) Message {
	return Message{ID: v.ID, Text: v.Text, Mine: v.Mine, FileKey: v.FileKey, FileName: v.FileName, FileType: v.FileType, Created: v.Created}
}

// readOnly reports whether a thread of the given round no longer accepts
// changes: the draw was reset or replaced by a newer round.
func readOnly(started bool, currentRound, round int) bool {
	return !started || currentRound != round
}

// threadFor loads a thread the actor takes part in.
func threadFor(ctx context.Context, db *store.Store, actor int64, key string) (store.ThreadView, error) {
	t, err := db.Thread(ctx, key)
	if err == nil && actor != t.SenderID && actor != t.ReceiverID {
		return t, forbidden()
	}
	return t, err
}

func (s *Service) Threads(ctx context.Context, actor Actor, gameID int64) ([]Thread, error) {
	g, err := gameFor(ctx, s.store, actor.ID, gameID)
	if err != nil {
		return nil, err
	}
	views, err := s.store.Threads(ctx, g.ID, actor.ID)
	if err != nil {
		return nil, err
	}
	result := make([]Thread, 0, len(views))
	for _, v := range views {
		t := Thread{Key: v.Thread, GameName: g.Name, Round: v.Round, TrackingNumber: v.TrackingNumber, Received: v.Received, UnreadCount: v.UnreadCount, Side: "santa", ReadOnly: readOnly(g.Started, g.Round, v.Round)}
		if v.SenderID == actor.ID {
			t.Side = "receiver"
			t.CounterpartName = v.ReceiverName
		}
		result = append(result, t)
	}
	return result, nil
}

// readMessages lists messages after the cursor and marks them read. An empty
// thread key means game announcements.
func (s *Service) readMessages(ctx context.Context, actor, game int64, thread string, after int64) ([]Message, error) {
	var result []Message
	err := s.write(ctx, func(tx *store.Store, _ *notes) error {
		views, err := tx.Messages(ctx, actor, game, thread, after)
		if err != nil {
			return err
		}
		result = make([]Message, 0, len(views))
		for _, v := range views {
			result = append(result, toMessage(v))
		}
		if len(result) == 0 {
			return nil
		}
		return tx.MarkRead(ctx, actor, game, thread, result[len(result)-1].ID)
	})
	return result, err
}

func (s *Service) Messages(ctx context.Context, actor Actor, key string, after int64) ([]Message, error) {
	t, err := threadFor(ctx, s.store, actor.ID, key)
	if err != nil {
		return nil, err
	}
	return s.readMessages(ctx, actor.ID, t.GameID, t.Thread, after)
}

func (s *Service) Send(ctx context.Context, actor Actor, key string, input SendMessage) error {
	expected, err := threadFor(ctx, s.store, actor.ID, key)
	if err != nil {
		return err
	}
	if readOnly(expected.Started, expected.CurrentRound, expected.Round) {
		return conflict("Этот диалог доступен только для чтения")
	}
	text, file, err := s.stageMessage(input)
	if err != nil {
		return err
	}
	err = s.write(ctx, func(tx *store.Store, n *notes) error {
		t, err := threadFor(ctx, tx, actor.ID, key)
		if err != nil {
			return err
		}
		if t != expected {
			return conflict("Состояние диалога изменилось. Повторите отправку.")
		}
		receiver := t.ReceiverID
		if actor.ID == receiver {
			receiver = t.SenderID
		}
		if err := tx.CreateMessage(ctx, &store.Message{Thread: &t.Thread, GameID: t.GameID, Round: t.Round, SenderID: actor.ID, ReceiverID: &receiver, Text: text, FileKey: file.key, FileName: file.name, FileType: file.mime}); err != nil {
			return err
		}
		n.add(receiver, "Новое сообщение в Secret Santa. Откройте приложение, чтобы прочитать.")
		return nil
	})
	if err != nil {
		s.removeFile(file)
	}
	return err
}

func (s *Service) Announcements(ctx context.Context, actor Actor, gameID, after int64) ([]Message, error) {
	g, err := gameFor(ctx, s.store, actor.ID, gameID)
	if err != nil {
		return nil, err
	}
	return s.readMessages(ctx, actor.ID, g.ID, "", after)
}

func (s *Service) LatestAnnouncement(ctx context.Context, actor Actor, gameID int64) (AnnouncementSummary, error) {
	g, err := gameFor(ctx, s.store, actor.ID, gameID)
	if err != nil {
		return AnnouncementSummary{}, err
	}
	var result AnnouncementSummary
	view, err := s.store.LatestAnnouncement(ctx, actor.ID, g.ID)
	switch {
	case err == nil:
		latest := toMessage(view)
		result.Latest = &latest
	case !isNoRows(err):
		return result, err
	}
	result.UnreadCount, err = s.store.UnreadAnnouncements(ctx, actor.ID, g.ID)
	return result, err
}

func (s *Service) Announce(ctx context.Context, actor Actor, gameID int64, input Announcement) error {
	return s.write(ctx, func(tx *store.Store, n *notes) error {
		g, err := ownedGame(ctx, tx, actor.ID, gameID)
		if err != nil {
			return err
		}
		text, err := checkedText(input.Text, MaxText)
		if err != nil {
			return err
		}
		if err := tx.CreateMessage(ctx, &store.Message{GameID: g.ID, Round: g.Round, SenderID: actor.ID, Text: text}); err != nil {
			return err
		}
		return notifyMembers(ctx, tx, n, g.ID, "Новое объявление в игре «"+g.Name+"».")
	})
}
