package santa

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/DmitryKolbin/santa-tg-web/internal/store"
)

type DrawGame struct {
	Force      bool   `json:"force"`
	Exclusions string `json:"exclusions"`
}

// Readiness explains whether a draw can run. Exclusions is a fingerprint of
// the excluded members that a forced draw must confirm.
type Readiness struct {
	Exclusions string            `json:"exclusions"`
	Members    []MemberReadiness `json:"members"`
	Excluded   []string          `json:"excluded"`
	Problems   []string          `json:"problems"`
	Ready      bool              `json:"ready"`
	Error      string            `json:"error,omitempty"`
}

type MemberReadiness struct {
	Name          string   `json:"name"`
	Missing       []string `json:"missing"`
	WishlistEmpty bool     `json:"wishlist_empty"`
}

type player struct {
	ID           int64
	Name         string
	Address      string
	Wishlist     string
	Location     string
	Destinations []string
}

// drawSnapshot is everything a draw depends on. The search runs outside the
// transaction, so the snapshot is compared again before assignments are saved.
type drawSnapshot struct {
	game    store.GameView
	players []player
	blocked []store.BlockedPair
}

func loadDrawSnapshot(ctx context.Context, db *store.Store, actor, id int64) (drawSnapshot, error) {
	g, err := ownedGame(ctx, db, actor, id)
	if err != nil {
		return drawSnapshot{}, err
	}
	if g.Started {
		return drawSnapshot{}, conflict("Жеребьёвка уже прошла")
	}
	ids, err := db.MemberIDs(ctx, g.ID)
	if err != nil {
		return drawSnapshot{}, err
	}
	players := make([]player, 0, len(ids))
	for _, id := range ids {
		u, err := db.User(ctx, id)
		if err != nil {
			return drawSnapshot{}, err
		}
		players = append(players, player{ID: u.ID, Name: u.Name, Address: u.Address, Wishlist: u.Wishlist, Location: u.Location, Destinations: []string(u.Destinations)})
	}
	blocked, err := db.BlockedPairs(ctx, g.ID)
	if err != nil {
		return drawSnapshot{}, err
	}
	return drawSnapshot{game: g, players: players, blocked: blocked}, nil
}

func (s *Service) CheckReadiness(ctx context.Context, actor Actor, id int64, input DrawGame) (Readiness, error) {
	snapshot, err := loadDrawSnapshot(ctx, s.store, actor.ID, id)
	if err != nil {
		return Readiness{}, err
	}
	report, _, err := s.prepare(ctx, snapshot, input.Force)
	return report, err
}

func (s *Service) Draw(ctx context.Context, actor Actor, id int64, input DrawGame) (Readiness, error) {
	snapshot, err := loadDrawSnapshot(ctx, s.store, actor.ID, id)
	if err != nil {
		return Readiness{}, err
	}
	report, pairs, err := s.prepare(ctx, snapshot, input.Force)
	if err != nil {
		return Readiness{}, err
	}
	if !report.Ready {
		return Readiness{}, Error{Kind: Unprocessable, Text: report.Error}
	}
	if input.Force && input.Exclusions != report.Exclusions {
		return Readiness{}, conflict("Список исключений изменился. Проверьте готовность и подтвердите запуск ещё раз.")
	}
	err = s.write(ctx, func(tx *store.Store, n *notes) error {
		current, err := loadDrawSnapshot(ctx, tx, actor.ID, id)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, snapshot) {
			return conflict("Состав игры или анкеты изменились. Проверьте готовность и запустите жеребьёвку ещё раз.")
		}
		g := current.game
		for _, pair := range pairs {
			if err := tx.CreateAssignment(ctx, &store.Assignment{Thread: randomKey(), GameID: g.ID, Round: g.Round, SenderID: pair.sender, ReceiverID: pair.receiver}); err != nil {
				return err
			}
		}
		if err := tx.UpdateGame(ctx, &store.Game{ID: g.ID, Started: true}, "started"); err != nil {
			return err
		}
		return notifyMembers(ctx, tx, n, g.ID, "Жеребьёвка в игре «"+g.Name+"» завершена. Откройте приложение, чтобы посмотреть результат.")
	})
	if err != nil {
		return Readiness{}, err
	}
	return report, nil
}

