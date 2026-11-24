package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/DmitryKolbin/santa-tg-web/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	_ "github.com/uptrace/bun/driver/sqliteshim"
)

func TestRepositories(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Repositories Suite")
}

func testStore() *store.Store {
	sqldb, err := sql.Open("sqliteshim", filepath.Join(GinkgoT().TempDir(), "test.db"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(sqldb.Close)
	db := store.New(sqldb)
	Expect(store.Migrate(context.Background(), db)).To(Succeed())
	return store.NewStore(db)
}

var _ = Describe("Store", func() {
	It("rolls back the complete unit of work", func() {
		st := testStore()
		want := errors.New("stop")
		err := st.Tx(context.Background(), func(tx *store.Store) error {
			Expect(tx.InsertUser(context.Background(), &store.User{ID: 1, Name: "One", Destinations: store.StringList{}})).To(Succeed())
			return want
		})
		Expect(err).To(MatchError(want))

		var count int
		Expect(st.DB().NewSelect().Table("users").ColumnExpr("count(*)").Scan(context.Background(), &count)).To(Succeed())
		Expect(count).To(BeZero())
	})

	It("persists models and their relations", func() {
		st := testStore()
		ctx := context.Background()
		Expect(st.Tx(ctx, func(tx *store.Store) error {
			user := &store.User{ID: 10, Name: "One", Address: "Address", Location: "ge", Destinations: store.StringList{"ge", "de"}}
			if err := tx.InsertUser(ctx, user); err != nil {
				return err
			}
			game := &store.Game{Name: "Game", CreatorID: user.ID, Invite: "invite", SingleCycle: true, AllowMutualPairs: true, Round: 1}
			if err := tx.CreateGame(ctx, game); err != nil {
				return err
			}
			return tx.AddMember(ctx, game.ID, user.ID)
		})).To(Succeed())

		user, err := st.User(ctx, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(user.Destinations).To(Equal(store.StringList{"ge", "de"}))

		games, err := st.GameViews(ctx, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(games).To(HaveLen(1))
		Expect(games[0].CreatorName).To(Equal("One"))
		Expect(games[0].IsMember).To(BeTrue())
		Expect(games[0].MemberCount).To(Equal(1))
	})
})
