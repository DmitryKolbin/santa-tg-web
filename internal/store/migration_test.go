package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFailedMigrationIsRolledBackAndNotMarkedApplied(t *testing.T) {
	originalSchema := schema
	schema = `CREATE TABLE partial(id INTEGER PRIMARY KEY); THIS IS NOT SQL;`
	defer func() { schema = originalSchema }()

	sqldb, err := Open(filepath.Join(t.TempDir(), "broken.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	db := New(sqldb)
	if err := Migrate(context.Background(), db); err == nil {
		t.Fatal("invalid migration succeeded")
	}

	var partialTables, appliedMigrations int
	if err := sqldb.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='partial'`).Scan(&partialTables); err != nil {
		t.Fatal(err)
	}
	if err := sqldb.QueryRow(`SELECT count(*) FROM bun_migrations`).Scan(&appliedMigrations); err != nil {
		t.Fatal(err)
	}
	if partialTables != 0 || appliedMigrations != 0 {
		t.Fatalf("partial tables = %d, applied migrations = %d; want both zero", partialTables, appliedMigrations)
	}
}
