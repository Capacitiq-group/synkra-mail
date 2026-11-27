// serve_test.go: the authoritative DNS edge against an in-memory provider —
// real UDP/TCP round trips with the miekg client.
package dnssvc

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

type memProvider struct{ zones []Zone }

func (m *memProvider) Zones(ctx context.Context) ([]Zone, error) { return m.zones, nil }

func testZone() Zone {
	return Zone{
		Origin: "customer.co.za",
		Serial: 2026112701,
		Records: []Record{
			{Name: "@", RRType: "A", RData: "203.0.113.10", TTL: 300},
			{Name: "@", RRType: "MX", RData: "10 mx1.synkra.co.za", TTL: 300},
			{Name: "@", RRType: "TXT", RData: "v=spf1 include:spf.synkra.email ~all", TTL: 300},
			{Name: "sk1._domainkey", RRType: "TXT", RData: "v=DKIM1; k=rsa; p=TESTKEY", TTL: 300},
			{Name: "*", RRType: "A", RData: "203.0.113.99", TTL: 300},
		},
	}
}

func startEdge(t *testing.T, prov ZoneProvider, opts ServerOptions) (*Server, string, string) {
	t.Helper()
	udpLn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("udp listen: %v", err)
	}
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcp listen: %v", err)
	}
	udpAddr := udpLn.LocalAddr().String()
	tcpAddr := tcpLn.Addr().String()
	_ = udpLn.Close()
	_ = tcpLn.Close()

	s := NewServer(prov, opts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = s.ListenAndServe(ctx, udpAddr, tcpAddr)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	// Wait for readiness.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c := new(dns.Client)
		m := new(dns.Msg)
		m.SetQuestion("customer.co.za.", dns.TypeSOA)
		if r, _, err := c.Exchange(m, udpAddr); err == nil && len(r.Answer) > 0 {
			return s, udpAddr, tcpAddr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("edge did not become ready")
	return nil, "", ""
}

func query(t *testing.T, addr, name string, qtype uint16, proto string) *dns.Msg {
	t.Helper()
	c := &dns.Client{Net: proto, Timeout: 3 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	r, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query %s/%d (%s): %v", name, qtype, proto, err)
	}
	return r
}

func TestAuthoritativeAnswers(t *testing.T) {
	// Two zones: one with an apex wildcard (covers every name below it, so
	// NXDOMAIN is impossible there — RFC 1034 4.3.3) and one without, for the
	// negative-answer cases.
	_, udp, _ := startEdge(t, &memProvider{zones: []Zone{testZone(),
		{Origin: "plain.example", Serial: 7, Records: []Record{{Name: "@", RRType: "A", RData: "203.0.113.1"}}}}},
		ServerOptions{})
	_ = udp

	// A at apex.
	r := query(t, udp, "customer.co.za", dns.TypeA, "udp")
	if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 1 {
		t.Fatalf("apex A: rcode=%d answers=%d", r.Rcode, len(r.Answer))
	}
	if a, ok := r.Answer[0].(*dns.A); !ok || a.A.String() != "203.0.113.10" {
		t.Fatalf("apex A rdata: %v", r.Answer[0])
	}
	if !r.Authoritative {
		t.Fatal("answers must carry the AA bit")
	}
	if r.RecursionAvailable {
		t.Fatal("an authoritative edge must never offer recursion (4b.8)")
	}

	// MX with the SOA in authority.
	r = query(t, udp, "customer.co.za", dns.TypeMX, "udp")
	if len(r.Answer) != 1 || r.Ns == nil {
		t.Fatalf("MX answers=%d authority=%d", len(r.Answer), len(r.Ns))
	}
	if mx, ok := r.Answer[0].(*dns.MX); !ok || mx.Mx != "mx1.synkra.co.za." || mx.Preference != 10 {
		t.Fatalf("MX rdata: %v", r.Answer[0])
	}

	// TXT with special characters survives quoting.
	r = query(t, udp, "sk1._domainkey.customer.co.za", dns.TypeTXT, "udp")
	if len(r.Answer) != 1 {
		t.Fatalf("DKIM TXT answers=%d", len(r.Answer))
	}
	got := strings.Join(r.Answer[0].(*dns.TXT).Txt, "")
	if got != "v=DKIM1; k=rsa; p=TESTKEY" {
		t.Fatalf("DKIM TXT = %q", got)
	}

	// SOA synthesised with serial + NS set.
	r = query(t, udp, "customer.co.za", dns.TypeSOA, "udp")
	soa, ok := r.Answer[0].(*dns.SOA)
	if !ok || soa.Serial != 2026112701 {
		t.Fatalf("SOA: %v", r.Answer[0])
	}
	if soa.Ns != "ns1.synkra.email." || soa.Mbox != "hostmaster.synkra.email." {
		t.Fatalf("SOA identity: ns=%q mbox=%q", soa.Ns, soa.Mbox)
	}

	// Wildcard: a.b.customer.co.za synthesises the queried name.
	r = query(t, udp, "a.b.customer.co.za", dns.TypeA, "udp")
	if len(r.Answer) != 1 || r.Answer[0].Header().Name != "a.b.customer.co.za." {
		t.Fatalf("wildcard: %v", r.Answer)
	}
	if a := r.Answer[0].(*dns.A); a.A.String() != "203.0.113.99" {
		t.Fatalf("wildcard rdata: %v", r.Answer[0])
	}

	// NXDOMAIN carries the SOA (negative caching) — in the zone WITHOUT a
	// wildcard; the wildcard zone above covers every name below its apex.
	r = query(t, udp, "nothere.plain.example", dns.TypeA, "udp")
	if r.Rcode != dns.RcodeNameError || len(r.Ns) != 1 {
		t.Fatalf("nxdomain: rcode=%d authority=%d", r.Rcode, len(r.Ns))
	}

	// NODATA: name exists (apex), type absent.
	r = query(t, udp, "plain.example", dns.TypeSRV, "udp")
	if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 0 || len(r.Ns) != 1 {
		t.Fatalf("nodata: rcode=%d answers=%d authority=%d", r.Rcode, len(r.Answer), len(r.Ns))
	}

	// A foreign zone is REFUSED — an authoritative edge does not guess.
	r = query(t, udp, "example.org", dns.TypeA, "udp")
	if r.Rcode != dns.RcodeRefused {
		t.Fatalf("foreign zone rcode = %d, want REFUSED", r.Rcode)
	}
}

func TestAXFRDeniedByDefaultThenACL(t *testing.T) {
	// No ACL → refused (4b.7: deny by default).
	_, _, tcp := startEdge(t, &memProvider{zones: []Zone{testZone()}}, ServerOptions{})
	r := query(t, tcp, "customer.co.za", dns.TypeAXFR, "tcp")
	if r.Rcode != dns.RcodeRefused || len(r.Answer) != 0 {
		t.Fatalf("AXFR without ACL: rcode=%d answers=%d", r.Rcode, len(r.Answer))
	}

	// With the ACL (127.0.0.0/8) the transfer streams SOA…records…SOA.
	_, _, tcp = startEdge(t, &memProvider{zones: []Zone{testZone()}},
		ServerOptions{AXFRAllowCIDR: []string{"127.0.0.0/8"}})
	r = query(t, tcp, "customer.co.za", dns.TypeAXFR, "tcp")
	if r.Rcode != dns.RcodeSuccess || len(r.Answer) < 5 {
		t.Fatalf("AXFR with ACL: rcode=%d answers=%d", r.Rcode, len(r.Answer))
	}
	first, ok := r.Answer[0].(*dns.SOA)
	if !ok {
		t.Fatal("AXFR must start with the SOA")
	}
	if _, ok := r.Answer[len(r.Answer)-1].(*dns.SOA); !ok {
		t.Fatal("AXFR must end with the SOA")
	}
	_ = first
}

func TestRRLDropsFlood(t *testing.T) {
	s, udp, _ := startEdge(t, &memProvider{zones: []Zone{testZone()}},
		ServerOptions{RRLBurst: 5, RRLRefill: 1})
	dropped := 0
	for i := 0; i < 60; i++ {
		c := &dns.Client{Net: "udp", Timeout: 100 * time.Millisecond}
		m := new(dns.Msg)
		m.SetQuestion("customer.co.za.", dns.TypeA)
		if _, _, err := c.Exchange(m, udp); err != nil {
			dropped++
		}
	}
	if dropped == 0 || s.RRLDrops == 0 {
		t.Fatalf("RRL must drop the flood: client-side drops=%d server drops=%d", dropped, s.RRLDrops)
	}
}
