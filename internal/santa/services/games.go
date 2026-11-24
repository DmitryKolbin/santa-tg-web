package services

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"reflect"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/projections"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/repos"
)

type gameData struct {
	dto.Game
	Creator int64
	Invite  string
}

type drawSnapshot struct {
	game    gameData
	players []Player
}

type gameReader interface {
	Get(context.Context, int64, int64) (projections.GameDetails, error)
}

func gameFromDetails(details projections.GameDetails, actor int64, botName string) gameData {
	g := gameData{Game: dto.Game{ID: details.ID, Name: details.Name, CreatorName: details.CreatorName, SingleCycle: details.SingleCycle, AllowMutualPairs: details.AllowMutualPairs, Started: details.Started, Round: details.Round, IsMember: details.IsMember, Count: details.MemberCount}, Creator: details.CreatorID, Invite: details.Invite}
	g.IsOwner = g.Creator == actor
	if g.IsOwner && botName != "" {
		g.InviteURL = "https://t.me/" + botName + "?startapp=" + url.QueryEscape(g.Invite)
	}
	return g
}

func loadGame(ctx context.Context, repo gameReader, actor, id int64, botName string) (gameData, error) {
	details, err := repo.Get(ctx, actor, id)
	g := gameFromDetails(details, actor, botName)
	if err == nil && !g.IsOwner && !g.IsMember {
		return g, forbidden()
	}
	return g, err
}

type GameService interface {
	List(context.Context, Actor) ([]dto.Game, error)
	Create(context.Context, Actor, dto.CreateGame) (dto.Game, error)
	Get(context.Context, Actor, int64) (dto.Game, error)
	Update(context.Context, Actor, int64, dto.UpdateGame) (dto.Game, error)
	CheckReadiness(context.Context, Actor, int64, dto.DrawGame) (dto.Readiness, error)
	Draw(context.Context, Actor, int64, dto.DrawGame) (dto.Readiness, error)
	Reset(context.Context, Actor, int64) error
	Receiver(context.Context, Actor, int64) (dto.Receiver, error)
	LatestAnnouncement(context.Context, Actor, int64) (dto.AnnouncementSummary, error)
	Announcements(context.Context, Actor, int64, int64) ([]dto.Message, error)
	Announce(context.Context, Actor, int64, dto.Announcement) error
}

type gameService struct {
	uow       repos.UnitOfWork
	botName   string
	locations map[string]string
	notifier  Notifier
}

func NewGameService(uow repos.UnitOfWork, botName string, locations map[string]string, notifier Notifier) GameService {
	return &gameService{uow: uow, botName: botName, locations: locations, notifier: notifier}
}

func (s *gameService) List(ctx context.Context, actor Actor) ([]dto.Game, error) {
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) ([]dto.Game, error) {
		games, err := repositories.Games.List(ctx, actor.ID)
		if err != nil {
			return nil, err
		}
		result := make([]dto.Game, 0, len(games))
		for _, game := range games {
			result = append(result, gameFromDetails(game, actor.ID, s.botName).Game)
		}
		return result, nil
	})
}

func (s *gameService) Get(ctx context.Context, actor Actor, id int64) (dto.Game, error) {
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) (dto.Game, error) {
		g, err := loadGame(ctx, repositories.Games, actor.ID, id, s.botName)
		return g.Game, err
	})
}

func (s *gameService) Create(ctx context.Context, actor Actor, input dto.CreateGame) (dto.Game, error) {
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) (dto.Game, error) {
		name, err := checkedText(input.Name, 120)
		if err != nil {
			return dto.Game{}, err
		}
		model := &santa.Game{Name: name, CreatorID: actor.ID, Invite: randomKey(), SingleCycle: input.SingleCycle, AllowMutualPairs: input.AllowMutualPairs, Round: 1}
		if err := repositories.Games.Create(ctx, model); err != nil {
			return dto.Game{}, err
		}
		g, err := loadGame(ctx, repositories.Games, actor.ID, model.ID, s.botName)
		return g.Game, err
	})
}

