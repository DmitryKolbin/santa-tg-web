package repos_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	appdb "github.com/DmitryKolbin/santa-tg-web/internal/database"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/repos"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	_ "github.com/uptrace/bun/driver/sqliteshim"
)

func TestRepositories(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Repositories Suite")
}

func testStore() *repos.Store {
	sqldb, err := sql.Open("sqliteshim", filepath.Join(GinkgoT().TempDir(), "test.db"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(sqldb.Close)
	db := appdb.New(sqldb)
	Expect(appdb.Migrate(context.Background(), db)).To(Succeed())
	return repos.New(db)
}

var _ = Describe("Store", func() {
	It("rolls back the complete unit of work", func() {
		store := testStore()
		want := errors.New("stop")
		err := store.InTx(context.Background(), func(repositories repos.Repositories) error {
			Expect(repositories.Import.User(context.Background(), &santa.User{ID: 1, Name: "One", Destinations: santa.StringList{}})).To(Succeed())
			return want
		})
		Expect(err).To(MatchError(want))

		var count int
		Expect(store.DB().NewSelect().Table("users").ColumnExpr("count(*)").Scan(context.Background(), &count)).To(Succeed())
		Expect(count).To(BeZero())
	})

	It("persists models and their relations", func() {
		store := testStore()
		ctx := context.Background()
		Expect(store.InTx(ctx, func(repositories repos.Repositories) error {
			user := &santa.User{ID: 10, Name: "One", Address: "Address", Location: "ge", Destinations: santa.StringList{"ge", "de"}}
			if err := repositories.Import.User(ctx, user); err != nil {
				return err
			}
			game := &santa.Game{Name: "Game", CreatorID: user.ID, Invite: "invite", SingleCycle: true, AllowMutualPairs: true, Round: 1}
			if err := repositories.Games.Create(ctx, game); err != nil {
				return err
			}
			return repositories.Games.AddMember(ctx, game.ID, user.ID)
		})).To(Succeed())

		repositories := store.Repositories()
		user, err := repositories.Users.Get(ctx, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(user.Destinations).To(Equal(santa.StringList{"ge", "de"}))

		games, err := repositories.Games.List(ctx, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(games).To(HaveLen(1))
		Expect(games[0].CreatorName).To(Equal("One"))
		Expect(games[0].IsMember).To(BeTrue())
		Expect(games[0].MemberCount).To(Equal(1))
	})
})
