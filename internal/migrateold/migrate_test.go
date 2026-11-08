package migrateold

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	appdb "github.com/DmitryKolbin/santa-tg-web/internal/database"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/uptrace/bun"
	_ "github.com/uptrace/bun/driver/sqliteshim"
)

const oldSchema = `
CREATE TABLE location (id INTEGER PRIMARY KEY, name TEXT, code TEXT UNIQUE);
CREATE TABLE person (id INTEGER PRIMARY KEY, name TEXT NOT NULL, address TEXT, wishlist TEXT, telegram_id INTEGER, location_id INTEGER, active BOOLEAN, status TEXT, created_at TIMESTAMP, updated_at TIMESTAMP);
CREATE TABLE game (id INTEGER PRIMARY KEY, name TEXT NOT NULL, status TEXT, creator_id INTEGER, route integer[], created_at TIMESTAMP, updated_at TIMESTAMP);
CREATE TABLE person_game (person_id INTEGER, game_id INTEGER, PRIMARY KEY(person_id,game_id));
CREATE TABLE person_where_can_send (person_id INTEGER, location_id INTEGER, PRIMARY KEY(person_id,location_id));
`

func TestMigration(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Legacy Migration Suite")
}

func openDB(name string) *bun.DB {
	db, err := sql.Open("sqliteshim", filepath.Join(ginkgo.GinkgoT().TempDir(), name))
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(db.Close)
	return appdb.New(db)
}

var _ = ginkgo.Describe("Legacy database migration", func() {
	ginkgo.It("imports profiles, games, memberships and valid assignments", func() {
		source, target := openDB("old.db"), openDB("new.db")
		_, err := source.Exec(oldSchema + `
INSERT INTO location VALUES(1,'A','a'),(2,'B','b');
INSERT INTO person(id,name,address,wishlist,telegram_id,location_id) VALUES
 (1,'One','Address 1','Wish 1',101,1),(2,'Two','Address 2',NULL,102,2),(3,'Three',NULL,NULL,103,NULL);
INSERT INTO person_where_can_send VALUES(1,1),(1,2),(2,1),(3,2);
INSERT INTO game(id,name,status,creator_id,route) VALUES
 (10,'Started','started',1,'[1,2,3]'),(20,'Broken','started',2,'null'),(30,'Draft','init',3,NULL);
INSERT INTO person_game VALUES(1,10),(2,10),(3,10),(1,20),(2,20),(3,30);
`)
		Expect(err).NotTo(HaveOccurred())

		report, err := Migrate(context.Background(), source, target)
		Expect(err).NotTo(HaveOccurred())
		Expect(report.Users).To(Equal(3))
		Expect(report.Games).To(Equal(3))
		Expect(report.Members).To(Equal(6))
		Expect(report.Assignments).To(Equal(3))
		Expect(report.ResetGames).To(Equal([]int64{20}))

		var name, address, wishlist, location, encoded string
		Expect(target.QueryRow(`SELECT name,address,wishlist,location,destinations FROM users WHERE id=101`).Scan(&name, &address, &wishlist, &location, &encoded)).To(Succeed())
		var destinations []string
		Expect(json.Unmarshal([]byte(encoded), &destinations)).To(Succeed())
		Expect(name).To(Equal("One"))
		Expect(address).To(Equal("Address 1"))
		Expect(wishlist).To(Equal("Wish 1"))
		Expect(location).To(Equal("a"))
		Expect(destinations).To(Equal([]string{"a", "b"}))

		for sender, receiver := range map[int64]int64{101: 102, 102: 103, 103: 101} {
			var got int64
			Expect(target.QueryRow(`SELECT receiver FROM assignments WHERE game=10 AND sender=?`, sender).Scan(&got)).To(Succeed())
			Expect(got).To(Equal(receiver), "sender %d", sender)
		}
		var started int
		Expect(target.QueryRow(`SELECT started FROM games WHERE id=20`).Scan(&started)).To(Succeed())
		Expect(started).To(BeZero())
		var problems int
		Expect(target.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&problems)).To(Succeed())
		Expect(problems).To(BeZero())
	})

	ginkgo.It("rejects duplicate Telegram IDs", func() {
		source, target := openDB("old.db"), openDB("new.db")
		_, err := source.Exec(oldSchema + `INSERT INTO person(id,name,telegram_id) VALUES(1,'One',101),(2,'Two',101);`)
		Expect(err).NotTo(HaveOccurred())
		_, err = Migrate(context.Background(), source, target)
		Expect(err).To(MatchError(ContainSubstring("duplicate telegram_id")))
	})

	ginkgo.It("rejects a non-empty target", func() {
		source, target := openDB("old.db"), openDB("new.db")
		_, err := source.Exec(oldSchema)
		Expect(err).NotTo(HaveOccurred())
		_, err = target.Exec(`CREATE TABLE existing(id INTEGER)`)
		Expect(err).NotTo(HaveOccurred())
		_, err = Migrate(context.Background(), source, target)
		Expect(err).To(MatchError("target database is not empty"))
	})
})
