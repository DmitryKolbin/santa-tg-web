package store

import (
	"context"
	"fmt"
)

// InsertUser and InsertGame write complete rows; they are used by the import
// from the old bot.
func (s *Store) InsertUser(ctx context.Context, user *User) error {
	_, err := s.db.NewInsert().Model(user).Exec(ctx)
	return err
}

func (s *Store) InsertGame(ctx context.Context, game *Game) error {
	_, err := s.db.NewInsert().Model(game).Exec(ctx)
	return err
}

// Verify checks foreign keys, table row counts and SQLite integrity.
func (s *Store) Verify(ctx context.Context, counts map[string]int) error {
	var violations []struct {
		Table  string `bun:"table"`
		RowID  int64  `bun:"rowid"`
		Parent string `bun:"parent"`
		FKID   int    `bun:"fkid"`
	}
	if err := s.db.NewRaw("PRAGMA foreign_key_check").Scan(ctx, &violations); err != nil {
		return err
	}
	if len(violations) > 0 {
		return fmt.Errorf("foreign key check failed")
	}
	for table, want := range counts {
		var got int
		if err := s.db.NewSelect().Table(table).ColumnExpr("count(*)").Scan(ctx, &got); err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s count is %d, want %d", table, got, want)
		}
	}
	var integrity string
	if err := s.db.NewRaw("PRAGMA integrity_check").Scan(ctx, &integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("integrity check failed: %s", integrity)
	}
	return nil
}
