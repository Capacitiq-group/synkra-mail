// Package store: Postgres-backed data access for the mail schema.
// Tenancy rule (Section 2.3): every query is scoped by org resolved from a
// verified session — never from client input. Callers pass org/user ids from AuthContext.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// commandTag aliases pgconn.CommandTag for the Querier interface.
type commandTag = pgconn.CommandTag

var ErrNotFound = errors.New("not found")

// Store wraps the pool. All reads/writes go through here so tenancy and
// transactional rules stay in one place.
type Store struct {
	Pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{Pool: pool} }

// Tx aliases pgx.Tx for service-layer signatures.
type Tx = pgx.Tx

// WithTx runs fn in a transaction, rolling back on error.
func (s *Store) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func optUUID(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return *u
}

func optText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func fmtErr(op string, err error) error {
	return fmt.Errorf("%s: %w", op, err)
}
