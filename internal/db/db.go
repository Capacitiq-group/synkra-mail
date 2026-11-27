// Package db: pgx pool + a tiny ordered SQL migration runner.
package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	// The pool is shared by API handlers, SMTP sessions, workers and the
	// janitor; the 20-connection ceiling is now tunable for the VPS size
	// (audit item 29).
	cfg.MaxConns = 20
	if v := os.Getenv("SYNKRA_MAIL_DB_MAX_CONNS"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n >= 4 {
			cfg.MaxConns = int32(n)
		}
	}
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("database ping: %w", err)
	}
	return pool, nil
}

// migrationLockKey is the advisory-lock id that serialises migrations across
// split-role start-ups (audit item 29: the api and worker processes racing to
// migrate the same database interleaved their DDL).
const migrationLockKey = 's'<<24 | 'y'<<16 | 'n'<<8 | 'k' // "synk"

// Migrate applies every *.sql in dir in lexical order, once each, inside a transaction,
// under a session-scoped advisory lock.
func Migrate(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire for migration lock: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("migration advisory lock: %w", err)
	}
	// Unlock on a detached context: a cancelled migration context must not
	// leak the session lock (it dies with the connection anyway, but the
	// explicit unlock keeps split-role restarts clean).
	unlockCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer conn.Exec(unlockCtx, `select pg_advisory_unlock($1)`, migrationLockKey)

	if _, err := conn.Exec(ctx, `
		create table if not exists schema_migrations (
			version text primary key,
			applied_at timestamptz not null default now()
		)`); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		var exists bool
		if err := pool.QueryRow(ctx, `select exists(select 1 from schema_migrations where version=$1)`, f).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sqlBytes, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		// Multi-statement scripts go through a simple protocol exec.
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", f, err)
		}
		if _, err := tx.Exec(ctx, `insert into schema_migrations(version) values($1)`, f); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s record: %w", f, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("migration %s commit: %w", f, err)
		}
	}
	return nil
}

// EnsureDatabase creates the database if it does not exist (dev convenience when
// pointing at a fresh embedded Postgres).
func EnsureDatabase(ctx context.Context, url string) error {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return err
	}
	dbname := cfg.Database
	if dbname == "" {
		return fmt.Errorf("database url has no database name")
	}
	cfg.Database = "postgres"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	var exists bool
	if err := conn.QueryRow(ctx, `select exists(select 1 from pg_database where datname=$1)`, dbname).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		_, err = conn.Exec(ctx, fmt.Sprintf(`create database %s`, quoteIdent(dbname)))
	}
	return err
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
