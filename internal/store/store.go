package store

import (
	"context"

	"github.com/uptrace/bun"
)

// Store runs the application queries either directly on the database or
// inside a transaction started with Tx.
type Store struct {
	db   bun.IDB
	root *bun.DB
}

func NewStore(db *bun.DB) *Store { return &Store{db: db, root: db} }

func (s *Store) DB() *bun.DB { return s.root }

// Tx runs fn in a transaction. Inside a transaction it reuses the current one,
// because the application holds a single SQLite connection.
func (s *Store) Tx(ctx context.Context, fn func(tx *Store) error) error {
	if _, ok := s.db.(bun.Tx); ok {
		return fn(s)
	}
	return s.root.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(&Store{db: tx, root: s.root})
	})
}