type pair struct{ sender, receiver int64 }

// prepare checks the questionnaires and searches for sender→receiver pairs
// among the members with a complete questionnaire.
func (s *Service) prepare(ctx context.Context, snapshot drawSnapshot, force bool) (Readiness, []pair, error) {
	report := Readiness{Members: []MemberReadiness{}, Excluded: []string{}, Problems: []string{}}
	players, excluded := drawnPlayers(snapshot.players, &report)
	report.Exclusions = excluded
	if len(report.Excluded) > 0 && !force {
		report.Error = "Не все участники заполнили анкеты"
		return report, nil, nil
	}
	if len(players) < 2 {
		report.Error = "Нужно минимум два участника с заполненной анкетой"
		return report, nil, nil
	}
	singleCycle, allowMutual := snapshot.game.SingleCycle, snapshot.game.AllowMutualPairs
	if len(players) == 2 && !allowMutual {
		report.Error = "При запрете взаимных пар нужно минимум три участника"
		report.Problems = append(report.Problems, "Разрешите взаимные пары или добавьте ещё одного участника.")
		return report, nil, nil
	}
	shipping := shippingGraph(players)
	graph := withoutBlocked(shipping, players, snapshot.blocked)
	assignments, err := FindAssignments(ctx, graph, singleCycle, allowMutual)
	if errors.Is(err, ErrNoAssignment) {
		report.Error = "Нет распределения с выбранными настройками и направлениями доставки"
		if _, relaxedErr := FindAssignments(ctx, shipping, false, true); relaxedErr != nil {
			report.Problems = append(report.Problems, shippingProblems(players, shipping, s.locations)...)
		} else if _, relaxedErr := FindAssignments(ctx, graph, false, true); relaxedErr != nil {
			report.Error = "Нет распределения с текущими запретами"
			report.Problems = append(report.Problems, blockedProblems(players, shipping, graph)...)
		} else {
			switch {
			case singleCycle && !allowMutual:
				report.Problems = append(report.Problems, "Доставка возможна, но текущий состав не позволяет одновременно собрать одну цепочку и запретить взаимные пары. Ослабьте одно из правил.")
			case singleCycle:
				report.Problems = append(report.Problems, "Доставка возможна только в нескольких цепочках. Отключите правило «Только одна цепочка».")
			case !allowMutual:
				report.Problems = append(report.Problems, "Доставка возможна только с взаимными парами. Включите правило «Разрешить взаимные пары».")
			}
		}
		return report, nil, nil
	}
	if err != nil {
		return report, nil, err
	}
	report.Ready = true
	pairs := make([]pair, 0, len(assignments))
	for from, to := range assignments {
		pairs = append(pairs, pair{sender: players[from].ID, receiver: players[to].ID})
	}
	return report, pairs, nil
}

// drawnPlayers reports each member and returns those with a complete
// questionnaire plus a fingerprint of the excluded ones.
func drawnPlayers(members []player, report *Readiness) ([]player, string) {
	players := make([]player, 0, len(members))
	excluded := sha256.New()
	for _, p := range members {
		missing := missingFields(p)
		report.Members = append(report.Members, MemberReadiness{Name: p.Name, Missing: missing, WishlistEmpty: p.Wishlist == ""})
		if len(missing) > 0 {
			report.Excluded = append(report.Excluded, p.Name)
			fmt.Fprintf(excluded, "%d:%s\n", p.ID, p.Name)
		} else {
			players = append(players, p)
		}
	}
	return players, fmt.Sprintf("%x", excluded.Sum(nil))
}

func missingFields(p player) []string {
	return missingProfile(store.User{Address: p.Address, Location: p.Location, Destinations: p.Destinations})
}

