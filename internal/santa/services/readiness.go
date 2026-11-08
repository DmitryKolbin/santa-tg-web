package services

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
)

type Player struct {
	ID           int64
	Name         string
	Address      string
	Wishlist     string
	Location     string
	Destinations []string
}

type DrawSettings struct {
	SingleCycle      bool
	AllowMutualPairs bool
}

func PrepareAssignments(ctx context.Context, settings DrawSettings, members []Player, force bool, locations map[string]string) (dto.Readiness, []Player, []int, error) {
	report := dto.Readiness{Members: []dto.MemberReadiness{}, Excluded: []string{}, Problems: []string{}}
	players := make([]Player, 0, len(members))
	excluded := sha256.New()
	for _, player := range members {
		missing := missingFields(player)
		report.Members = append(report.Members, dto.MemberReadiness{Name: player.Name, Missing: missing, WishlistEmpty: player.Wishlist == ""})
		if len(missing) > 0 {
			report.Excluded = append(report.Excluded, player.Name)
			fmt.Fprintf(excluded, "%d:%s\n", player.ID, player.Name)
		} else {
			players = append(players, player)
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
	if len(players) == 2 && !settings.AllowMutualPairs {
		report.Error = "При запрете взаимных пар нужно минимум три участника"
		report.Problems = append(report.Problems, "Разрешите взаимные пары или добавьте ещё одного участника.")
		return report, players, nil, nil
	}
	graph := santa.Graph{PersonCount: int64(len(players)), Edges: make([]bool, len(players)*len(players))}
	for from, player := range players {
		for to, receiver := range players {
			if from == to {
				continue
			}
			for _, destination := range player.Destinations {
				if destination == receiver.Location {
					graph.SetEdge(int64(from), int64(to))
					break
				}
			}
		}
	}
	assignments, err := FindAssignments(ctx, graph, settings.SingleCycle, settings.AllowMutualPairs)
	if errors.Is(err, ErrNoAssignment) {
		report.Error = "Нет распределения с выбранными настройками и направлениями доставки"
		if _, relaxedErr := FindAssignments(ctx, graph, false, true); relaxedErr == nil {
			switch {
			case settings.SingleCycle && !settings.AllowMutualPairs:
				report.Problems = append(report.Problems, "Доставка возможна, но текущий состав не позволяет одновременно собрать одну цепочку и запретить взаимные пары. Ослабьте одно из правил.")
			case settings.SingleCycle:
				report.Problems = append(report.Problems, "Доставка возможна только в нескольких цепочках. Отключите правило «Только одна цепочка».")
			case !settings.AllowMutualPairs:
				report.Problems = append(report.Problems, "Доставка возможна только с взаимными парами. Включите правило «Разрешить взаимные пары».")
			}
		} else {
			shippingProblems(&report, players, graph, locations)
		}
		return report, players, nil, nil
	}
	if err != nil {
		return report, players, nil, err
	}
	report.Ready = true
	return report, players, assignments, nil
}

func missingFields(player Player) []string {
	result := []string{}
	if strings.TrimSpace(player.Address) == "" {
		result = append(result, "Адрес")
	}
	if player.Location == "" {
		result = append(result, "Местоположение")
	}
	if len(player.Destinations) == 0 {
		result = append(result, "Направления отправки")
	}
	return result
}

func shippingProblems(report *dto.Readiness, players []Player, graph santa.Graph, locations map[string]string) {
	for from, player := range players {
		hasReceiver := false
		for to := range players {
			hasReceiver = hasReceiver || graph.HasEdge(int64(from), int64(to))
		}
		if !hasReceiver {
			names := make([]string, 0, len(player.Destinations))
			for _, code := range player.Destinations {
				names = append(names, locations[code])
			}
			report.Problems = append(report.Problems, fmt.Sprintf("%s не может никому отправить подарок: среди участников нет получателей из выбранных направлений (%s).", player.Name, strings.Join(names, ", ")))
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
		report.Problems = append(report.Problems, fmt.Sprintf("Никто не может отправить подарок участникам из локации «%s»: %s.", locations[code], strings.Join(blocked[code], ", ")))
	}
	if len(report.Problems) == 0 {
		report.Problems = append(report.Problems, "Направления доставки нескольких участников конфликтуют между собой. Расширьте доступные направления отправки.")
	}
}
