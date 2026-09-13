package mini

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/services"
)

type game struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Creator          int64  `json:"-"`
	CreatorName      string `json:"creator_name"`
	Invite           string `json:"-"`
	SingleCycle      bool   `json:"single_cycle"`
	AllowMutualPairs bool   `json:"allow_mutual_pairs"`
	Started          bool   `json:"started"`
	Round            int    `json:"round"`
	IsOwner          bool   `json:"is_owner"`
	IsMember         bool   `json:"is_member"`
	Count            int    `json:"count"`
	InviteURL        string `json:"invite_url,omitempty"`
}

const gameSelect = `SELECT g.id,g.name,g.creator,u.name,g.invite,g.single_cycle,g.allow_mutual_pairs,g.started,g.round,EXISTS(SELECT 1 FROM members WHERE game=g.id AND person=?),(SELECT count(*) FROM members WHERE game=g.id) FROM games g JOIN users u ON u.id=g.creator `

type scanner interface{ Scan(...any) error }

func (q *request) scanGame(row scanner) (game, error) {
	var g game
	err := row.Scan(&g.ID, &g.Name, &g.Creator, &g.CreatorName, &g.Invite, &g.SingleCycle, &g.AllowMutualPairs, &g.Started, &g.Round, &g.IsMember, &g.Count)
	g.IsOwner = g.Creator == q.user
	if g.IsOwner {
		g.InviteURL = "https://t.me/" + q.s.botName + "?startapp=" + url.QueryEscape(g.Invite)
	}
	return g, err
}
func (q *request) game() (game, error) {
	id, err := q.gameID()
	if err != nil {
		return game{}, err
	}
	g, err := q.scanGame(q.tx.QueryRowContext(q.r.Context(), gameSelect+`WHERE g.id=?`, q.user, id))
	if err == nil && !g.IsOwner && !g.IsMember {
		return g, forbidden()
	}
	return g, err
}
func getGame(q *request) (any, error) { return q.game() }
func listGames(q *request) (any, error) {
	rows, err := q.tx.QueryContext(q.r.Context(), gameSelect+`WHERE g.creator=? OR EXISTS(SELECT 1 FROM members WHERE game=g.id AND person=?) ORDER BY g.id DESC`, q.user, q.user, q.user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	games := []game{}
	for rows.Next() {
		g, err := q.scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, g)
	}
	return games, rows.Err()
}
func createGame(q *request) (any, error) {
	input := struct {
		Name             string `json:"name"`
		SingleCycle      bool   `json:"single_cycle"`
		AllowMutualPairs bool   `json:"allow_mutual_pairs"`
	}{SingleCycle: true, AllowMutualPairs: true}
	if err := q.decode(&input); err != nil {
		return nil, err
	}
	name, err := checkedText(input.Name, 120)
	if err != nil {
		return nil, err
	}
	result, err := q.tx.ExecContext(q.r.Context(), `INSERT INTO games(name,creator,invite,single_cycle,allow_mutual_pairs) VALUES(?,?,?,?,?)`, name, q.user, randomKey(), input.SingleCycle, input.AllowMutualPairs)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return q.scanGame(q.tx.QueryRowContext(q.r.Context(), gameSelect+`WHERE g.id=?`, q.user, id))
}
func editGame(q *request) (any, error) {
	g, err := q.game()
	if err != nil {
		return nil, err
	}
	if !g.IsOwner {
		return nil, forbidden()
	}
	if g.Started {
		return nil, apiError{409, "Сначала сбросьте жеребьёвку"}
	}
	input := struct {
		Name             string `json:"name"`
		SingleCycle      bool   `json:"single_cycle"`
		AllowMutualPairs bool   `json:"allow_mutual_pairs"`
	}{g.Name, g.SingleCycle, g.AllowMutualPairs}
	if err := q.decode(&input); err != nil {
		return nil, err
	}
	name, err := checkedText(input.Name, 120)
	if err != nil {
		return nil, err
	}
	if err := q.exec(`UPDATE games SET name=?,single_cycle=?,allow_mutual_pairs=? WHERE id=?`, name, input.SingleCycle, input.AllowMutualPairs, g.ID); err != nil {
		return nil, err
	}
	return q.game()
}
func (q *request) invite() (game, error) {
	return q.scanGame(q.tx.QueryRowContext(q.r.Context(), gameSelect+`WHERE g.invite=?`, q.user, q.r.PathValue("token")))
}
func getInvite(q *request) (any, error) {
	g, err := q.invite()
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": g.ID, "name": g.Name, "creator_name": g.CreatorName, "started": g.Started, "is_member": g.IsMember}, nil
}
func joinGame(q *request) (any, error) {
	g, err := q.invite()
	if err != nil {
		return nil, err
	}
	if g.IsMember {
		return map[string]int64{"id": g.ID}, nil
	}
	if g.Started {
		return nil, apiError{409, "Жеребьёвка уже прошла"}
	}
	if g.Count >= 50 {
		return nil, bad("В игре уже 50 участников")
	}
	if err := q.exec(`INSERT INTO members(game,person) VALUES(?,?)`, g.ID, q.user); err != nil {
		return nil, err
	}
	return map[string]int64{"id": g.ID}, nil
}

