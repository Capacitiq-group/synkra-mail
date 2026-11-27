// seed-demo — provisions a demo tenant and injects real mail so the web UI
// has something to render. Dev/demo tool only; never runs in production.
//
// Usage: SYNKRA_MAIL_PLATFORM_DOMAIN=acme.co.za go run ./cmd/seed-demo
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"synkra.email/mail/internal/authn"
	"synkra.email/mail/internal/blob"
	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/db"
	"synkra.email/mail/internal/fsblob"
	"synkra.email/mail/internal/ingest"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/store"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	cfg.Env = "dev"
	cfg.BlobDir = "/home/z/my-project/devdata/blobs"
	_ = os.MkdirAll(cfg.BlobDir, 0o700)

	if err := db.EnsureDatabase(ctx, cfg.DB.URL); err != nil {
		log.Fatalf("ensure db: %v", err)
	}
	pool, err := db.Connect(ctx, cfg.DB.URL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	if err := db.Migrate(ctx, pool, "migrations"); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	st := store.New(pool)
	blobs, err := fsblob.New(cfg.BlobDir)
	if err != nil {
		log.Fatalf("blobs: %v", err)
	}
	var blobStore blob.Store = blobs
	deps := &ingest.Deps{Store: st, Blobs: blobStore, Cfg: cfg}
	auth := &authn.Service{Store: st, Cfg: cfg}

	// Idempotent: if the demo owner already exists, skip provisioning.
	login := "thabo@acme.co.za"
	password := "Demo!Passw0rd"
	if _, _, err := auth.Login(ctx, login, password, "127.0.0.1", "seed", ""); err == nil {
		fmt.Println("demo tenant already provisioned — delivering mail only")
	} else {
		plan, err := store.GetPlanBySlug(ctx, st.Pool, "business")
		if err != nil {
			log.Fatalf("plan: %v", err)
		}
		if err := st.WithTx(ctx, func(tx store.Tx) error {
			_, err := store.EnsurePlatformDomain(ctx, tx, cfg.PlatformDomain)
			return err
		}); err != nil {
			log.Fatalf("platform domain: %v", err)
		}
		org, user, err := auth.ProvisionOrg(ctx, "Acme (Pty) Ltd", login, "Thabo Mokoena",
			krypto.HashPassword(password), cfg.PlatformDomain, plan)
		if err != nil {
			log.Fatalf("provision: %v", err)
		}
		fmt.Printf("provisioned org=%s user=%s\n", org.Name, user.Email)
	}

	time.Sleep(300 * time.Millisecond) // let the provision tx settle

	// Demo privilege: approve the tenant so the UI can exercise the send path.
	// (Real tenants flip this through plan payment / staff approval.)
	if _, err := pool.Exec(ctx, `update mail.org set status='active', review_status='approved', sending_enabled=true where name='Acme (Pty) Ltd'`); err != nil {
		log.Fatalf("approve org: %v", err)
	}
	if _, err := pool.Exec(ctx, `update mail.mailbox mb set send_status='active' from mail.org o where mb.org_id=o.id and o.name='Acme (Pty) Ltd'`); err != nil {
		log.Fatalf("approve mailbox: %v", err)
	}

	messages := []struct{ from, subject, body string }{
		{"Annelie de Wet <annelie@client.co.za>", "Re: March invoice — PO-1142",
			"Hi Thabo,\n\nThe signed purchase order is attached (PO-1142). Please confirm the March invoice matches the PO line items before Friday.\n\nKind regards,\nAnnelie"},
		{"SARS eFiling <noreply@sars.gov.za>", "Your Notice of Original Assessment is available",
			"Dear Taxpayer,\n\nYour Notice of Original Assessment for the 2026 tax period is now available on eFiling.\n\nThis is an automated message. Do not reply."},
		{"Vusi Ndlovu <vusi@acme.co.za>", "Standup notes — deliverability review",
			"Morning team,\n\nNotes from this morning: delivery rate steady at 97.2%, two deferred batches to outlook.com retried successfully.\n\nVusi"},
		{"Cape Town Runners <news@capetownrunners.co.za>", "This weekend: Two Oceans training run",
			"See this weekend's training schedule and the latest club news. Unsubscribe any time via the link below.\n\n— CTR committee"},
		{"FNB Security Team <security@fnb-secure.co.za>", "Urgent: verify your business banking profile",
			"Dear customer,\n\nYour business profile requires verification within 24 hours. Click the secure link to confirm your details.\n\nFNB Security"},
	}
	for i, m := range messages {
		raw := []byte("From: " + m.from + "\r\n" +
			"To: Thabo Mokoena <" + login + ">\r\n" +
			"Subject: " + m.subject + "\r\n" +
			"Date: " + time.Now().Add(-time.Duration(len(messages)-i)*17*time.Minute).Format(time.RFC1123Z) + "\r\n" +
			"Message-ID: <seed-" + fmt.Sprint(time.Now().UnixNano()) + "-" + fmt.Sprint(i) + "@seed.acme.co.za>\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n" +
			"\r\n" + m.body + "\r\n")
		envFrom := "bounces@" + domainOf(m.from)
		if err := ingest.DeliverLoopback(ctx, deps, raw, envFrom, login); err != nil {
			log.Printf("deliver %q: %v", m.subject, err)
		} else {
			fmt.Printf("delivered: %s\n", m.subject)
		}
	}

	// One message with an attachment (multipart, small fake PDF).
	att := "JVBERi0xLjQKJcOkw7zDtsOfCjEgMCBvYmoKPDwvVHlwZS9DYXRhbG9nL1BhZ2VzIDIgMCBSPj4KZW5kb2JqCg=="
	raw := []byte("From: Annelie de Wet <annelie@client.co.za>\r\n" +
		"To: Thabo Mokoena <" + login + ">\r\n" +
		"Subject: Signed PO-1142 (attachment)\r\n" +
		"Date: " + time.Now().Add(-8*time.Minute).Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Message-ID: <seed-att-" + fmt.Sprint(time.Now().UnixNano()) + "@client.co.za>\r\n" +
		"Content-Type: multipart/mixed; boundary=\"seedb\"\r\n" +
		"\r\n--seedb\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"Thabo,\n\nAttached is the signed PO as requested.\n\nAnnelie\r\n" +
		"--seedb\r\n" +
		"Content-Type: application/pdf; name=\"PO-1142.pdf\"\r\n" +
		"Content-Disposition: attachment; filename=\"PO-1142.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" + att + "\r\n" +
		"--seedb--\r\n")
	if err := ingest.DeliverLoopback(ctx, deps, raw, "bounces@client.co.za", login); err != nil {
		log.Printf("deliver attachment: %v", err)
	} else {
		fmt.Println("delivered: Signed PO-1142 (attachment)")
	}

	fmt.Printf("\nLOGIN: %s / %s\nAPI: %s\n", login, password, cfg.HTTPAddr)
	_ = http.DefaultClient // silence unused import in constrained builds
}

func domainOf(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == '@' {
			return addr[i+1:]
		}
	}
	return "acme.co.za"
}
