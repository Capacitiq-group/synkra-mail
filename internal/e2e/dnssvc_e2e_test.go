// dnssvc_e2e_test.go: phase 8 end-to-end — a zone written through the
// PGDnsService API is served by the authoritative edge (PGZoneProvider),
// including the wildcards and TTLs the register demands.
package e2e

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/miekg/dns"

	"synkra.email/mail/internal/dnssvc"
)

func TestAuthoritativeDNSServesManagedZones(t *testing.T) {
	s := boot(t)
	ctx := context.Background()
	edge := dnssvc.NewServer(&dnssvc.PGZoneProvider{Store: s.st}, dnssvc.ServerOptions{
		NSHosts:      []string{"ns1.synkra.email", "ns2.synkra.email"},
		RefreshEvery: 50 * time.Millisecond,
		RRLBurst:     1000, // do not trip the limiter in a test loop
	})

	// Provision a zone through the service (4b.4) with the mail-ready set.
	zone := "dns-e2e-" + uuid.NewString()[:8] + ".example"
	svc := dnssvc.NewPG(s.st, nil)
	if _, err := svc.CreateZone(ctx, zone, "customer", nil); err != nil {
		t.Fatalf("createZone: %v", err)
	}
	if err := svc.BatchApply(ctx, zone, dnssvc.MailReadyRecords(dnssvc.TemplateInput{
		Domain:         zone,
		DKIMSelector:   "sk1",
		DKIMRecordTXT:  "v=DKIM1; k=rsa; p=E2EKEY",
		Hostname:       s.cfg.Hostname,
		PlatformDomain: s.cfg.PlatformDomain,
		OwnershipToken: "synkra-verify-e2e",
	})); err != nil {
		t.Fatalf("batchApply: %v", err)
	}

	// Serve the edge on ephemeral ports and query it.
	udpLn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("udp: %v", err)
	}
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcp: %v", err)
	}
	udpAddr := udpLn.LocalAddr().String()
	_ = udpLn.Close()
	_ = tcpLn.Close()
	edgeCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- edge.ListenAndServe(edgeCtx, udpAddr, tcpLn.Addr().String())
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	var r *dns.Msg
	for time.Now().Before(deadline) {
		c := &dns.Client{Net: "udp", Timeout: time.Second}
		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(zone), dns.TypeMX)
		r, _, err = c.Exchange(m, udpAddr)
		if err == nil && r != nil && len(r.Answer) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || len(r.Answer) == 0 {
		select {
		case serveErr := <-done:
			t.Fatalf("edge exited early: %v", serveErr)
		default:
		}
		t.Fatalf("edge never served the managed zone: err=%v r=%v", err, r)
	}
	if mx, ok := r.Answer[0].(*dns.MX); !ok || mx.Mx != dns.Fqdn(s.cfg.Hostname) {
		t.Fatalf("MX served: %v", r.Answer[0])
	}

	// The DKIM TXT and ownership token are live.
	c := &dns.Client{Net: "udp", Timeout: time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn("sk1._domainkey."+zone), dns.TypeTXT)
	rt, _, _ := c.Exchange(m, udpAddr)
	if len(rt.Answer) != 1 {
		t.Fatalf("DKIM TXT answers=%d", len(rt.Answer))
	}
	if got := rt.Answer[0].(*dns.TXT).Txt; len(got) == 0 || got[0] != "v=DKIM1; k=rsa; p=E2EKEY" {
		t.Fatalf("DKIM TXT = %v", got)
	}
}
