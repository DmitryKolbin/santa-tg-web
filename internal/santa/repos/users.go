package repos

import (
	"context"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/projections"
	"github.com/uptrace/bun"
)

type UserRepo interface {
	Ensure(ctx context.Context, id int64, name string) error
	Get(ctx context.Context, id int64) (santa.User, error)
	Update(ctx context.Context, user *santa.User) error
	ActiveSenders(ctx context.Context, receiver int64) ([]projections.ActiveSender, error)
}

type userRepo struct{ db bun.IDB }

func NewUserRepo(db bun.IDB) UserRepo { return &userRepo{db: db} }

func (r *userRepo) Ensure(ctx context.Context, id int64, name string) error {
	u := &santa.User{ID: id, Name: name}
	_, err := r.db.NewInsert().Model(u).
		Column("id", "name").
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").Exec(ctx)
	return err
}

func (r *userRepo) Get(ctx context.Context, id int64) (santa.User, error) {
	var user santa.User
	err := r.db.NewSelect().Model(&user).Where("u.id = ?", id).Scan(ctx)
	return user, err
}

func (r *userRepo) Update(ctx context.Context, user *santa.User) error {
	_, err := r.db.NewUpdate().Model(user).
		Column("address", "wishlist", "location", "destinations").
		WherePK().Exec(ctx)
	return err
}

func (r *userRepo) ActiveSenders(ctx context.Context, receiver int64) ([]projections.ActiveSender, error) {
	var result []projections.ActiveSender
	err := r.db.NewSelect().TableExpr("assignments AS a").
		ColumnExpr("a.sender, g.name").
		Join("JOIN games AS g ON g.id = a.game AND g.round = a.round").
		Where("a.receiver = ? AND g.started = 1", receiver).Scan(ctx, &result)
	return result, err
}
