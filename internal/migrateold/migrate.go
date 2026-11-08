package migrateold

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/DmitryKolbin/santa-tg-web/internal/database"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/repos"
	"github.com/uptrace/bun"
)

type Report struct {
	Users, Games, Members, Assignments int
	ResetGames                         []int64
}

type person struct {
	ID, TelegramID                 int64
	Name, Address, Wishlist, Place string
	Destinations                   []string
}

type game struct {
	ID, Creator int64
	Name        string
	Started     bool
	Reset       bool
	Route       []int64
	Members     []int64
}

// Migrate imports a stopped old application database into an empty new database.
func Migrate(ctx context.Context, source, target *bun.DB) (Report, error) {
	empty, err := isEmpty(ctx, target)
	if err != nil {
		return Report{}, err
	}
	if !empty {
		return Report{}, fmt.Errorf("target database is not empty")
	}

	people, peopleByID, err := loadPeople(ctx, source)
	if err != nil {
		return Report{}, err
	}
	games, members, err := loadGames(ctx, source, peopleByID)
	if err != nil {
		return Report{}, err
	}

	if err := database.Migrate(ctx, target); err != nil {
		_ = database.ResetEmptyTarget(ctx, target)
		return Report{}, err
	}

	report := Report{Users: len(people), Games: len(games), Members: members}
	store := repos.New(target)
	err = store.InTx(ctx, func(repositories repos.Repositories) error {
		for _, p := range people {
			u := &santa.User{ID: p.TelegramID, Name: p.Name, Address: p.Address, Wishlist: p.Wishlist, Location: p.Place, Destinations: santa.StringList(p.Destinations)}
			if err := repositories.Import.User(ctx, u); err != nil {
				return fmt.Errorf("import person %d: %w", p.ID, err)
			}
		}
		for _, g := range games {
			model := &santa.Game{ID: g.ID, Name: g.Name, CreatorID: peopleByID[g.Creator], Invite: randomKey(), SingleCycle: true, AllowMutualPairs: true, Started: g.Started, Round: 1}
			if err := repositories.Import.Game(ctx, model); err != nil {
				return fmt.Errorf("import game %d: %w", g.ID, err)
			}
			for _, oldID := range g.Members {
				if err := repositories.Games.AddMember(ctx, g.ID, peopleByID[oldID]); err != nil {
					return fmt.Errorf("import member %d of game %d: %w", oldID, g.ID, err)
				}
			}
			if g.Started {
				for i, sender := range g.Route {
					receiver := g.Route[(i+1)%len(g.Route)]
					if err := repositories.Games.CreateAssignment(ctx, &santa.Assignment{Thread: randomKey(), GameID: g.ID, Round: 1, SenderID: peopleByID[sender], ReceiverID: peopleByID[receiver]}); err != nil {
						return fmt.Errorf("import assignments of game %d: %w", g.ID, err)
					}
					report.Assignments++
				}
			}
			if g.Reset {
				report.ResetGames = append(report.ResetGames, g.ID)
			}
		}
		return repositories.Import.Verify(ctx, map[string]int{"users": report.Users, "games": report.Games, "members": report.Members, "assignments": report.Assignments})
	})
	if err != nil {
		if resetErr := database.ResetEmptyTarget(ctx, target); resetErr != nil {
			return Report{}, fmt.Errorf("%v; reset target: %w", err, resetErr)
		}
		return Report{}, err
	}
	return report, nil
}

func isEmpty(ctx context.Context, db *bun.DB) (bool, error) {
	var count int
	err := db.NewSelect().TableExpr("sqlite_master").ColumnExpr("count(*)").Where("type IN ('table','view','trigger') AND name NOT LIKE 'sqlite_%'").Scan(ctx, &count)
	return count == 0, err
}

