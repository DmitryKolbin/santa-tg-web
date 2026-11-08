package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/migrate"
)

//go:embed schema.sql
var schema string

var migrations = migrate.NewMigrations()

func init() {
	migrations.Add(migrate.Migration{
		Name: "20261104000000",
		Up: func(ctx context.Context, db *bun.DB) error {
			_, err := db.ExecContext(ctx, schema)
			return err
		},
		Down: func(ctx context.Context, db *bun.DB) error { return nil },
	})
}

func New(sqldb *sql.DB) *bun.DB {
	sqldb.SetMaxOpenConns(1)
	return bun.NewDB(sqldb, sqlitedialect.New())
}

func Migrate(ctx context.Context, db *bun.DB) error {
	m := migrate.NewMigrator(db, migrations)
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
