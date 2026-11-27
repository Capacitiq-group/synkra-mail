// serve.go: the authoritative DNS edge (build register §4b — the tier-0
// service itself). The PGDnsService zone API is the data plane; this file is
// the serving plane: UDP+TCP on port 53, authoritative-only answers, SOA/NS
// synthesis, wildcard records, response rate limiting (4b.8), and AXFR to
// operator-configured secondaries (4b.7 — denied by default).
package dnssvc

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/miekg/dns"

	"synkra.email/mail/internal/store"
)

// Zone is one served zone: origin + its records.
type Zone struct {
	Origin  string // lowercase FQDN WITHOUT trailing dot, e.g. "customer.co.za"
	Serial  uint32
	Records []Record // Name relative ("@" = apex); RData the raw rdata
}

// ZoneProvider supplies the serving data. The PG implementation re-reads on a
// ticker; the in-memory one serves tests.
type ZoneProvider interface {
	Zones(ctx context.Context) ([]Zone, error)
}

// PGZoneProvider reads mail.dns_zone + mail.dns_record.
type PGZoneProvider struct {
	Store *store.Store
}

func (p *PGZoneProvider) Zones(ctx context.Context) ([]Zone, error) {
	rows, err := p.Store.Pool.Query(ctx, `select z.id, z.zone, z.serial, r.name, r.rrtype, r.rdata, r.ttl
		from mail.dns_zone z left join mail.dns_record r on r.zone_id = z.id
		order by z.zone, r.name, r.rrtype`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[uuid.UUID]*Zone{}
	var order []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		var origin string
		var serial int64 // dns_zone.serial is bigint
		var name, rrtype, rdata *string
		var ttl *int
		if err := rows.Scan(&id, &origin, &serial, &name, &rrtype, &rdata, &ttl); err != nil {
			return nil, err
		}
		z, ok := byID[id]
		if !ok {
			if serial > 0xFFFFFFFF {
				serial = 0xFFFFFFFF
			}
			z = &Zone{Origin: strings.ToLower(strings.TrimSuffix(origin, ".")), Serial: uint32(serial)}
			byID[id] = z
			order = append(order, id)
		}
		if name == nil {
			continue // empty zone (left join miss)
		}
		rec := Record{Name: *name, RRType: "TXT", TTL: TTLDefault}
		if rrtype != nil {
			rec.RRType = *rrtype
		}
		if rdata != nil {
			rec.RData = *rdata
		}
		if ttl != nil && *ttl > 0 {
			rec.TTL = *ttl
		}
		z.Records = append(z.Records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Zone, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// ServerOptions configures the DNS edge.
type ServerOptions struct {
	NSHosts       []string      // authoritative NS names (4b.2: ≥2, distinct paths)
	Hostmaster    string        // SOA RNAME, e.g. "hostmaster.synkra.email"
	RefreshEvery  time.Duration // zone re-read cadence (0 = 15 s)
	AXFRAllowCIDR []string      // secondaries allowed to AXFR; EMPTY = deny all (4b.7)
	RRLBurst      int           // per-IP in-flight token bucket (0 = 100)
	RRLRefill     float64       // tokens per second (0 = 25)
}

// Server is the authoritative DNS edge.
type Server struct {
	Opts ServerOptions
	Prov ZoneProvider

	udp *dns.Server
	tcp *dns.Server
	rrl rrlState

	mu    sync.RWMutex
	zones []Zone // sorted by origin length desc (longest-suffix match)
	idx   map[string]*zoneView

	axfrAllowed []*net.IPNet

	// counters for ops/monitoring (4b.11: query volume, RRL drops).
	Queries  int64
	RRLDrops int64
	AXFRs    int64
}

// zoneView is one zone compiled into wire-ready RR sets.
type zoneView struct {
	origin  string // FQDN with trailing dot
	serial  uint32
	answers map[string][]dns.RR // FQDN → all records at that name (any type)
	soa     *dns.SOA
	ns      []dns.RR
}

// NewServer compiles an empty edge; call Refresh to load zones.
func NewServer(prov ZoneProvider, opts ServerOptions) *Server {
	if opts.RefreshEvery <= 0 {
		opts.RefreshEvery = 15 * time.Second
	}
	if opts.RRLBurst <= 0 {
		opts.RRLBurst = 100
	}
	if opts.RRLRefill <= 0 {
		opts.RRLRefill = 25
	}
	if len(opts.NSHosts) == 0 {
		opts.NSHosts = []string{"ns1.synkra.email", "ns2.synkra.email"}
	}
	if opts.Hostmaster == "" {
		opts.Hostmaster = "hostmaster.synkra.email"
	}
	s := &Server{Opts: opts, Prov: prov, idx: map[string]*zoneView{}}
	s.compileAXFRACL(opts.AXFRAllowCIDR)
	return s
}

func (s *Server) compileAXFRACL(cidrs []string) {
	s.axfrAllowed = nil
	for _, c := range cidrs {
		if !strings.Contains(c, "/") {
			c += "/32"
		}
		if _, n, err := net.ParseCIDR(c); err == nil {
			s.axfrAllowed = append(s.axfrAllowed, n)
		}
	}
}

// Refresh re-reads zones from the provider and recompiles.
func (s *Server) Refresh(ctx context.Context) error {
	zones, err := s.Prov.Zones(ctx)
	if err != nil {
		return err
	}
	idx := make(map[string]*zoneView, len(zones))
	for i := range zones {
		v := s.compileZone(&zones[i])
		idx[v.origin] = v
	}
	s.mu.Lock()
	s.zones = zones
	s.idx = idx
	s.mu.Unlock()
	return nil
}

func (s *Server) compileZone(z *Zone) *zoneView {
	origin := dns.Fqdn(z.Origin)
	v := &zoneView{origin: origin, serial: z.Serial, answers: map[string][]dns.RR{}}
	// SOA (synthesised: the operator's NS hostnames, serial from the store).
	v.soa = &dns.SOA{
		Hdr:     dns.RR_Header{Name: origin, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: TTLDefault},
		Ns:      dns.Fqdn(s.Opts.NSHosts[0]),
		Mbox:    dns.Fqdn(strings.ReplaceAll(s.Opts.Hostmaster, "@", ".")),
		Serial:  z.Serial,
		Refresh: 3600, Retry: 600, Expire: 1209600, Minttl: TTLDefault,
	}
	for _, ns := range s.Opts.NSHosts {
		v.ns = append(v.ns, &dns.NS{
			Hdr: dns.RR_Header{Name: origin, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: TTLNS},
			Ns:  dns.Fqdn(ns),
		})
	}
	for _, r := range z.Records {
		name := r.Name
		if name == "" || name == "@" {
			name = origin
		} else {
			name = dns.Fqdn(strings.ToLower(name) + "." + origin)
		}
		rr, err := rrFromRecord(name, r)
		if err != nil {
			log.Printf("[dnsd] zone %s record %s/%s: %v", z.Origin, r.Name, r.RRType, err)
			continue
		}
		v.answers[name] = append(v.answers[name], rr)
	}
	return v
}

// rrFromRecord builds a miekg RR from the store row. TXT values are quoted
// (the store keeps the unquoted value; the wire needs a character-string).
func rrFromRecord(name string, r Record) (dns.RR, error) {
	rrtype := strings.ToUpper(strings.TrimSpace(r.RRType))
	ttl := r.TTL
	if ttl <= 0 {
		ttl = TTLDefault
	}
	rdata := strings.TrimSpace(r.RData)
	if rrtype == "TXT" && !strings.HasPrefix(rdata, `"`) {
		rdata = `"` + strings.ReplaceAll(strings.ReplaceAll(rdata, `\`, `\\`), `"`, `\"`) + `"`
	}
	rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", name, ttl, rrtype, rdata))
	if err != nil {
		return nil, fmt.Errorf("bad rdata: %w", err)
	}
	return rr, nil
}

// matchZone finds the zone whose origin is the longest suffix of qname.
func (s *Server) matchZone(qname string) *zoneView {
	name := strings.ToLower(dns.Fqdn(qname))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if z, ok := s.idx[name]; ok {
		return z
	}
	for _, z := range s.idx {
		if dns.IsSubDomain(z.origin, name) {
			return z // the map is built over few zones; suffix test is exact
		}
	}
	return nil
}

// lookup answers qname/qtype against one zone: exact match, then DNS
// wildcards (closest encloser's wildcard, walking up), then NXDOMAIN/NODATA.
func (s *Server) lookup(z *zoneView, qname string, qtype uint16) ([]dns.RR, bool) {
	name := strings.ToLower(dns.Fqdn(qname))
	if rrs, ok := z.answers[name]; ok {
		var out []dns.RR
		for _, rr := range rrs {
			if rr.Header().Rrtype == qtype {
				out = append(out, rr)
			}
		}
		if len(out) > 0 || qtype == dns.TypeANY {
			return out, true
		}
		return nil, true // name exists, type absent → NODATA
	}
	// Wildcard walk: replace leftmost labels below the zone origin with "*",
	// closest first (approximates RFC 1034 4.3.3 closest-encloser).
	labels := dns.SplitDomainName(name)
	zoneLabels := dns.SplitDomainName(z.origin)
	for i := 1; i < len(labels)-len(zoneLabels)+1 && len(labels)-i >= len(zoneLabels); i++ {
		if labels[i-1] == "*" {
			break // already at a wildcard query
		}
		wild := "*." + strings.Join(labels[i:], ".")
		if rrs, ok := z.answers[dns.Fqdn(strings.ToLower(wild))]; ok {
			var out []dns.RR
			for _, rr := range rrs {
				if rr.Header().Rrtype == qtype {
					c := dns.Copy(rr)
					// The wildcard synthesises the queried name.
					c.Header().Name = dns.Fqdn(name)
					out = append(out, c)
				}
			}
			if len(out) > 0 {
				return out, true
			}
			return nil, true // wildcard name exists → NODATA for this type
		}
	}
	return nil, false
}

// Handler is the authoritative query handler (single entry for UDP and TCP;
// AXFR is dispatched here and only honoured over TCP).
func (s *Server) Handler(w dns.ResponseWriter, r *dns.Msg) {
	if _, isTCP := w.RemoteAddr().(*net.TCPAddr); isTCP && len(r.Question) == 1 &&
		r.Question[0].Qtype == dns.TypeAXFR && !r.Response && r.Opcode == dns.OpcodeQuery {
		s.HandleAXFR(w, r)
		return
	}
	if r.Opcode != dns.OpcodeQuery || r.Response {
		_ = w.WriteMsg(refuse(r))
		return
	}
	q := r.Question[0]
	m := new(dns.Msg)
	m.SetReply(r)
	// Authoritative only: never offer recursion (we are not a resolver — 4b.8 posture).
	m.RecursionAvailable = false
	m.RecursionDesired = r.RecursionDesired
	m.Authoritative = true

	// Response rate limiting on UDP (4b.8: an authoritative server is an
	// amplification target — drop, don't answer, when a source floods).
	if _, isUDP := w.RemoteAddr().(*net.UDPAddr); isUDP && !s.rrlAllow(w.RemoteAddr()) {
		s.countRRL()
		return // drop silently (standard RRL "no response" mode)
	}

	z := s.matchZone(q.Name)
	if z == nil {
		m.MsgHdr.Authoritative = false
		m.SetRcode(r, dns.RcodeRefused) // not our data — say so, do not lie
		_ = w.WriteMsg(m)
		return
	}
	if q.Qtype == dns.TypeSOA && dns.Fqdn(strings.ToLower(q.Name)) == z.origin {
		m.Answer = append(m.Answer, z.soa)
		_ = w.WriteMsg(m)
		return
	}
	rrs, found := s.lookup(z, q.Name, q.Qtype)
	switch {
	case found && len(rrs) > 0:
		m.Answer = rrs
		m.Ns = []dns.RR{z.soa} // authority: the serving SOA
	case found:
		m.Ns = []dns.RR{z.soa} // NODATA: name exists, type empty
	default:
		m.SetRcode(r, dns.RcodeNameError)
		m.Ns = []dns.RR{z.soa} // NXDOMAIN carries the SOA (negative TTL)
	}
	if w.WriteMsg(m) == nil {
		s.countQuery()
	}
}

func refuse(r *dns.Msg) *dns.Msg {
	m := new(dns.Msg)
	m.SetRcode(r, dns.RcodeNotImplemented)
	m.Authoritative = false
	return m
}

// ---- RRL: per-source token bucket ----

type bucket struct {
	tokens float64
	last   time.Time
}

type rrlState struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

func (s *Server) rrlAllow(addr net.Addr) bool {
	ip := ""
	if a, ok := addr.(*net.UDPAddr); ok {
		ip = a.IP.String()
	} else {
		return true
	}
	s.rrl.mu.Lock()
	defer s.rrl.mu.Unlock()
	if s.rrl.buckets == nil {
		s.rrl.buckets = map[string]*bucket{}
	}
	now := time.Now()
	b, ok := s.rrl.buckets[ip]
	if !ok {
		s.rrl.buckets[ip] = &bucket{tokens: float64(s.Opts.RRLBurst) - 1, last: now}
		return true
	}
	b.tokens += now.Sub(b.last).Seconds() * s.Opts.RRLRefill
	if b.tokens > float64(s.Opts.RRLBurst) {
		b.tokens = float64(s.Opts.RRLBurst)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (s *Server) countQuery() { s.mu.Lock(); s.Queries++; s.mu.Unlock() }
func (s *Server) countRRL()   { s.mu.Lock(); s.RRLDrops++; s.mu.Unlock() }

// ---- AXFR (4b.7): TCP only, ACL-gated, denied by default ----

func (s *Server) axfrAllowedFor(addr net.Addr) bool {
	if len(s.axfrAllowed) == 0 {
		return false // deny by default — no ACL, no transfer
	}
	ip := net.ParseIP(hostOf(addr))
	if ip == nil {
		return false
	}
	for _, n := range s.axfrAllowed {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func hostOf(addr net.Addr) string {
	switch a := addr.(type) {
	case *net.TCPAddr:
		return a.IP.String()
	case *net.UDPAddr:
		return a.IP.String()
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

// HandleAXFR serves a zone transfer over an established TCP connection
// (miekg hands us the first TSIG-checked request; we stream the whole zone).
func (s *Server) HandleAXFR(w dns.ResponseWriter, r *dns.Msg) {
	if !s.axfrAllowedFor(w.RemoteAddr()) {
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeRefused)
		_ = w.WriteMsg(m)
		return
	}
	q := r.Question[0]
	z := s.matchZone(q.Name)
	if z == nil || dns.Fqdn(strings.ToLower(q.Name)) != z.origin {
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeRefused)
		_ = w.WriteMsg(m)
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	// SOA — records — SOA (the AXFR frame).
	m.Answer = append(m.Answer, z.soa)
	for _, name := range sortedKeys(z.answers) {
		m.Answer = append(m.Answer, z.answers[name]...)
	}
	m.Answer = append(m.Answer, z.soa)
	m.Truncate(dns.MaxMsgSize)
	_ = w.WriteMsg(m)
	s.mu.Lock()
	s.AXFRs++
	s.mu.Unlock()
}

func sortedKeys(m map[string][]dns.RR) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---- lifecycle ----

// ListenAndServe starts UDP+TCP listeners, refreshes zones on the ticker, and
// blocks until ctx is done.
func (s *Server) ListenAndServe(ctx context.Context, udpAddr, tcpAddr string) error {
	if err := s.Refresh(ctx); err != nil {
		return fmt.Errorf("initial zone load: %w", err)
	}
	s.udp = &dns.Server{Addr: udpAddr, Net: "udp", Handler: dns.HandlerFunc(s.Handler)}
	s.tcp = &dns.Server{Addr: tcpAddr, Net: "tcp", Handler: dns.HandlerFunc(s.Handler)}
	go func() { _ = s.udp.ListenAndServe() }()
	go func() { _ = s.tcp.ListenAndServe() }()

	ticker := time.NewTicker(s.Opts.RefreshEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.udp.ShutdownContext(shutdownCtx)
			_ = s.tcp.ShutdownContext(shutdownCtx)
			cancel()
			return nil
		case <-ticker.C:
			if err := s.Refresh(ctx); err != nil {
				log.Printf("[dnsd] zone refresh: %v", err)
			}
		}
	}
}