type readinessResult struct {
	Exclusions string            `json:"exclusions"`
	Members    []memberReadiness `json:"members"`
	Excluded   []string          `json:"excluded"`
	Problems   []string          `json:"problems"`
	Ready      bool              `json:"ready"`
	Error      string            `json:"error,omitempty"`
}
type memberReadiness struct {
	Name          string   `json:"name"`
	Missing       []string `json:"missing"`
	WishlistEmpty bool     `json:"wishlist_empty"`
}

func (q *request) prepare(g game, force bool) (readinessResult, []profile, []int, error) {
	report := readinessResult{Members: []memberReadiness{}, Excluded: []string{}, Problems: []string{}}
	rows, err := q.tx.QueryContext(q.r.Context(), `SELECT person FROM members WHERE game=? ORDER BY person`, g.ID)
	if err != nil {
		return report, nil, nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return report, nil, nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, nil, nil, err
	}
	players := []profile{}
	excluded := sha256.New()
	for _, id := range ids {
		p, err := q.profile(id)
		if err != nil {
			return report, nil, nil, err
		}
		absent := missing(p)
		report.Members = append(report.Members, memberReadiness{p.Name, absent, p.Wishlist == ""})
		if len(absent) > 0 {
			report.Excluded = append(report.Excluded, p.Name)
			fmt.Fprintf(excluded, "%d:%s\n", p.ID, p.Name)
		} else {
			players = append(players, p)
		}
	}
	report.Exclusions = fmt.Sprintf("%x", excluded.Sum(nil))
	if len(report.Excluded) > 0 && !force {
		report.Error = "Не все участники заполнили анкеты"
		return report, players, nil, nil
	}
	if len(players) < 2 {
		report.Error = "Нужно минимум два участника с заполненной анкетой"
		return report, players, nil, nil
	}
	if len(players) == 2 && !g.AllowMutualPairs {
		report.Error = "При запрете взаимных пар нужно минимум три участника"
		report.Problems = append(report.Problems, "Разрешите взаимные пары или добавьте ещё одного участника.")
		return report, players, nil, nil
	}
	graph := santa.Graph{PersonCount: int64(len(players)), Edges: make([]bool, len(players)*len(players))}
	for from, p := range players {
		for to, receiver := range players {
			if from == to {
				continue
			}
			for _, dest := range p.Destinations {
				if dest == receiver.Location {
					graph.SetEdge(int64(from), int64(to))
					break
				}
			}
		}
	}
	assignments, err := services.FindAssignments(q.r.Context(), graph, g.SingleCycle, g.AllowMutualPairs)
	if errors.Is(err, services.ErrNoAssignment) {
		report.Error = "Нет распределения с выбранными настройками и направлениями доставки"
		if _, relaxedErr := services.FindAssignments(q.r.Context(), graph, false, true); relaxedErr == nil {
			switch {
			case g.SingleCycle && !g.AllowMutualPairs:
				report.Problems = append(report.Problems, "Доставка возможна, но текущий состав не позволяет одновременно собрать одну цепочку и запретить взаимные пары. Ослабьте одно из правил.")
			case g.SingleCycle:
				report.Problems = append(report.Problems, "Доставка возможна только в нескольких цепочках. Отключите правило «Только одна цепочка».")
			case !g.AllowMutualPairs:
				report.Problems = append(report.Problems, "Доставка возможна только с взаимными парами. Включите правило «Разрешить взаимные пары».")
			}
		} else {
			q.shippingProblems(&report, players, graph)
		}
		return report, players, nil, nil
	}
	if err != nil {
		return report, players, nil, err
	}
	report.Ready = true
	return report, players, assignments, nil
}

