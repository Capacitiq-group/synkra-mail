// tracker: the isolated tracking service binary (Section 9.1). Deployable on
// its own host/port; holds no database credentials — events go to the mail
// core's HMAC-authenticated internal endpoint.
package main

import (
	"log"
	"net/http"
	"time"

	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/trackingd"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	svc := trackingd.New(cfg.HTTPAddr+"/api/v1/internal/tracking/events", []byte(cfg.TrackingSecret))
	srv := &http.Server{
		Addr:              cfg.TrackerAddr,
		Handler:           svc.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("[tracker] listening on %s (core internal=%s)", cfg.TrackerAddr, cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("tracker: %v", err)
	}
}
