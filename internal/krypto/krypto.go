package krypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

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

// DeriveKey turns a passphrase into a 32-byte AES key (SHA-256). Raw 32-byte
// base64 keys are preferred; this keeps passphrase-form env values safe to use.
func DeriveKey(passphrase string) []byte {
	return deriveKey([]byte(passphrase))
}

// SealedString / OpenSealedString: the durable TEXT form of encrypted-at-rest
// secrets (DKIM private keys, TOTP seeds). Format "enc1.<b64(nonce)>.<b64(ct)>"
// so plaintext rows are recognisable and migratable. Fail-closed: an empty key
// refuses to seal; a "enc1." row without a key refuses to open.
const sealPrefix = "enc1."

func SealedString(key []byte, plaintext string) (string, error) {
	if len(key) == 0 {
		return "", errors.New("refusing to seal: no key configured")
	}
	ct, err := gcmSeal(key, []byte(plaintext))
	if err != nil {
		return "", err
	}
	nonceLen := 12
	if len(ct) < nonceLen {
		return "", errors.New("seal: short ciphertext")
	}
	return sealPrefix + base64.RawStdEncoding.EncodeToString(ct), nil
}

func OpenSealedString(key []byte, sealed string) (string, error) {
	if !strings.HasPrefix(sealed, sealPrefix) {
		// Legacy plaintext row (pre-hardening dev data). Prod refuses.
		return sealed, nil
	}
	if len(key) == 0 {
		return "", errors.New("refusing to open sealed secret: no key configured")
	}
	ct, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(sealed, sealPrefix))
	if err != nil {
		return "", err
	}
	pt, err := gcmOpen(key, ct)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// ---- TOTP (RFC 6238, SHA-1, 6 digits, 30 s step — the authenticator-app
// standard; implemented here so MFA has no new dependency) ----

// NewTOTPSecret returns a base32 (RFC 4648, no padding) seed, 160 bits.
func NewTOTPSecret() string {
	b := make([]byte, 20)
	rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

// TOTPCode computes the 6-digit code for the 30-second step at t.
func TOTPCode(secret string, t time.Time) (string, error) {
	return totpCodeOffset(secret, t.Unix()/30)
}

// VerifyTOTP checks code against the current step and ±1 window (clock skew),
// in constant time per comparison.
func VerifyTOTP(secret, code string) bool {
	step := time.Now().Unix() / 30
	for _, off := range []int64{0, -1, 1} {
		want, err := totpCodeOffset(secret, step+off)
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

func totpCodeOffset(secret string, step int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil || len(key) == 0 {
		return "", errors.New("bad totp secret")
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	val := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", val), nil
}

// TOTPAuthURL renders the otpauth:// URI authenticator apps scan.
func TOTPAuthURL(issuer, account, secret string) string {
	q := urlQueryEscape("issuer") + "=" + urlQueryEscape(issuer) +
		"&" + urlQueryEscape("secret") + "=" + urlQueryEscape(secret)
	return "otpauth://totp/" + urlQueryEscape(issuer) + ":" + urlQueryEscape(account) + "?" + q
}

func urlQueryEscape(s string) string {
	replacer := strings.NewReplacer("%", "%25", " ", "%20", ":", "%3A", "/", "%2F", "?", "%3F", "&", "%26", "=", "%3D", "#", "%23")
	return replacer.Replace(s)
}