func (q *request) shippingProblems(report *readinessResult, players []profile, graph santa.Graph) {
	for from, player := range players {
		hasReceiver := false
		for to := range players {
			hasReceiver = hasReceiver || graph.HasEdge(int64(from), int64(to))
		}
		if !hasReceiver {
			destinations := make([]string, 0, len(player.Destinations))
			for _, code := range player.Destinations {
				destinations = append(destinations, q.s.locations[code])
			}
			report.Problems = append(report.Problems, fmt.Sprintf("%s не может никому отправить подарок: среди участников нет получателей из выбранных направлений (%s).", player.Name, strings.Join(destinations, ", ")))
		}
	}
	blocked := map[string][]string{}
	for to, player := range players {
		hasSanta := false
		for from := range players {
			hasSanta = hasSanta || graph.HasEdge(int64(from), int64(to))
		}
		if !hasSanta {
			blocked[player.Location] = append(blocked[player.Location], player.Name)
		}
	}
	codes := make([]string, 0, len(blocked))
	for code := range blocked {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		names := blocked[code]
		report.Problems = append(report.Problems, fmt.Sprintf("Никто не может отправить подарок участникам из локации «%s»: %s.", q.s.locations[code], strings.Join(names, ", ")))
	}
	if len(report.Problems) == 0 {
		report.Problems = append(report.Problems, "Направления доставки нескольких участников конфликтуют между собой. Расширьте доступные направления отправки.")
	}
}

type drawOptions struct {
	Force      bool   `json:"force"`
	Exclusions string `json:"exclusions"`
}

func (q *request) drawInput() (game, drawOptions, error) {
	g, err := q.game()
	if err != nil {
		return g, drawOptions{}, err
	}
	if !g.IsOwner {
		return g, drawOptions{}, forbidden()
	}
	if g.Started {
		return g, drawOptions{}, apiError{409, "Жеребьёвка уже прошла"}
	}
	var input drawOptions
	err = q.decode(&input)
	return g, input, err
}
func readiness(q *request) (any, error) {
	g, input, err := q.drawInput()
	if err != nil {
		return nil, err
	}
	result, _, _, err := q.prepare(g, input.Force)
	return result, err
}
func drawGame(q *request) (any, error) {
	g, input, err := q.drawInput()
	if err != nil {
		return nil, err
	}
	report, players, assignments, err := q.prepare(g, input.Force)
	if err != nil {
		return nil, err
	}
	if !report.Ready {
		return nil, apiError{422, report.Error}
	}
	if input.Force && input.Exclusions != report.Exclusions {
		return nil, apiError{409, "Список исключений изменился. Проверьте готовность и подтвердите запуск ещё раз."}
	}
	for from, to := range assignments {
		if err := q.exec(`INSERT INTO assignments(thread,game,round,sender,receiver) VALUES(?,?,?,?,?)`, randomKey(), g.ID, g.Round, players[from].ID, players[to].ID); err != nil {
			return nil, err
		}
	}
	if err := q.exec(`UPDATE games SET started=1 WHERE id=?`, g.ID); err != nil {
		return nil, err
	}
	if err := q.notifyMembers(g.ID, "Жеребьёвка в игре «"+g.Name+"» завершена. Откройте приложение, чтобы посмотреть результат."); err != nil {
		return nil, err
	}
	return report, nil
}
func resetGame(q *request) (any, error) {
	g, err := q.game()
	if err != nil {
		return nil, err
	}
	if !g.IsOwner {
		return nil, forbidden()
	}
	if !g.Started {
		return nil, apiError{409, "Игра ещё не разыграна"}
	}
	if err := q.exec(`UPDATE games SET started=0,round=round+1 WHERE id=?`, g.ID); err != nil {
		return nil, err
	}
	return nil, q.notifyMembers(g.ID, fmt.Sprintf("Жеребьёвка в игре «%s» сброшена. Прежние назначения больше не действуют.", g.Name))
}
func getReceiver(q *request) (any, error) {
	g, err := q.game()
	if err != nil {
		return nil, err
	}
	if !g.Started {
		return nil, apiError{409, "Жеребьёвка ещё не прошла"}
	}
	var id int64
	err = q.tx.QueryRowContext(q.r.Context(), `SELECT receiver FROM assignments WHERE game=? AND round=? AND sender=?`, g.ID, g.Round, q.user).Scan(&id)
	if err != nil {
		return nil, err
	}
	p, err := q.profile(id)
	if err != nil {
		return nil, err
	}
	return map[string]string{"name": p.Name, "address": p.Address, "wishlist": p.Wishlist}, nil
}
