package api

import (
	"net/http"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
)

func (s *Server) profile(r *http.Request, actor santa.Actor) (any, error) {
	return s.svc.Profile(r.Context(), actor)
}

func (s *Server) saveProfile(r *http.Request, actor santa.Actor) (any, error) {
	var input santa.SaveProfile
	if err := decodeJSON(r, &input); err != nil {
		return nil, err
	}
	return s.svc.SaveProfile(r.Context(), actor, input)
}

func (s *Server) locations(r *http.Request, _ santa.Actor) (any, error) {
	return s.svc.Locations(), nil
}

func (s *Server) games(r *http.Request, actor santa.Actor) (any, error) {
	return s.svc.Games(r.Context(), actor)
}

func (s *Server) createGame(r *http.Request, actor santa.Actor) (any, error) {
	input := santa.CreateGame{SingleCycle: true, AllowMutualPairs: true}
	if err := decodeJSON(r, &input); err != nil {
		return nil, err
	}
	return s.svc.CreateGame(r.Context(), actor, input)
}

func (s *Server) game(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return s.svc.Game(r.Context(), actor, id)
}

func (s *Server) updateGame(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	var input santa.UpdateGame
	if err := decodeJSON(r, &input); err != nil {
		return nil, err
	}
	return s.svc.UpdateGame(r.Context(), actor, id, input)
}

func drawInput(r *http.Request) (int64, santa.DrawGame, error) {
	id, err := gameID(r)
	if err != nil {
		return 0, santa.DrawGame{}, err
	}
	var input santa.DrawGame
	err = decodeJSON(r, &input)
	return id, input, err
}

func (s *Server) readiness(r *http.Request, actor santa.Actor) (any, error) {
	id, input, err := drawInput(r)
	if err != nil {
		return nil, err
	}
	return s.svc.CheckReadiness(r.Context(), actor, id, input)
}

func (s *Server) draw(r *http.Request, actor santa.Actor) (any, error) {
	id, input, err := drawInput(r)
	if err != nil {
		return nil, err
	}
	return s.svc.Draw(r.Context(), actor, id, input)
}

func (s *Server) resetGame(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return nil, s.svc.ResetGame(r.Context(), actor, id)
}

func (s *Server) receiver(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return s.svc.Receiver(r.Context(), actor, id)
}

func (s *Server) latestAnnouncement(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return s.svc.LatestAnnouncement(r.Context(), actor, id)
}

func (s *Server) announcements(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	after, err := cursor(r)
	if err != nil {
		return nil, err
	}
	return s.svc.Announcements(r.Context(), actor, id, after)
}

func (s *Server) announce(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	var input santa.Announcement
	if err := decodeJSON(r, &input); err != nil {
		return nil, err
	}
	return nil, s.svc.Announce(r.Context(), actor, id, input)
}

func (s *Server) threads(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return s.svc.Threads(r.Context(), actor, id)
}

func (s *Server) invite(r *http.Request, actor santa.Actor) (any, error) {
	return s.svc.Invite(r.Context(), actor, r.PathValue("token"))
}

func (s *Server) join(r *http.Request, actor santa.Actor) (any, error) {
	return s.svc.Join(r.Context(), actor, r.PathValue("token"))
}

func (s *Server) messages(r *http.Request, actor santa.Actor) (any, error) {
	after, err := cursor(r)
	if err != nil {
		return nil, err
	}
	return s.svc.Messages(r.Context(), actor, r.PathValue("thread"), after)
}

// send accepts an empty body, or a multipart form with "text" and at most one "file".
func (s *Server) send(r *http.Request, actor santa.Actor) (any, error) {
	thread := r.PathValue("thread")
	if r.Body == nil || r.ContentLength == 0 {
		return nil, s.svc.Send(r.Context(), actor, thread, santa.SendMessage{})
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		return nil, badRequest("Не удалось загрузить сообщение. Максимальный размер файла — 20 МБ")
	}
	count := 0
	for field, files := range r.MultipartForm.File {
		if field != "file" {
			return nil, badRequest("Неизвестное поле файла")
		}
		count += len(files)
	}
	if count > 1 {
		return nil, badRequest("Можно приложить один файл")
	}
	input := santa.SendMessage{Text: r.FormValue("text")}
	if count == 1 {
		file, header, err := r.FormFile("file")
		if err != nil {
			return nil, badRequest("Не удалось прочитать файл")
		}
		defer file.Close()
		input.File = &santa.Upload{Reader: file, Name: header.Filename, Size: header.Size}
	}
	return nil, s.svc.Send(r.Context(), actor, thread, input)
}

func (s *Server) saveTracking(r *http.Request, actor santa.Actor) (any, error) {
	var input santa.SaveTracking
	if err := decodeJSON(r, &input); err != nil {
		return nil, err
	}
	return s.svc.SaveTracking(r.Context(), actor, r.PathValue("thread"), input)
}

func (s *Server) setReceived(r *http.Request, actor santa.Actor) (any, error) {
	var input santa.SetReceived
	if err := decodeJSON(r, &input); err != nil {
		return nil, err
	}
	return s.svc.SetReceived(r.Context(), actor, r.PathValue("thread"), input)
}

func (s *Server) members(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return s.svc.Members(r.Context(), actor, id)
}

func (s *Server) removeMember(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	user, err := userID(r, "user")
	if err != nil {
		return nil, err
	}
	return nil, s.svc.RemoveMember(r.Context(), actor, id, user)
}

func (s *Server) block(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	var input santa.BlockedPair
	if err := decodeJSON(r, &input); err != nil {
		return nil, err
	}
	if input.Sender < 1 || input.Receiver < 1 {
		return nil, badRequest("Некорректный участник")
	}
	return nil, s.svc.Block(r.Context(), actor, id, input)
}

func (s *Server) unblock(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	sender, err := userID(r, "sender")
	if err != nil {
		return nil, err
	}
	receiver, err := userID(r, "receiver")
	if err != nil {
		return nil, err
	}
	return nil, s.svc.Unblock(r.Context(), actor, id, santa.BlockedPair{Sender: sender, Receiver: receiver})
}

func (s *Server) finishGame(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return nil, s.svc.FinishGame(r.Context(), actor, id)
}

func (s *Server) reopenGame(r *http.Request, actor santa.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return nil, s.svc.ReopenGame(r.Context(), actor, id)
}