func (s *gameService) Update(ctx context.Context, actor Actor, id int64, input dto.UpdateGame) (dto.Game, error) {
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) (dto.Game, error) {
		g, err := loadGame(ctx, repositories.Games, actor.ID, id, s.botName)
		if err != nil {
			return dto.Game{}, err
		}
		if !g.IsOwner {
			return dto.Game{}, forbidden()
		}
		if g.Started {
			return dto.Game{}, Error{Status: 409, Text: "Сначала сбросьте жеребьёвку"}
		}
		name := g.Name
		if input.Name != nil {
			name = *input.Name
		}
		name, err = checkedText(name, 120)
		if err != nil {
			return dto.Game{}, err
		}
		singleCycle, allowMutual := g.SingleCycle, g.AllowMutualPairs
		if input.SingleCycle != nil {
			singleCycle = *input.SingleCycle
		}
		if input.AllowMutualPairs != nil {
			allowMutual = *input.AllowMutualPairs
		}
		if err := repositories.Games.Update(ctx, &santa.Game{ID: g.ID, Name: name, SingleCycle: singleCycle, AllowMutualPairs: allowMutual}, "name", "single_cycle", "allow_mutual_pairs"); err != nil {
			return dto.Game{}, err
		}
		updated, err := loadGame(ctx, repositories.Games, actor.ID, id, s.botName)
		return updated.Game, err
	})
}

func (s *gameService) loadDrawSnapshot(ctx context.Context, repositories repos.Repositories, actor Actor, id int64) (drawSnapshot, error) {
	g, err := s.drawInput(ctx, repositories.Games, actor, id)
	if err != nil {
		return drawSnapshot{}, err
	}
	ids, err := repositories.Games.MemberIDs(ctx, g.ID)
	if err != nil {
		return drawSnapshot{}, err
	}
	members := make([]Player, 0, len(ids))
	for _, id := range ids {
		user, _, err := readProfile(ctx, repositories.Users, id)
		if err != nil {
			return drawSnapshot{}, err
		}
		members = append(members, Player{ID: user.ID, Name: user.Name, Address: user.Address, Wishlist: user.Wishlist, Location: user.Location, Destinations: []string(user.Destinations)})
	}
	return drawSnapshot{game: g, players: members}, nil
}

func (s *gameService) prepare(ctx context.Context, snapshot drawSnapshot, force bool) (dto.Readiness, []int, error) {
	report, _, assignments, err := PrepareAssignments(ctx, DrawSettings{SingleCycle: snapshot.game.SingleCycle, AllowMutualPairs: snapshot.game.AllowMutualPairs}, snapshot.players, force, s.locations)
	return report, assignments, err
}

func (s *gameService) drawInput(ctx context.Context, games repos.GameRepo, actor Actor, id int64) (gameData, error) {
	g, err := loadGame(ctx, games, actor.ID, id, s.botName)
	if err != nil {
		return g, err
	}
	if !g.IsOwner {
		return g, forbidden()
	}
	if g.Started {
		return g, Error{Status: 409, Text: "Жеребьёвка уже прошла"}
	}
	return g, nil
}

func (s *gameService) CheckReadiness(ctx context.Context, actor Actor, id int64, input dto.DrawGame) (dto.Readiness, error) {
	snapshot, err := execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) (drawSnapshot, error) {
		return s.loadDrawSnapshot(ctx, repositories, actor, id)
	})
	if err != nil {
		return dto.Readiness{}, err
	}
	result, _, err := s.prepare(ctx, snapshot, input.Force)
	return result, err
}

func notifyMembers(ctx context.Context, games repos.GameRepo, e *effects, game int64, text string) error {
	ids, err := games.MemberIDs(ctx, game)
	if err != nil {
		return err
	}
	for _, id := range ids {
		e.notify(id, text)
	}
	return nil
}

func (s *gameService) Draw(ctx context.Context, actor Actor, id int64, input dto.DrawGame) (dto.Readiness, error) {
	snapshot, err := execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) (drawSnapshot, error) {
		return s.loadDrawSnapshot(ctx, repositories, actor, id)
	})
	if err != nil {
		return dto.Readiness{}, err
	}
	report, assignments, err := s.prepare(ctx, snapshot, input.Force)
	if err != nil {
		return dto.Readiness{}, err
	}
	if !report.Ready {
		return dto.Readiness{}, Error{Status: 422, Text: report.Error}
	}
	if input.Force && input.Exclusions != report.Exclusions {
		return dto.Readiness{}, Error{Status: 409, Text: "Список исключений изменился. Проверьте готовность и подтвердите запуск ещё раз."}
	}
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, e *effects) (dto.Readiness, error) {
		current, err := s.loadDrawSnapshot(ctx, repositories, actor, id)
		if err != nil {
			return dto.Readiness{}, err
		}
		if !reflect.DeepEqual(current, snapshot) {
			return dto.Readiness{}, Error{Status: 409, Text: "Состав игры или анкеты изменились. Проверьте готовность и запустите жеребьёвку ещё раз."}
		}
		g := current.game
		for from, to := range assignments {
			if err := repositories.Games.CreateAssignment(ctx, &santa.Assignment{Thread: randomKey(), GameID: g.ID, Round: g.Round, SenderID: current.players[from].ID, ReceiverID: current.players[to].ID}); err != nil {
				return dto.Readiness{}, err
			}
		}
		if err := repositories.Games.Update(ctx, &santa.Game{ID: g.ID, Started: true}, "started"); err != nil {
			return dto.Readiness{}, err
		}
		err = notifyMembers(ctx, repositories.Games, e, g.ID, "Жеребьёвка в игре «"+g.Name+"» завершена. Откройте приложение, чтобы посмотреть результат.")
		return report, err
	})
}

