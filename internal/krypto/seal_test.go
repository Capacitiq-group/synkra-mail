package krypto

import (
	"strings"
	"testing"
	"time"
)

// ---- sealed secrets (audit item 4) ----

func TestSealedStringRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	sealed, err := SealedString(key, "-----BEGIN RSA PRIVATE KEY-----")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if !strings.HasPrefix(sealed, sealPrefix) {
		t.Fatalf("sealed form must carry the %q prefix, got %q", sealPrefix, sealed)
	}
	if strings.Contains(sealed, "BEGIN RSA") {
		t.Fatal("sealed string must not contain the plaintext")
	}
	opened, err := OpenSealedString(key, sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if opened != "-----BEGIN RSA PRIVATE KEY-----" {
		t.Fatalf("round trip mismatch")
	}
	// Wrong key → refuse.
	if _, err := OpenSealedString([]byte("another-key-another-key-another-"), sealed); err == nil {
		t.Fatal("wrong key must fail to open")
	}
	// No key → refuse to seal, refuse to open sealed rows (fail-closed).
	if _, err := SealedString(nil, "x"); err == nil {
		t.Fatal("sealing without a key must fail")
	}
	if _, err := OpenSealedString(nil, sealed); err == nil {
		t.Fatal("opening a sealed row without a key must fail")
	}
	// Legacy plaintext rows pass through (dev bootstrap migration path).
	if got, err := OpenSealedString(nil, "plain-dev-key"); err != nil || got != "plain-dev-key" {
		t.Fatalf("legacy plaintext passthrough broken: %q %v", got, err)
	}
}

// ---- TOTP (audit item 3) ----

func TestTOTPCodeMatchesAuthenticatorSemantics(t *testing.T) {
	secret := NewTOTPSecret()
	if len(secret) != 32 { // 160 bits base32, no padding
		t.Fatalf("secret length = %d, want 32", len(secret))
	}
	// RFC 6238 Appendix B vector: SHA-1, secret "12345678901234567890",
	// T=59 → 287082. This pins the algorithm, not just the format.
	rfcSecret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := TOTPCode(rfcSecret, time.Unix(59, 0))
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if code != "287082" {
		t.Fatalf("RFC 6238 vector: got %q, want 287082", code)
	}
	// A fresh code verifies against the current step (±1 window).
	now, _ := TOTPCode(secret, time.Now())
	if !VerifyTOTP(secret, now) {
		t.Fatal("current code must verify")
	}
	if VerifyTOTP(secret, "000000") {
		// Possible-but-improbable collision; recompute to be sure.
		next, _ := TOTPCode(secret, time.Now().Add(90*time.Second))
		if next != "000000" {
			t.Fatal("a wrong code must not verify")
		}
	}
}

func TestTOTPAuthURLShape(t *testing.T) {
	url := TOTPAuthURL("Synkra Mail", "user@example.co.za", "ABC234DEF")
	if !strings.HasPrefix(url, "otpauth://totp/Synkra%20Mail:") {
		t.Fatalf("otpauth url shape: %q", url)
	}
	if !strings.Contains(url, "secret=ABC234DEF") || !strings.Contains(url, "issuer=Synkra%20Mail") {
		t.Fatalf("otpauth url params: %q", url)
	}
}
