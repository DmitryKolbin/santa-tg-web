package repos

import (
	"context"

	"github.com/uptrace/bun"
)

type Store struct {
	db *bun.DB
}

type Repositories struct {
	Users    UserRepo
	Games    GameRepo
	Messages MessageRepo
	Import   ImportRepo
}

type UnitOfWork interface {
	InTx(context.Context, func(Repositories) error) error
}

func New(db *bun.DB) *Store  { return &Store{db: db} }
func (s *Store) DB() *bun.DB { return s.db }

func NewRepositories(db bun.IDB) Repositories {
	return Repositories{
		Users:    NewUserRepo(db),
		Games:    NewGameRepo(db),
		Messages: NewMessageRepo(db),
		Import:   NewImportRepo(db),
	}
}

func (s *Store) Repositories() Repositories { return NewRepositories(s.db) }

func (s *Store) InTx(ctx context.Context, fn func(Repositories) error) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(NewRepositories(tx))
	})
}
