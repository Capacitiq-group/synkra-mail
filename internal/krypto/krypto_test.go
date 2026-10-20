package krypto

import (
	"strings"
	"testing"
)

func TestPasswordHashRoundtrip(t *testing.T) {
	h := HashPassword("correct horse battery staple")
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("expected argon2id PHC string, got %q", h)
	}
	if err := VerifyPassword(h, "correct horse battery staple"); err != nil {
		t.Fatalf("verify should pass: %v", err)
	}
	if err := VerifyPassword(h, "wrong"); err == nil {
		t.Fatal("verify with wrong password should fail")
	}
	// Distinct salts → distinct hashes.
	if h == HashPassword("correct horse battery staple") {
		t.Fatal("hashes must be salted (unique per call)")
	}
}

func TestTokens(t *testing.T) {
	tok := NewToken()
	if len(tok) < 40 {
		t.Fatalf("token too short: %q", tok)
	}
	h1 := SHA256Hex(tok)
	h2 := SHA256Hex(tok)
	if h1 != h2 || len(h1) != 64 {
		t.Fatalf("sha256 hex unstable or wrong length: %q %q", h1, h2)
	}
	if SHA256Hex("other") == h1 {
		t.Fatal("sha256 collision?")
	}
}

func TestHMAC(t *testing.T) {
	key := []byte("secret")
	sig := HMACSign(key, "payload")
	if !HMACVerify(key, "payload", sig) {
		t.Fatal("HMAC verify should pass")
	}
	if HMACVerify(key, "payload2", sig) {
		t.Fatal("HMAC verify must fail on different payload")
	}
	if HMACVerify([]byte("other"), "payload", sig) {
		t.Fatal("HMAC verify must fail on different key")
	}
}

func TestAppPasswordFormat(t *testing.T) {
	secret, prefix := NewAppPassword()
	if len(prefix) != 4 {
		t.Fatalf("prefix length = %d, want 4", len(prefix))
	}
	if len(secret) < 25 || secret[:4] != prefix || secret[4] != '_' {
		t.Fatalf("secret format wrong: %q (prefix %q)", secret, prefix)
	}
}

func TestSealOpen(t *testing.T) {
	key := []byte("cookie-secret")
	plain := []byte("totp-secret-bytes")
	ct, err := Seal(key, plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	pt, err := Open(key, ct)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(pt) != string(plain) {
		t.Fatalf("roundtrip mismatch: %q != %q", pt, plain)
	}
	if _, err := Open([]byte("wrong-secret"), ct); err == nil {
		t.Fatal("open with wrong key should fail")
	}
}
