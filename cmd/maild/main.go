// maild: the all-in-one mail server binary (Stage-A single-VPS topology,
// Section 14.1). Runs API, inbound MX, submission, IMAP, the isolated tracker
// endpoint, and the outbound worker in one process, sharing one Postgres and
// one blob store. On the VPS, roles can be split across processes with -only.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"synkra.email/mail/internal/api"
	"synkra.email/mail/internal/authn"
	"synkra.email/mail/internal/blob"
	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/db"
	"synkra.email/mail/internal/events"
	"synkra.email/mail/internal/fsblob"
	"synkra.email/mail/internal/imapd"
	"synkra.email/mail/internal/ingest"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/outbound"
	"synkra.email/mail/internal/s3blob"
	"synkra.email/mail/internal/smtpin"
	"synkra.email/mail/internal/store"
	"synkra.email/mail/internal/trackingd"
)

func main() {
	seed := flag.Bool("seed", false, "bootstrap platform domain + admin user")
	only := flag.String("only", "", "restrict roles: api,smtp,submission,imap,worker,tracker")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Create the database if missing (no-op when it exists — prod-safe).
	if err := db.EnsureDatabase(ctx, cfg.DB.URL); err != nil {
		log.Printf("ensure database: %v", err)
	}

	pool, err := db.Connect(ctx, cfg.DB.URL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, "migrations"); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	st := store.New(pool)

	// Blob store: fs for dev/scratch, S3-compatible object storage for the
	// Stage-A VPS (design doc Sections 4.4 and 14.1). Selection is config,
	// not code: the endpoint/bucket/keys arrive via environment.
	var blobStore blob.Store
	switch cfg.BlobMode {
	case "s3":
		s3c, err := s3blob.New(cfg.S3)
		if err != nil {
			log.Fatalf("blob store: %v", err)
		}
		blobStore = s3c
		log.Printf("[blobs] s3 mode: bucket %s at %s", cfg.S3.Bucket, cfg.S3.Endpoint)
	default:
		blobs, err := fsblob.New(cfg.BlobDir)
		if err != nil {
			log.Fatalf("blob store: %v", err)
		}
		blobStore = blobs
	}

	hub := events.NewHub()
	go func() {
		if err := hub.Run(ctx, cfg.DB.URL); err != nil && ctx.Err() == nil {
			log.Printf("events hub: %v", err)
		}
	}()

	authSvc := &authn.Service{Store: st, Cfg: cfg}
	deps := &ingest.Deps{Store: st, Blobs: blobStore, Cfg: cfg}

	pipeline := &outbound.Pipeline{
		Store: st, Blobs: blobStore, Cfg: cfg, Deps: deps,
		TrackEnabled: func(ctx context.Context, orgID uuid.UUID) bool {
			org, err := store.GetOrg(ctx, st.Pool, orgID)
			return err == nil && org.TrackingEnabled
		},
		TrackerURL:  cfg.TrackerURL,
		TrackingKey: []byte(cfg.TrackingSecret),
	}

	if *seed {
		if err := bootstrap(ctx, cfg, st); err != nil {
			log.Fatalf("seed: %v", err)
		}
		log.Printf("seed complete (platform domain + admin if configured)")
		return
	}

	roles := map[string]bool{}
	if *only != "" {
		for _, r := range strings.Split(*only, ",") {
			roles[strings.TrimSpace(r)] = true
		}
	} else {
		for _, r := range []string{"api", "smtp", "submission", "imap", "worker", "tracker"} {
			roles[r] = true
		}
	}

	errCh := make(chan error, 8)

	if roles["api"] {
		apiSrv := &api.Server{Cfg: cfg, Auth: authSvc, Store: st, Pipeline: pipeline, Blobs: blobStore}
		httpSrv := &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           withMiddleware(apiSrv.Routes()),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() { errCh <- httpSrv.ListenAndServe() }()
		log.Printf("[api] listening on %s", cfg.HTTPAddr)
	}

	if roles["smtp"] {
		mx := smtpin.New(cfg, deps)
		ln, err := net.Listen("tcp", cfg.SMTPInAddr)
		if err != nil {
			log.Fatalf("mx listen: %v", err)
		}
		go func() { errCh <- mx.Serve(ln) }()
		log.Printf("[mx] inbound SMTP on %s", cfg.SMTPInAddr)
	}

	if roles["submission"] {
		subAuth := &smtpin.AuthAdapter{
			AuthenticateAppPassword: func(ctx context.Context, username, secret string) (string, string, error) {
				u, _, err := authSvc.AuthenticateAppPassword(ctx, username, secret)
				if err != nil {
					return "", "", err
				}
				return u.ID.String(), u.OrgID.String(), nil
			},
		}
		sub := smtpin.NewSubmission(cfg, deps, subAuth)
		sub.QueueSubmission = func(ctx context.Context, userID, orgID string, raw []byte, from string, rcpts []string) error {
			uid, _ := uuid.Parse(userID)
			oid, _ := uuid.Parse(orgID)
			return pipeline.QueueSubmission(ctx, uid, oid, raw, from, rcpts)
		}
		ln, err := net.Listen("tcp", cfg.SubmissionAddr)
		if err != nil {
			log.Fatalf("submission listen: %v", err)
		}
		go func() { errCh <- sub.Serve(ln) }()
		log.Printf("[submission] SMTP submission on %s", cfg.SubmissionAddr)
	}

	if roles["imap"] {
		imap := imapd.New(cfg, st, authSvc, blobStore, hub)
		go func() { errCh <- imap.ListenAndServe() }()
		log.Printf("[imap] listening on %s", cfg.IMAPAddr)
	}

	if roles["worker"] {
		var transport outbound.Transport = &outbound.LoopbackTransport{Deps: deps}
		if cfg.Env == "prod" {
			transport = &outbound.SMTPTransport{}
		}
		w := outbound.NewWorker(pipeline, transport)
		go w.Run(ctx)
		log.Printf("[worker] outbound worker started (transport=%s)",
			map[bool]string{true: "smtp", false: "loopback"}[cfg.Env == "prod"])
	}

	if roles["tracker"] {
		tracker := trackingd.New(cfg.HTTPAddr+"/api/v1/internal/tracking/events", []byte(cfg.TrackingSecret))
		httpT := &http.Server{
			Addr:              cfg.TrackerAddr,
			Handler:           tracker.Routes(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() { errCh <- httpT.ListenAndServe() }()
		log.Printf("[tracker] isolated tracking service on %s (core=%s)", cfg.TrackerAddr, cfg.HTTPAddr)
	}

	select {
	case <-ctx.Done():
		log.Printf("shutting down")
	case err := <-errCh:
		if err != nil {
			log.Fatalf("server error: %v", err)
		}
	}
}

// bootstrap seeds the platform domain and (if env allows) a platform admin.
func bootstrap(ctx context.Context, cfg *config.Config, st *store.Store) error {
	plan, err := store.GetPlanBySlug(ctx, st.Pool, "business")
	if err != nil {
		return fmt.Errorf("plan seed missing: %w", err)
	}
	return st.WithTx(ctx, func(tx store.Tx) error {
		if _, err := store.EnsurePlatformDomain(ctx, tx, cfg.PlatformDomain); err != nil {
			return err
		}
		email := os.Getenv("SYNKRA_ADMIN_EMAIL")
		password := os.Getenv("SYNKRA_ADMIN_PASSWORD")
		if email == "" || password == "" {
			return nil
		}
		// Platform staff live in a dedicated internal org (no customer mailboxes).
		var orgID uuid.UUID
		err := tx.QueryRow(ctx, `select id from mail.org where name='Platform' limit 1`).Scan(&orgID)
		if err != nil {
			orgID = uuid.New()
			if _, err := tx.Exec(ctx, `insert into mail.org (id, name, plan_id, review_status, sending_enabled)
                                values ($1,'Platform',$2,'approved',true)`, orgID, plan.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `insert into mail.subscription (id, org_id, plan_id, status)
                                values ($1,$2,$3,'active')`, uuid.New(), orgID, plan.ID); err != nil {
				return err
			}
		}
		u := &model.User{
			ID: uuid.New(), OrgID: orgID, Email: email,
			PasswordHash:    krypto.HashPassword(password),
			DisplayName:     "Platform Admin",
			Role:            "owner",
			IsPlatformAdmin: true,
		}
		if err := store.CreateUser(ctx, tx, u); err != nil {
			log.Printf("admin user (may already exist): %v", err)
		}
		return nil
	})
}

func withMiddleware(h http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Mount("/", h)
	return r
}
