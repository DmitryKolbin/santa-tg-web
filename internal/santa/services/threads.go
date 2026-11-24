package services

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/projections"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/repos"
)

type threadData struct {
	dto.Thread
	Game     int64
	Sender   int64
	Receiver int64
}

type ThreadService interface {
	List(context.Context, Actor, int64) ([]dto.Thread, error)
	Messages(context.Context, Actor, string, int64) ([]dto.Message, error)
	Send(context.Context, Actor, string, dto.SendMessage) error
	SaveTracking(context.Context, Actor, string, dto.SaveTracking) (dto.Tracking, error)
	SetReceived(context.Context, Actor, string, dto.SetReceived) (dto.Received, error)
}

type threadService struct {
	uow      repos.UnitOfWork
	uploads  string
	notifier Notifier
}

func NewThreadService(uow repos.UnitOfWork, uploads string, notifier Notifier) ThreadService {
	return &threadService{uow: uow, uploads: uploads, notifier: notifier}
}

func loadThread(ctx context.Context, messages repos.MessageRepo, actor int64, key string) (threadData, error) {
	view, err := messages.GetThread(ctx, key)
	t := threadData{Thread: dto.Thread{Key: view.Thread, GameName: view.GameName, Round: view.Round}, Game: view.GameID, Sender: view.SenderID, Receiver: view.ReceiverID}
	if err != nil {
		return t, err
	}
	if actor != t.Sender && actor != t.Receiver {
		return t, forbidden()
	}
	t.ReadOnly = !view.Started || view.CurrentRound != t.Round
	return t, nil
}

func (s *threadService) List(ctx context.Context, actor Actor, gameID int64) ([]dto.Thread, error) {
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) ([]dto.Thread, error) {
		g, err := loadGame(ctx, repositories.Games, actor.ID, gameID, "")
		if err != nil {
			return nil, err
		}
		views, err := repositories.Messages.ListThreads(ctx, g.ID, actor.ID)
		if err != nil {
			return nil, err
		}
		result := make([]dto.Thread, 0, len(views))
		for _, view := range views {
			t := dto.Thread{Key: view.Thread, GameName: g.Name, Round: view.Round, TrackingNumber: view.TrackingNumber, Received: view.Received, UnreadCount: view.UnreadCount, Side: "santa", ReadOnly: !g.Started || view.Round != g.Round}
			if view.SenderID == actor.ID {
				t.Side = "receiver"
				t.CounterpartName = view.ReceiverName
			}
			result = append(result, t)
		}
		return result, nil
	})
}

func messageFromDetails(details projections.MessageDetails) dto.Message {
	return dto.Message{ID: details.ID, Text: details.Text, Mine: details.Mine, FileKey: details.FileKey, FileName: details.FileName, FileType: details.FileType, Created: details.Created}
}

type stagedFile struct {
	key, name, mime string
}

func (s *threadService) stageFile(upload *dto.Upload) (stagedFile, error) {
	if upload == nil {
		return stagedFile{}, nil
	}
	if upload.Size > 20<<20 {
		return stagedFile{}, bad("Файл больше 20 МБ")
	}
	var sniff [512]byte
	n, err := upload.Reader.Read(sniff[:])
	if err != nil && err != io.EOF {
		return stagedFile{}, err
	}
	mimeType := http.DetectContentType(sniff[:n])
	if _, err := upload.Reader.Seek(0, 0); err != nil {
		return stagedFile{}, err
	}
	name := filepath.Base(strings.ReplaceAll(upload.Name, "\\", "/"))
	if len([]rune(name)) > 200 {
		return stagedFile{}, bad("Слишком длинное имя файла")
	}
	if !strings.HasPrefix(mimeType, "image/") && !strings.HasPrefix(mimeType, "video/") && !strings.HasPrefix(mimeType, "audio/") || mimeType == "image/svg+xml" {
		mimeType = "application/octet-stream"
	}
	file := stagedFile{key: randomKey(), name: name, mime: mimeType}
	dst, err := os.OpenFile(filepath.Join(s.uploads, file.key), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return stagedFile{}, err
	}
	size, copyErr := io.Copy(dst, io.LimitReader(upload.Reader, (20<<20)+1))
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(filepath.Join(s.uploads, file.key))
		return stagedFile{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(filepath.Join(s.uploads, file.key))
		return stagedFile{}, closeErr
	}
	if size > 20<<20 {
		_ = os.Remove(filepath.Join(s.uploads, file.key))
		return stagedFile{}, bad("Файл больше 20 МБ")
	}
	if size == 0 {
		_ = os.Remove(filepath.Join(s.uploads, file.key))
		return stagedFile{}, bad("Файл пуст")
	}
	return file, nil
}

func (s *threadService) Messages(ctx context.Context, actor Actor, key string, after int64) ([]dto.Message, error) {
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) ([]dto.Message, error) {
		t, err := loadThread(ctx, repositories.Messages, actor.ID, key)
		if err != nil {
			return nil, err
		}
		views, err := repositories.Messages.List(ctx, actor.ID, t.Game, &t.Key, after)
		if err != nil {
			return nil, err
		}
		result := make([]dto.Message, 0, len(views))
		for _, details := range views {
			result = append(result, messageFromDetails(details))
		}
		if len(result) > 0 {
			if err := repositories.Messages.MarkRead(ctx, actor.ID, t.Game, t.Key, result[len(result)-1].ID); err != nil {
				return nil, err
			}
		}
		return result, nil
	})
}

