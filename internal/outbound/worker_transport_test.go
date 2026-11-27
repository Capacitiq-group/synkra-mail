// worker_transport_test.go: the transport behaviours the 27 November audit
// called out — above all the RFC 3207 sequence (audit item 1: the TLS
// handshake ran without ever sending STARTTLS, so real servers saw a
// ClientHello where an SMTP command belongs).
package outbound

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-msgauth/dkim"
	gosmtp "github.com/emersion/go-smtp"

	"synkra.email/mail/internal/mimeutil"
)

// tlsFakeServer: a REAL go-smtp server that advertises STARTTLS with a
// self-signed certificate. This is the closest a sandbox gets to "test
// against a real server": the client must speak the correct command sequence
// or the dialogue dies.
func tlsFakeServer(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "mx1.synkra.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}

	be := gosmtp.BackendFunc(func(c *gosmtp.Conn) (gosmtp.Session, error) { return &openSession{}, nil })
	srv := gosmtp.NewServer(be)
	srv.Domain = "synkra.test"
	srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.MaxMessageBytes = 1024 * 1024
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

// openSession: no-auth backend (a bare relay for the transport test only).
type openSession struct{}

func (o *openSession) Reset()        {}
func (o *openSession) Logout() error { return nil }
func (o *openSession) Mail(string, *gosmtp.MailOptions) error {
	return nil
}
func (o *openSession) Rcpt(string, *gosmtp.RcptOptions) error { return nil }
func (o *openSession) Data(r io.Reader) error {
	_, _ = io.ReadAll(r)
	return nil
}

// TestSTARTTLSCommandSequence delivers through a real STARTTLS server. The
// pre-audit code handshaked without sending STARTTLS: the server read a TLS
// ClientHello as an SMTP command, the handshake failed, and the message
// retried for 72 hours before bouncing. This test fails that way.
func TestSTARTTLSCommandSequence(t *testing.T) {
	addr := tlsFakeServer(t)
	tr := &SMTPTransport{
		HostPort:         addr,
		HeloName:         "mx1.synkra.co.za",
		SkipEgressFilter: true,
		Timeout:          10 * time.Second,
	}
	raw := buildSimpleMessage(t)
	code, msg, err := tr.Deliver(t.Context(), "sender@example.co.za", "rcpt@example.co.za", raw)
	if err != nil {
		t.Fatalf("deliver through STARTTLS server: %v (code=%d msg=%q)", err, code, msg)
	}
	if code != 250 {
		t.Fatalf("deliver code = %d msg=%q, want 250", code, msg)
	}
}

// TestEgressFilterRefusesPrivateSpace: MX names that resolve to loopback are
// refused for outbound delivery (audit item 10 — outbound SSRF).
func TestEgressFilterRefusesPrivateSpace(t *testing.T) {
	if err := egressAllowed(t.Context(), "localhost"); err == nil {
		t.Fatal("localhost must be refused for outbound delivery")
	}
	// The classifier itself: RFC1918, CGNAT and benchmark space are not public.
	for _, s := range []string{"10.1.2.3", "192.168.5.5", "172.16.9.9", "100.64.0.9", "198.18.0.1", "127.0.0.1"} {
		ip := net.ParseIP(s).To4()
		if ip == nil {
			t.Fatalf("test premise broken for %s", s)
		}
		private := ip.IsPrivate() || ip.IsLoopback() || ip[0] == 100 || (ip[0] == 198 && ip[1] == 18) || ip[0] == 0
		if !private {
			t.Errorf("%s should classify as non-public", s)
		}
	}
}

// TestClassifyBounce: 5.7.x is OUR reputation problem, not the recipient's
// (audit item 18: every 5xx suppressed recipients and fed the breaker).
func TestClassifyBounce(t *testing.T) {
	cases := []struct {
		code int
		msg  string
		want bounceClass
	}{
		{550, "5.1.1 User unknown in virtual mailbox table", bounceRecipientUnknown},
		{550, "5.1.10 no such user here", bounceRecipientUnknown},
		{554, "5.7.1 Service unavailable; Client host blocked using Spamhaus", bouncePolicyReject},
		{550, "5.7.26 This mail has been blocked because the sender is unauthenticated", bouncePolicyReject},
		{552, "5.2.2 The email account that you tried to reach is over quota", bounceMailboxFull},
		{550, "blacklisted by reputation", bouncePolicyReject},
		{553, "sorry, that domain isn't allowed to be relayed", bounceOtherHard},
	}
	for _, c := range cases {
		if got := classifyBounce(c.code, c.msg); got != c.want {
			t.Errorf("classifyBounce(%d, %q) = %s, want %s", c.code, c.msg, got, c.want)
		}
	}
}

// TestDKIMOversigningBreaksOnInjectedFrom: an attacker-prepended second From
// header must break the signature (audit item 9).
func TestDKIMOversigningBreaksOnInjectedFrom(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	// The public record a resolver would answer with (no live DNS in tests).
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("pubkey: %v", err)
	}
	pubTXT := "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(pubDER)
	lookup := func(domain string) ([]string, error) {
		if domain == "test._domainkey.example.co.za" {
			return []string{pubTXT}, nil
		}
		return nil, fmt.Errorf("no key for %s", domain)
	}

	raw := buildSimpleMessage(t)
	signed, err := dkimSign(raw, "example.co.za", "test", privPEM)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// Sanity: the honest message verifies.
	if vs, verr := dkim.VerifyWithOptions(strings.NewReader(string(signed)), &dkim.VerifyOptions{LookupTXT: lookup}); verr != nil || len(vs) == 0 || vs[0].Err != nil {
		t.Fatalf("honest message must verify: err=%v vs=%+v", verr, vs)
	}
	// Forged: a second From header prepended above ours.
	forged := append([]byte("From: attacker@evil.example\r\n"), signed...)
	vs, verr := dkim.VerifyWithOptions(strings.NewReader(string(forged)), &dkim.VerifyOptions{LookupTXT: lookup})
	if verr == nil && len(vs) > 0 && vs[0].Err == nil {
		t.Fatal("forged second From must break the DKIM signature (oversigning)")
	}
}

// ---- helpers ----

func buildSimpleMessage(t *testing.T) []byte {
	t.Helper()
	raw, _, err := mimeutil.Build(mimeutil.ComposeInput{
		From:    mimeutil.Addr{Email: "sender@example.co.za", Name: "Sender"},
		To:      []mimeutil.Addr{{Email: "rcpt@example.co.za"}},
		Subject: "transport test",
		Text:    "hello over TLS",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return raw
}
