package database_test

import (
	"context"
	"path/filepath"
	"testing"

	appdb "github.com/DmitryKolbin/santa-tg-web/internal/database"
)

func TestMigrateEnablesForeignKeysAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "santa.db")
	for range 2 {
		sqldb, err := appdb.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		db := appdb.New(sqldb)
		if err := appdb.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}

		var enabled int
		if err := sqldb.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil {
			t.Fatal(err)
		}
		if enabled != 1 {
			t.Fatalf("foreign_keys = %d, want 1", enabled)
		}
		if err := sqldb.Close(); err != nil {
			t.Fatal(err)
		}
	}

	sqldb, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	db := appdb.New(sqldb)
	if err := appdb.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	_, err = sqldb.Exec("INSERT INTO members(game,person) VALUES(999,999)")
	if err == nil {
		t.Fatal("orphan member insert succeeded")
	}
}
