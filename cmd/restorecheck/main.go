// Command restorecheck — build register 3.5: a restore you have not tested is
// not a backup. It dumps every mail table (COPY TO), rebuilds the schema from
// the migrations in a scratch database, restores the data (COPY FROM), and
// verifies row counts and a write round-trip.
//
// Usage (from the repository root):
//
//	SYNKRA_MAIL_RESTORECHECK_URL=postgres://synkra@127.0.0.1:5433/synkra_mail_test?sslmode=disable \
//	  go run ./cmd/restorecheck
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"synkra.email/mail/internal/db"
)

// Foreign-key-safe order: referenced tables first.
var tables = []string{
	"plan", "org", "subscription", "user", "session", "app_password",
	"domain", "mailbox", "address", "mailbox_member", "folder", "blob",
	"thread", "message", "mailbox_item", "attachment", "delivery_job", "dsn",
	"suppression", "tracking_pixel", "open_event", "tracked_link", "click_event",
	"quarantine", "quota_event", "audit_event", "rate_event", "storage_ledger",
	"login_attempt", "idempotency",
}

func main() {
	src := os.Getenv("SYNKRA_MAIL_RESTORECHECK_URL")
	if src == "" {
		src = "postgres://synkra@127.0.0.1:5433/synkra_mail_test?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx, src); err != nil {
		fmt.Fprintf(os.Stderr, "restorecheck: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("== RESULT restore verified: dump, restore and counts all agree")
}

func run(ctx context.Context, srcURL string) error {
	cfg, err := pgxpool.ParseConfig(srcURL)
	if err != nil {
		return fmt.Errorf("parse source url: %w", err)
	}
	scratch := cfg.ConnConfig.Database + "_restorecheck"
	adminURL := replaceDB(srcURL, "postgres")
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("admin connect: %w", err)
	}
	defer admin.Close()
	_, _ = admin.Exec(ctx, fmt.Sprintf(
		`select pg_terminate_backend(pid) from pg_stat_activity where datname=%s`, literal(scratch)))
	if _, err := admin.Exec(ctx, fmt.Sprintf(`drop database if exists %s`, pgx.Identifier{scratch}.Sanitize())); err != nil {
		return fmt.Errorf("drop scratch: %w", err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf(`create database %s`, pgx.Identifier{scratch}.Sanitize())); err != nil {
		return fmt.Errorf("create scratch: %w", err)
	}
	defer func() {
		_, _ = admin.Exec(ctx, fmt.Sprintf(`drop database if exists %s`, pgx.Identifier{scratch}.Sanitize()))
	}()

	spool, err := pgxpool.New(ctx, srcURL)
	if err != nil {
		return fmt.Errorf("source connect: %w", err)
	}
	defer spool.Close()

	dpool, err := pgxpool.New(ctx, replaceDB(srcURL, scratch))
	if err != nil {
		return fmt.Errorf("scratch connect: %w", err)
	}
	defer dpool.Close()

	fmt.Println("== 1. schema from migrations")
	if err := db.Migrate(ctx, dpool, "migrations"); err != nil {
		return fmt.Errorf("migrate scratch: %w", err)
	}
	// The migrations seed rows (the plan table); clear everything so the
	// restored data is the only data.
	trunc := "truncate "
	for i, t := range tables {
		if i > 0 {
			trunc += ", "
		}
		trunc += "mail." + pgx.Identifier{t}.Sanitize()
	}
	if _, err := dpool.Exec(ctx, trunc+" restart identity cascade"); err != nil {
		return fmt.Errorf("truncate scratch: %w", err)
	}

	sconn, err := spool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer sconn.Release()
	dconn, err := dpool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer dconn.Release()

	fail := false
	fmt.Println("== 2. copy each table source → scratch")
	for _, t := range tables {
		var srcBuf byteBuf
		if _, err := sconn.Conn().PgConn().CopyTo(ctx, &srcBuf, fmt.Sprintf(`copy mail.%s to stdout (format text)`, pgx.Identifier{t}.Sanitize())); err != nil {
			return fmt.Errorf("dump %s: %w", t, err)
		}
		nSrc := countLines(srcBuf.b)
		if _, err := dconn.Conn().PgConn().CopyFrom(ctx, &srcBuf, fmt.Sprintf(`copy mail.%s from stdin (format text)`, pgx.Identifier{t}.Sanitize())); err != nil {
			return fmt.Errorf("restore %s: %w", t, err)
		}
		var nDst int
		if err := dpool.QueryRow(ctx, fmt.Sprintf(`select count(*) from mail.%s`, pgx.Identifier{t}.Sanitize())).Scan(&nDst); err != nil {
			return fmt.Errorf("count %s: %w", t, err)
		}
		status := "ok"
		if int64(nDst) != nSrc {
			status = "MISMATCH"
			fail = true
		}
		fmt.Printf("   %-14s src=%-8d restored=%-8d %s\n", t, nSrc, nDst, status)
	}

	fmt.Println("== 3. write probe on the restored schema")
	var one bool
	if err := dpool.QueryRow(ctx,
		`insert into mail.login_attempt (email, ip, ok) values ('restore-probe@x.test', '', true) returning true`).Scan(&one); err != nil {
		return fmt.Errorf("write probe: %w", err)
	}
	if !one {
		fail = true
	}
	if fail {
		return fmt.Errorf("row counts disagree — the restore drill FAILED")
	}
	return nil
}

// byteBuf is an io.ReadWriter over one buffer: CopyTo writes it, CopyFrom
// reads it back.
type byteBuf struct{ b []byte }

func (w *byteBuf) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }
func (w *byteBuf) Read(p []byte) (int, error) {
	if len(w.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, w.b)
	w.b = w.b[n:]
	return n, nil
}

func countLines(b []byte) int64 {
	n := int64(0)
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

func replaceDB(url, db string) string {
	// url is postgres://user@host:port/db?opts — swap the database segment.
	for i := len(url) - 1; i >= 0; i-- {
		if url[i] == '/' {
			return url[:i+1] + db
		}
	}
	return url
}

func literal(s string) string { return "'" + s + "'" }
