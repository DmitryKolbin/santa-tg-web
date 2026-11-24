package store

import "context"

func (s *Store) EnsureUser(ctx context.Context, id int64, name string) error {
	_, err := s.db.NewInsert().Model(&User{ID: id, Name: name}).
		Column("id", "name").
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").Exec(ctx)
	return err
}

func (s *Store) User(ctx context.Context, id int64) (User, error) {
	var user User
	err := s.db.NewSelect().Model(&user).Where("u.id = ?", id).Scan(ctx)
	return user, err
}

func (s *Store) UpdateProfile(ctx context.Context, user *User) error {
	_, err := s.db.NewUpdate().Model(user).
		Column("address", "wishlist", "location", "destinations").
		WherePK().Exec(ctx)
	return err
}

// ActiveSenders returns the santas of receiver in games with a running draw.
func (s *Store) ActiveSenders(ctx context.Context, receiver int64) ([]ActiveSender, error) {
	var result []ActiveSender
	err := s.db.NewSelect().TableExpr("assignments AS a").
		ColumnExpr("a.sender, g.name").
		Join("JOIN games AS g ON g.id = a.game AND g.round = a.round").
		Where("a.receiver = ? AND g.started = 1", receiver).Scan(ctx, &result)
	return result, err
}
