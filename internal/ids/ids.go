// Package ids mints prefixed UUIDv7 identifiers (design doc Section 4 conventions).
// Storage is a plain uuid column; the prefix is a presentation/type tag for humans
// and API consumers (usr_9m4..., msg_9m4...).
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Known prefixes (design doc Section 4).
const (
	Plan          = "plan"
	Org           = "org"
	Sub           = "sub"
	User          = "usr"
	Session       = "ses"
	AppPassword   = "apw"
	Domain        = "dom"
	Invoice       = "inv"
	Payment       = "pay"
	PaymentMethod = "pmt"
	Migration     = "mig"
	MigrationItem = "mi"
	Mailbox       = "mbx"
	Address       = "adr"
	Member        = "mem"
	Folder        = "fld"
	Blob          = "blb"
	Message       = "msg"
	Item          = "itm"
	Thread        = "thr"
	Attachment    = "att"
	Delivery      = "dlv"
	DSN           = "dsn"
	Pixel         = "trk"
	Link          = "lnk"
	Quarantine    = "qrt"
	QuotaEvent    = "qev"
	Audit         = "aud"
	RateEvent     = "rat"
	Ledger        = "led"
	Signature     = "sig"
	Rule          = "rul"
	Webhook       = "whk"
	APIKey        = "key"
)

var lastMu sync.Mutex
var lastMS int64
var lastCounter [10]byte // post-mask bytes 6..15; holds the 74 usable counter bits
var haveLast bool

// UUIDv7 mints a monotonic-within-process UUIDv7 (RFC 9562): bytes 0..5 carry
// the 48-bit unix-millisecond timestamp, byte 6 the version, and the rest a
// 74-bit counter (12-bit rand_a + 62-bit rand_b) that is seeded randomly on
// the first mint of each new millisecond and incremented on every
// same-millisecond mint. Together with the backward-clock clamp below, every
// id minted by this process is strictly greater than the previous one.
func UUIDv7() uuid.UUID {
	lastMu.Lock()
	defer lastMu.Unlock()

	ms := time.Now().UnixMilli()
	if haveLast && ms < lastMS {
		// Wall clock jumped backwards; keep ordering by reusing the last
		// timestamp (RFC 9562 Section 6.2: counters must not go backwards).
		ms = lastMS
	}

	var b [16]byte
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	if haveLast && ms == lastMS {
		if !incCounter74(&lastCounter) {
			rand.Read(lastCounter[:]) // 74-bit overflow: reseed (never in practice)
		}
	} else {
		rand.Read(lastCounter[:]) // fresh 74 random bits for a new millisecond
	}
	lastMS = ms
	haveLast = true

	copy(b[6:16], lastCounter[:])
	b[6] = (b[6] & 0x0f) | 0x70   // version 7
	b[8] = (b[8] & 0x3f) | 0x80   // variant 10xx
	copy(lastCounter[:], b[6:16]) // store post-mask; incCounter74 ignores reserved bits
	return b
}

// incCounter74 increments the 74 usable counter bits held big-endian in c
// (c[0] low nibble, c[1] full byte, c[2] low 6 bits, c[3..9] full bytes),
// leaving the reserved version/variant bit positions untouched so that a
// carry can never be silently clipped by the version/variant masks.
func incCounter74(c *[10]byte) bool {
	for i := 9; i >= 3; i-- {
		c[i]++
		if c[i] != 0 {
			return true
		}
	}
	// c[3] wrapped: carry into the 6 usable bits of c[2] (top 2 are variant).
	c[2] = (c[2] + 1) & 0x3f
	if c[2] != 0 {
		return true
	}
	// c[2] wrapped: carry into c[1].
	c[1]++
	if c[1] != 0 {
		return true
	}
	// c[1] wrapped: carry into the 4 usable bits of c[0] (top 4 are version).
	c[0] = (c[0] & 0xf0) | ((c[0] + 1) & 0x0f)
	return c[0]&0x0f != 0
}

// New returns "prefix_<uuidv7>" (used in API surfaces; DB stores the bare uuid).
func New(prefix string) string {
	return prefix + "_" + UUIDv7().String()
}

// Parse strips a known prefix and returns the uuid. Accepts bare uuids too.
func Parse(s string) (uuid.UUID, bool) {
	if i := strings.IndexByte(s, '_'); i > 0 {
		s = s[i+1:]
	}
	u, err := uuid.Parse(s)
	return u, err == nil
}

// ParseTyped parses an id and enforces the expected type (build register §1.1):
// a msg_ id passed where a folder id is required must be rejected, not silently
// stripped. Bare uuids are still accepted (the DB stores bare uuids).
func ParseTyped(s, prefix string) (uuid.UUID, bool) {
	if i := strings.IndexByte(s, '_'); i >= 0 {
		if s[:i] != prefix {
			return uuid.Nil, false
		}
		s = s[i+1:]
	}
	u, err := uuid.Parse(s)
	return u, err == nil
}

// Format renders a uuid with its prefix for API output.
func Format(prefix string, u uuid.UUID) string {
	return prefix + "_" + u.String()
}

// Hex returns 16 random bytes hex-encoded (tokens, hashes of secrets are elsewhere).
func Hex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