func (s *threadService) Send(ctx context.Context, actor Actor, key string, input dto.SendMessage) error {
	expected, err := execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) (threadData, error) {
		t, err := loadThread(ctx, repositories.Messages, actor.ID, key)
		if err == nil && t.ReadOnly {
			err = Error{Status: 409, Text: "Этот диалог доступен только для чтения"}
		}
		return t, err
	})
	if err != nil {
		return err
	}
	text := strings.TrimSpace(input.Text)
	if len([]rune(text)) > 4000 {
		return bad("Сообщение — до 4000 символов")
	}
	file, err := s.stageFile(input.File)
	if err != nil {
		return err
	}
	if text == "" && file.key == "" {
		return bad("Введите сообщение или выберите файл")
	}
	committed := false
	if file.key != "" {
		defer func() {
			if !committed {
				_ = os.Remove(filepath.Join(s.uploads, file.key))
			}
		}()
	}
	_, err = execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, e *effects) (struct{}, error) {
		t, err := loadThread(ctx, repositories.Messages, actor.ID, key)
		if err != nil {
			return struct{}{}, err
		}
		if t.ReadOnly || t != expected {
			return struct{}{}, Error{Status: 409, Text: "Состояние диалога изменилось. Повторите отправку."}
		}
		receiver := t.Receiver
		if actor.ID == receiver {
			receiver = t.Sender
		}
		if err := repositories.Messages.Create(ctx, &santa.Message{Thread: &t.Key, GameID: t.Game, Round: t.Round, SenderID: actor.ID, ReceiverID: &receiver, Text: text, FileKey: file.key, FileName: file.name, FileType: file.mime}); err != nil {
			return struct{}{}, err
		}
		e.notify(receiver, "Новое сообщение в Secret Santa. Откройте приложение, чтобы прочитать.")
		return struct{}{}, nil
	})
	committed = err == nil
	return err
}

func (s *threadService) SaveTracking(ctx context.Context, actor Actor, key string, input dto.SaveTracking) (dto.Tracking, error) {
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, e *effects) (dto.Tracking, error) {
		t, err := loadThread(ctx, repositories.Messages, actor.ID, key)
		if err != nil {
			return dto.Tracking{}, err
		}
		if actor.ID != t.Sender {
			return dto.Tracking{}, forbidden()
		}
		tracking, err := checkedText(input.TrackingNumber, 200)
		if err != nil {
			return dto.Tracking{}, err
		}
		delivery, err := repositories.Messages.GetDelivery(ctx, t.Key)
		if err != nil && err != sql.ErrNoRows {
			return dto.Tracking{}, err
		}
		if delivery.Received {
			return dto.Tracking{}, Error{Status: 409, Text: "Получатель уже подтвердил получение подарка"}
		}
		result := dto.Tracking{TrackingNumber: tracking, Received: false}
		if delivery.TrackingNumber == tracking {
			return result, nil
		}
		if err := repositories.Messages.SaveDelivery(ctx, &santa.Delivery{Thread: t.Key, TrackingNumber: tracking}, "tracking_number"); err != nil {
			return dto.Tracking{}, err
		}
		verb := "добавил"
		if delivery.TrackingNumber != "" {
			verb = "изменил"
		}
		e.notify(t.Receiver, fmt.Sprintf("Ваш Тайный Санта %s трек-номер подарка в игре «%s»: %s", verb, t.GameName, tracking))
		return result, nil
	})
}

func (s *threadService) SetReceived(ctx context.Context, actor Actor, key string, input dto.SetReceived) (dto.Received, error) {
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, e *effects) (dto.Received, error) {
		t, err := loadThread(ctx, repositories.Messages, actor.ID, key)
		if err != nil {
			return dto.Received{}, err
		}
		if actor.ID != t.Receiver {
			return dto.Received{}, forbidden()
		}
		delivery, err := repositories.Messages.GetDelivery(ctx, t.Key)
		if err != nil && err != sql.ErrNoRows {
			return dto.Received{}, err
		}
		if delivery.Received == input.Received {
			return dto.Received{Received: input.Received}, nil
		}
		if err := repositories.Messages.SaveDelivery(ctx, &santa.Delivery{Thread: t.Key, Received: input.Received}, "received"); err != nil {
			return dto.Received{}, err
		}
		_, receiver, err := readProfile(ctx, repositories.Users, actor.ID)
		if err != nil {
			return dto.Received{}, err
		}
		text := receiver.Name + " подтвердил(а) получение подарка в игре «" + t.GameName + "»."
		if !input.Received {
			text = receiver.Name + " отменил(а) подтверждение получения подарка в игре «" + t.GameName + "»."
		}
		e.notify(t.Sender, text)
		return dto.Received{Received: input.Received}, nil
	})
}
