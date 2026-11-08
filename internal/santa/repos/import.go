package repos

import (
	"context"
	"fmt"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/uptrace/bun"
)

type ImportRepo interface {
	User(context.Context, *santa.User) error
	Game(context.Context, *santa.Game) error
	Verify(context.Context, map[string]int) error
}

type importRepo struct{ db bun.IDB }

func NewImportRepo(db bun.IDB) ImportRepo { return &importRepo{db: db} }

func (r *importRepo) User(ctx context.Context, user *santa.User) error {
	_, err := r.db.NewInsert().Model(user).Exec(ctx)
	return err
}

func (r *importRepo) Game(ctx context.Context, game *santa.Game) error {
	_, err := r.db.NewInsert().Model(game).Exec(ctx)
	return err
}

func (r *importRepo) Verify(ctx context.Context, counts map[string]int) error {
	var violations []struct {
		Table  string `bun:"table"`
		RowID  int64  `bun:"rowid"`
		Parent string `bun:"parent"`
		FKID   int    `bun:"fkid"`
	}
	if err := r.db.NewRaw("PRAGMA foreign_key_check").Scan(ctx, &violations); err != nil {
		return err
	}
	if len(violations) > 0 {
		return fmt.Errorf("foreign key check failed")
	}
	for table, want := range counts {
		var got int
		if err := r.db.NewSelect().Table(table).ColumnExpr("count(*)").Scan(ctx, &got); err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s count is %d, want %d", table, got, want)
		}
	}
	var integrity string
	if err := r.db.NewRaw("PRAGMA integrity_check").Scan(ctx, &integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("integrity check failed: %s", integrity)
	}
	return nil
}