// missingProfile lists the questionnaire fields a member must fill to be drawn.
func missingProfile(u store.User) []string {
	result := []string{}
	if strings.TrimSpace(u.Address) == "" {
		result = append(result, "Адрес")
	}
	if u.Location == "" {
		result = append(result, "Местоположение")
	}
	if len(u.Destinations) == 0 {
		result = append(result, "Направления отправки")
	}
	return result
}

// shippingGraph has an edge from a santa to every player living in one of the
// santa's shipping destinations.
func shippingGraph(players []player) Graph {
	graph := Graph{PersonCount: int64(len(players)), Edges: make([]bool, len(players)*len(players))}
	for from, p := range players {
		for to, receiver := range players {
			if from == to {
				continue
			}
			for _, destination := range p.Destinations {
				if destination == receiver.Location {
					graph.SetEdge(int64(from), int64(to))
					break
				}
			}
		}
	}
	return graph
}

// withoutBlocked copies the graph without the pairs the organizer forbade.
func withoutBlocked(graph Graph, players []player, blocked []store.BlockedPair) Graph {
	result := Graph{PersonCount: graph.PersonCount, Edges: append([]bool(nil), graph.Edges...)}
	index := map[int64]int64{}
	for i, p := range players {
		index[p.ID] = int64(i)
	}
	for _, pair := range blocked {
		from, okFrom := index[pair.SenderID]
		to, okTo := index[pair.ReceiverID]
		if okFrom && okTo {
			result.Edges[from*result.PersonCount+to] = false
		}
	}
	return result
}

// blockedProblems names the players the forbidden pairs leave without a
// receiver or without a santa although shipping alone would allow one.
func blockedProblems(players []player, shipping, graph Graph) []string {
	var problems []string
	for from, p := range players {
		before, after := 0, 0
		for to := range players {
			if shipping.HasEdge(int64(from), int64(to)) {
				before++
			}
			if graph.HasEdge(int64(from), int64(to)) {
				after++
			}
		}
		if before > 0 && after == 0 {
			problems = append(problems, fmt.Sprintf("%s не может никому дарить: все возможные получатели под запретом.", p.Name))
		}
	}
	for to, p := range players {
		before, after := 0, 0
		for from := range players {
			if shipping.HasEdge(int64(from), int64(to)) {
				before++
			}
			if graph.HasEdge(int64(from), int64(to)) {
				after++
			}
		}
		if before > 0 && after == 0 {
			problems = append(problems, fmt.Sprintf("%s остаётся без Санты: всем, кто может отправить подарок, это запрещено.", p.Name))
		}
	}
	if len(problems) == 0 {
		problems = append(problems, "Запретов слишком много: с ними нельзя распределить всех участников. Снимите часть запретов.")
	}
	return problems
}

func shippingProblems(players []player, graph Graph, locations map[string]string) []string {
	var problems []string
	for from, p := range players {
		hasReceiver := false
		for to := range players {
			hasReceiver = hasReceiver || graph.HasEdge(int64(from), int64(to))
		}
		if !hasReceiver {
			names := make([]string, 0, len(p.Destinations))
			for _, code := range p.Destinations {
				names = append(names, locations[code])
			}
			problems = append(problems, fmt.Sprintf("%s не может никому отправить подарок: среди участников нет получателей из выбранных направлений (%s).", p.Name, strings.Join(names, ", ")))
		}
	}
	blocked := map[string][]string{}
	for to, p := range players {
		hasSanta := false
		for from := range players {
			hasSanta = hasSanta || graph.HasEdge(int64(from), int64(to))
		}
		if !hasSanta {
			blocked[p.Location] = append(blocked[p.Location], p.Name)
		}
	}
	codes := make([]string, 0, len(blocked))
	for code := range blocked {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		problems = append(problems, fmt.Sprintf("Никто не может отправить подарок участникам из локации «%s»: %s.", locations[code], strings.Join(blocked[code], ", ")))
	}
	if len(problems) == 0 {
		problems = append(problems, "Направления доставки нескольких участников конфликтуют между собой. Расширьте доступные направления отправки.")
	}
	return problems
}