func loadPeople(ctx context.Context, db *bun.DB) ([]person, map[int64]int64, error) {
	people := []person{}
	err := db.NewSelect().TableExpr("person AS p").ColumnExpr("p.id,p.telegram_id,p.name,COALESCE(p.address,'') AS address,COALESCE(p.wishlist,'') AS wishlist,COALESCE(l.code,'') AS place").Join("LEFT JOIN location AS l ON l.id=p.location_id").OrderExpr("p.id").Scan(ctx, &people)
	if err != nil {
		return nil, nil, fmt.Errorf("read people: %w", err)
	}
	byID, telegramIDs := map[int64]int64{}, map[int64]bool{}
	for _, p := range people {
		if p.TelegramID <= 0 || telegramIDs[p.TelegramID] {
			return nil, nil, fmt.Errorf("person %d has invalid or duplicate telegram_id %d", p.ID, p.TelegramID)
		}
		telegramIDs[p.TelegramID] = true
		byID[p.ID] = p.TelegramID
	}
	var destinations []struct {
		PersonID int64  `bun:"person_id"`
		Code     string `bun:"code"`
	}
	err = db.NewSelect().TableExpr("person_where_can_send AS pw").ColumnExpr("pw.person_id,l.code").Join("JOIN location AS l ON l.id=pw.location_id").OrderExpr("pw.person_id,l.code").Scan(ctx, &destinations)
	if err != nil {
		return nil, nil, fmt.Errorf("read destinations: %w", err)
	}
	indexes := map[int64]int{}
	for i := range people {
		indexes[people[i].ID] = i
	}
	for _, destination := range destinations {
		i, ok := indexes[destination.PersonID]
		if !ok || destination.Code == "" {
			return nil, nil, fmt.Errorf("invalid destination for person %d", destination.PersonID)
		}
		people[i].Destinations = append(people[i].Destinations, destination.Code)
	}
	return people, byID, nil
}

func loadGames(ctx context.Context, db *bun.DB, people map[int64]int64) ([]game, int, error) {
	var rows []struct {
		ID, Creator  int64
		Name, Status string
		Route        sql.NullString
	}
	err := db.NewSelect().TableExpr("game").ColumnExpr("id,name,status,creator_id AS creator,route").OrderExpr("id").Scan(ctx, &rows)
	if err != nil {
		return nil, 0, fmt.Errorf("read games: %w", err)
	}
	games := []game{}
	for _, row := range rows {
		g := game{ID: row.ID, Creator: row.Creator, Name: row.Name, Started: row.Status == "started"}
		if _, ok := people[g.Creator]; !ok {
			return nil, 0, fmt.Errorf("game %d has unknown creator %d", g.ID, g.Creator)
		}
		if g.Started {
			g.Route = []int64{}
			if !row.Route.Valid || json.Unmarshal([]byte(row.Route.String), &g.Route) != nil || len(g.Route) < 2 {
				g.Started = false
				g.Reset = true
			}
		}
		games = append(games, g)
	}
	byID := map[int64]*game{}
	for i := range games {
		byID[games[i].ID] = &games[i]
	}
	var members []struct{ GameID, PersonID int64 }
	err = db.NewSelect().TableExpr("person_game").Column("game_id", "person_id").OrderExpr("game_id,person_id").Scan(ctx, &members)
	if err != nil {
		return nil, 0, fmt.Errorf("read members: %w", err)
	}
	count := 0
	for _, member := range members {
		g, gameOK := byID[member.GameID]
		_, personOK := people[member.PersonID]
		if !gameOK || !personOK {
			return nil, 0, fmt.Errorf("invalid membership: game %d, person %d", member.GameID, member.PersonID)
		}
		g.Members = append(g.Members, member.PersonID)
		count++
	}
	for i := range games {
		if games[i].Started && !validRoute(games[i], people) {
			games[i].Started = false
			games[i].Reset = true
		}
	}
	return games, count, nil
}

func validRoute(g game, people map[int64]int64) bool {
	members := map[int64]bool{}
	for _, id := range g.Members {
		members[id] = true
	}
	seen := map[int64]bool{}
	for _, id := range g.Route {
		if _, ok := people[id]; !ok || !members[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return len(seen) >= 2
}

func randomKey() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
