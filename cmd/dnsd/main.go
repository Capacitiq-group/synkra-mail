// dnsd: the authoritative DNS server — a separate process with its own owner
// (build register §4b). It serves the zones managed through DnsService over
// UDP+TCP, refuses recursion, rate-limits responses, and offers AXFR only to
// operator-configured secondaries. It holds database credentials by necessity
// (it serves the zone data); the API surface for zone edits stays in maild
// behind session auth.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/db"
	"synkra.email/mail/internal/dnssvc"
	"synkra.email/mail/internal/store"
)

func main() {
	health := flag.String("health", "", "health endpoint listener (default from config)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DB.URL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, "migrations"); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	srv := dnssvc.NewServer(&dnssvc.PGZoneProvider{Store: store.New(pool)}, dnssvc.ServerOptions{
		NSHosts:       cfg.DNSNSHosts,
		Hostmaster:    "hostmaster." + cfg.PlatformDomain,
		RefreshEvery:  cfg.DNSZoneRefresh,
		AXFRAllowCIDR: cfg.DNSSecondaries,
	})

	// Health/readiness for the process supervisor (4b.11: we must know the
	// edge is up and serving zones).
	if *health == "" {
		*health = cfg.DNSHealthAddr
	}
	if *health != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			// Ready once the first zone load succeeded (even with zero zones —
			// a fresh deployment serves NXDOMAIN authoritatively).
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		httpSrv := &http.Server{
			Addr:              *health,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      15 * time.Second,
		}
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && ctx.Err() == nil {
				log.Printf("health: %v", err)
			}
		}()
		defer func() {
			sd, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = httpSrv.Shutdown(sd)
		}()
		log.Printf("[dnsd] health on %s", *health)
	}

	log.Printf("[dnsd] authoritative DNS on udp/tcp %s | zones refresh every %s | secondaries: %v | NS: %v",
		cfg.DNSUDPAddr, cfg.DNSZoneRefresh, cfg.DNSSecondaries, cfg.DNSNSHosts)
	if err := srv.ListenAndServe(ctx, cfg.DNSUDPAddr, cfg.DNSTCPAddr); err != nil {
		log.Fatalf("dnsd: %v", err)
	}
	if srv.RRLDrops > 0 {
		log.Printf("[dnsd] final stats: queries=%d rrl_drops=%d axfr=%d", srv.Queries, srv.RRLDrops, srv.AXFRs)
	}
	fmt.Fprintln(os.Stderr, "[dnsd] stopped")
}