func (s *gameService) Reset(ctx context.Context, actor Actor, id int64) error {
	_, err := execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, e *effects) (struct{}, error) {
		g, err := loadGame(ctx, repositories.Games, actor.ID, id, s.botName)
		if err != nil {
			return struct{}{}, err
		}
		if !g.IsOwner {
			return struct{}{}, forbidden()
		}
		if !g.Started {
			return struct{}{}, Error{Status: 409, Text: "Игра ещё не разыграна"}
		}
		if err := repositories.Games.Update(ctx, &santa.Game{ID: g.ID, Started: false, Round: g.Round + 1}, "started", "round"); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, notifyMembers(ctx, repositories.Games, e, g.ID, fmt.Sprintf("Жеребьёвка в игре «%s» сброшена. Прежние назначения больше не действуют.", g.Name))
	})
	return err
}

func (s *gameService) Receiver(ctx context.Context, actor Actor, id int64) (dto.Receiver, error) {
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) (dto.Receiver, error) {
		g, err := loadGame(ctx, repositories.Games, actor.ID, id, s.botName)
		if err != nil {
			return dto.Receiver{}, err
		}
		if !g.Started {
			return dto.Receiver{}, Error{Status: 409, Text: "Жеребьёвка ещё не прошла"}
		}
		userID, err := repositories.Games.Receiver(ctx, g.ID, g.Round, actor.ID)
		if err != nil {
			return dto.Receiver{}, err
		}
		_, profile, err := readProfile(ctx, repositories.Users, userID)
		return dto.Receiver{Name: profile.Name, Address: profile.Address, Wishlist: profile.Wishlist}, err
	})
}

func messages(ctx context.Context, repository repos.MessageRepo, actor, game int64, thread *string, after int64) ([]dto.Message, error) {
	views, err := repository.List(ctx, actor, game, thread, after)
	if err != nil {
		return nil, err
	}
	result := make([]dto.Message, 0, len(views))
	for _, view := range views {
		result = append(result, messageFromDetails(view))
	}
	if len(result) > 0 {
		key := ""
		if thread != nil {
			key = *thread
		}
		if err := repository.MarkRead(ctx, actor, game, key, result[len(result)-1].ID); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *gameService) Announcements(ctx context.Context, actor Actor, id, after int64) ([]dto.Message, error) {
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) ([]dto.Message, error) {
		g, err := loadGame(ctx, repositories.Games, actor.ID, id, s.botName)
		if err != nil {
			return nil, err
		}
		return messages(ctx, repositories.Messages, actor.ID, g.ID, nil, after)
	})
}

func (s *gameService) LatestAnnouncement(ctx context.Context, actor Actor, id int64) (dto.AnnouncementSummary, error) {
	return execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, _ *effects) (dto.AnnouncementSummary, error) {
		g, err := loadGame(ctx, repositories.Games, actor.ID, id, s.botName)
		if err != nil {
			return dto.AnnouncementSummary{}, err
		}
		result := dto.AnnouncementSummary{}
		view, err := repositories.Messages.LatestAnnouncement(ctx, actor.ID, g.ID)
		if err != nil && err != sql.ErrNoRows {
			return result, err
		}
		if err == nil {
			latest := messageFromDetails(view)
			result.Latest = &latest
		}
		result.UnreadCount, err = repositories.Messages.UnreadAnnouncements(ctx, actor.ID, g.ID)
		return result, err
	})
}

func (s *gameService) Announce(ctx context.Context, actor Actor, id int64, input dto.Announcement) error {
	_, err := execute(ctx, s.uow, actor, s.notifier, func(repositories repos.Repositories, e *effects) (struct{}, error) {
		g, err := loadGame(ctx, repositories.Games, actor.ID, id, s.botName)
		if err != nil {
			return struct{}{}, err
		}
		if !g.IsOwner {
			return struct{}{}, forbidden()
		}
		text, err := checkedText(input.Text, 4000)
		if err != nil {
			return struct{}{}, err
		}
		if err := repositories.Messages.Create(ctx, &santa.Message{GameID: g.ID, Round: g.Round, SenderID: actor.ID, Text: text}); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, notifyMembers(ctx, repositories.Games, e, g.ID, "Новое объявление в игре «"+g.Name+"».")
	})
	return err
}
