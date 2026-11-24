package store

import (
	"context"

	"github.com/uptrace/bun"
)

func (s *Store) gameViews(user int64) *bun.SelectQuery {
	return s.db.NewSelect().TableExpr("games AS g").
		ColumnExpr("g.*").ColumnExpr("u.name AS creator_name").
		ColumnExpr("EXISTS(SELECT 1 FROM members WHERE game=g.id AND person=?) AS is_member", user).
		ColumnExpr("(SELECT count(*) FROM members WHERE game=g.id) AS member_count").
		Join("JOIN users AS u ON u.id=g.creator")
}

func (s *Store) GameView(ctx context.Context, user, id int64) (GameView, error) {
	var result GameView
	err := s.gameViews(user).Where("g.id = ?", id).Scan(ctx, &result)
	return result, err
}

func (s *Store) GameViewByInvite(ctx context.Context, user int64, invite string) (GameView, error) {
	var result GameView
	err := s.gameViews(user).Where("g.invite = ?", invite).Scan(ctx, &result)
	return result, err
}

func (s *Store) GameViews(ctx context.Context, user int64) ([]GameView, error) {
	var result []GameView
	err := s.gameViews(user).
		Where("g.creator=? OR EXISTS(SELECT 1 FROM members WHERE game=g.id AND person=?)", user, user).
		OrderExpr("g.id DESC").Scan(ctx, &result)
	return result, err
}

func (s *Store) CreateGame(ctx context.Context, game *Game) error {
	_, err := s.db.NewInsert().Model(game).Exec(ctx)
	return err
}

// UpdateGame writes the given columns, or the whole row when none are given.
func (s *Store) UpdateGame(ctx context.Context, game *Game, columns ...string) error {
	query := s.db.NewUpdate().Model(game).WherePK()
	if len(columns) > 0 {
		query.Column(columns...)
	}
	_, err := query.Exec(ctx)
	return err
}

func (s *Store) AddMember(ctx context.Context, game, person int64) error {
	_, err := s.db.NewInsert().Model(&Member{GameID: game, PersonID: person}).Ignore().Exec(ctx)
	return err
}

func (s *Store) MemberIDs(ctx context.Context, game int64) ([]int64, error) {
	var ids []int64
	err := s.db.NewSelect().Model((*Member)(nil)).Column("person").Where("game = ?", game).OrderExpr("person").Scan(ctx, &ids)
	return ids, err
}

func (s *Store) IsMember(ctx context.Context, game, person int64) (bool, error) {
	return s.db.NewSelect().Model((*Member)(nil)).Where("game = ? AND person = ?", game, person).Exists(ctx)
}

// RemoveMember deletes the membership and every blocked pair that mentions the person.
func (s *Store) RemoveMember(ctx context.Context, game, person int64) error {
	if _, err := s.db.NewDelete().Model((*BlockedPair)(nil)).Where("game = ? AND (sender = ? OR receiver = ?)", game, person, person).Exec(ctx); err != nil {
		return err
	}
	_, err := s.db.NewDelete().Model((*Member)(nil)).Where("game = ? AND person = ?", game, person).Exec(ctx)
	return err
}

func (s *Store) BlockedPairs(ctx context.Context, game int64) ([]BlockedPair, error) {
	pairs := []BlockedPair{}
	err := s.db.NewSelect().Model(&pairs).Where("game = ?", game).OrderExpr("sender, receiver").Scan(ctx)
	return pairs, err
}

func (s *Store) AddBlockedPair(ctx context.Context, pair *BlockedPair) error {
	_, err := s.db.NewInsert().Model(pair).Ignore().Exec(ctx)
	return err
}

func (s *Store) DeleteBlockedPair(ctx context.Context, pair *BlockedPair) error {
	_, err := s.db.NewDelete().Model(pair).WherePK().Exec(ctx)
	return err
}

func (s *Store) CreateAssignment(ctx context.Context, assignment *Assignment) error {
	_, err := s.db.NewInsert().Model(assignment).Exec(ctx)
	return err
}

func (s *Store) ReceiverOf(ctx context.Context, game int64, round int, sender int64) (int64, error) {
	var receiver int64
	err := s.db.NewSelect().Model((*Assignment)(nil)).Column("receiver").
		Where("game=? AND round=? AND sender=?", game, round, sender).Scan(ctx, &receiver)
	return receiver, err
}
