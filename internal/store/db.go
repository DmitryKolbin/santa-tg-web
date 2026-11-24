package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
)

//go:embed schema.sql
var schema string

var migrations = migrate.NewMigrations()

func init() {
	migrations.Add(migrate.Migration{
		Name: "20261104000000",
		Up: func(ctx context.Context, db *bun.DB) error {
			return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
				_, err := tx.ExecContext(ctx, schema)
				return err
			})
		},
		Down: func(ctx context.Context, db *bun.DB) error { return nil },
	})
}

func New(sqldb *sql.DB) *bun.DB {
	sqldb.SetMaxOpenConns(1)
	return bun.NewDB(sqldb, sqlitedialect.New())
}

func Open(path string) (*sql.DB, error) {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	pragma := "_pragma=foreign_keys(1)"
	if sqliteshim.DriverName() == "sqlite3" {
		pragma = "_foreign_keys=on"
	}
	return sql.Open(sqliteshim.ShimName, path+separator+pragma)
}

func Migrate(ctx context.Context, db *bun.DB) error {
	// SQLite foreign key enforcement is connection-local and is reset whenever
	// the process reopens the database, so it must not live only in a migration.
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return err
	}
	var foreignKeys int
	if err := db.NewRaw("PRAGMA foreign_keys").Scan(ctx, &foreignKeys); err != nil {
		return err
	}
	if foreignKeys != 1 {
		return fmt.Errorf("sqlite foreign key enforcement is disabled")
	}
	m := migrate.NewMigrator(db, migrations, migrate.WithMarkAppliedOnSuccess(true))
	if err := m.Init(ctx); err != nil {
		return err
	}
	_, err := m.Migrate(ctx)
	return err
}

// ResetEmptyTarget restores an import target that was empty before migrations.
func ResetEmptyTarget(ctx context.Context, db *bun.DB) error {
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer db.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	for _, table := range []string{"message_reads", "deliveries", "messages", "assignments", "members", "games", "users", "bun_migration_locks", "bun_migrations"} {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			return fmt.Errorf("drop %s: %w", table, err)
		}
	}
	return nil
}
