package repos

import (
	"context"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/projections"
	"github.com/uptrace/bun"
)

type GameRepo interface {
	Get(context.Context, int64, int64) (projections.GameDetails, error)
	GetByInvite(context.Context, int64, string) (projections.GameDetails, error)
	List(context.Context, int64) ([]projections.GameDetails, error)
	Create(context.Context, *santa.Game) error
	Update(context.Context, *santa.Game, ...string) error
	AddMember(context.Context, int64, int64) error
	MemberIDs(context.Context, int64) ([]int64, error)
	CreateAssignment(context.Context, *santa.Assignment) error
	Receiver(context.Context, int64, int, int64) (int64, error)
}

type gameRepo struct{ db bun.IDB }

func NewGameRepo(db bun.IDB) GameRepo { return &gameRepo{db: db} }

func gameViewQuery(db bun.IDB, user int64) *bun.SelectQuery {
	return db.NewSelect().TableExpr("games AS g").
		ColumnExpr("g.*").ColumnExpr("u.name AS creator_name").
		ColumnExpr("EXISTS(SELECT 1 FROM members WHERE game=g.id AND person=?) AS is_member", user).
		ColumnExpr("(SELECT count(*) FROM members WHERE game=g.id) AS member_count").
		Join("JOIN users AS u ON u.id=g.creator")
}

func (r *gameRepo) Get(ctx context.Context, user, id int64) (projections.GameDetails, error) {
	var result projections.GameDetails
	err := gameViewQuery(r.db, user).Where("g.id = ?", id).Scan(ctx, &result)
	return result, err
}

func (r *gameRepo) GetByInvite(ctx context.Context, user int64, invite string) (projections.GameDetails, error) {
	var result projections.GameDetails
	err := gameViewQuery(r.db, user).Where("g.invite = ?", invite).Scan(ctx, &result)
	return result, err
}

func (r *gameRepo) List(ctx context.Context, user int64) ([]projections.GameDetails, error) {
	var result []projections.GameDetails
	err := gameViewQuery(r.db, user).
		Where("g.creator=? OR EXISTS(SELECT 1 FROM members WHERE game=g.id AND person=?)", user, user).
		OrderExpr("g.id DESC").Scan(ctx, &result)
	return result, err
}

func (r *gameRepo) Create(ctx context.Context, game *santa.Game) error {
	_, err := r.db.NewInsert().Model(game).Exec(ctx)
	return err
}

func (r *gameRepo) Update(ctx context.Context, game *santa.Game, columns ...string) error {
	query := r.db.NewUpdate().Model(game).WherePK()
	if len(columns) > 0 {
		query.Column(columns...)
	}
	_, err := query.Exec(ctx)
	return err
}

func (r *gameRepo) AddMember(ctx context.Context, game, person int64) error {
	_, err := r.db.NewInsert().Model(&santa.Member{GameID: game, PersonID: person}).Ignore().Exec(ctx)
	return err
}

func (r *gameRepo) MemberIDs(ctx context.Context, game int64) ([]int64, error) {
	var ids []int64
	err := r.db.NewSelect().Model((*santa.Member)(nil)).Column("person").Where("game = ?", game).OrderExpr("person").Scan(ctx, &ids)
	return ids, err
}

func (r *gameRepo) CreateAssignment(ctx context.Context, assignment *santa.Assignment) error {
	_, err := r.db.NewInsert().Model(assignment).Exec(ctx)
	return err
}

func (r *gameRepo) Receiver(ctx context.Context, game int64, round int, sender int64) (int64, error) {
	var receiver int64
	err := r.db.NewSelect().Model((*santa.Assignment)(nil)).Column("receiver").
		Where("game=? AND round=? AND sender=?", game, round, sender).Scan(ctx, &receiver)
	return receiver, err
}
