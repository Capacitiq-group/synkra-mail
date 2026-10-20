// Package krypto: password hashing (argon2id), token generation, and HMAC helpers.
// Passwords live in mail.user / mail.app_password — never anywhere else (Section 7.1).
package krypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// ---- argon2id (Section 13.1) ----

const (
	argonTime    = 3
	argonMemory  = 48 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword returns a PHC-style argon2id string: $argon2id$v=19$m=...,t=...,p=...$salt$hash
func HashPassword(password string) string {
	salt := make([]byte, argonSaltLen)
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

var ErrMismatch = errors.New("password mismatch")

func VerifyPassword(encoded, password string) error {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("unsupported hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return err
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return err
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// ---- tokens ----

// NewToken returns a 256-bit random URL-safe token.
func NewToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// SHA256Hex is the token-at-rest form (mail.session.token_hash).
func SHA256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ---- HMAC (tracking URLs, internal service auth — Sections 9.1, 11.3) ----

func HMACSign(key []byte, payload string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// HMACVerify is constant-time.
func HMACVerify(key []byte, payload, sig string) bool {
	want := HMACSign(key, payload)
	return subtle.ConstantTimeCompare([]byte(want), []byte(sig)) == 1
}

// ---- app passwords (owner decision: 10 active max, 180-day expiry) ----

// NewAppPassword mints "pppp_xxxxxxxxxxxxxxxxxxxxxxxx" where pppp is the visible prefix.
func NewAppPassword() (secret, prefix string) {
	pb := make([]byte, 3)
	rand.Read(pb)
	prefix = base64.RawURLEncoding.EncodeToString(pb)[:4]
	rb := make([]byte, 18)
	rand.Read(rb)
	secret = prefix + "_" + base64.RawURLEncoding.EncodeToString(rb)
	return secret, prefix
}

// ---- encryption for small secrets (TOTP secret at rest; DKIM keys handled separately) ----

// Seal seals small secrets with AES-GCM keyed from the cookie secret.
// Dev-honest: prod uses SOPS-managed keys (Section 7.6) — the cipher choice stays the same.
func Seal(key, plaintext []byte) ([]byte, error) {
	return gcmSeal(key, plaintext)
}

func Open(key, ciphertext []byte) ([]byte, error) {
	return gcmOpen(key, ciphertext)
}
