package ids

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUUIDv7Shape(t *testing.T) {
	for i := 0; i < 100; i++ {
		u := UUIDv7()
		if u.Version() != 7 {
			t.Fatalf("version = %d, want 7", u.Version())
		}
	}
}

func TestUUIDv7MonotonicWithinProcess(t *testing.T) {
	var last uuid.UUID
	for i := 0; i < 1000; i++ {
		u := UUIDv7()
		if i > 0 && bytes.Compare(last[:], u[:]) >= 0 {
			// Full strict monotonicity holds even across millisecond
			// boundaries and backward wall-clock jumps (the clock is
			// clamped), so any regression is a hard failure.
			t.Fatalf("uuid not strictly increasing: %v -> %v", last, u)
		}
		last = u
	}
}

func TestUUIDv7TimestampLayout(t *testing.T) {
	// RFC 9562: bytes 0..5 must carry the 48-bit unix-ms timestamp.
	before := time.Now().UnixMilli()
	u := UUIDv7()
	after := time.Now().UnixMilli()
	ms := int64(u[0])<<40 | int64(u[1])<<32 | int64(u[2])<<24 |
		int64(u[3])<<16 | int64(u[4])<<8 | int64(u[5])
	if ms < before-1 || ms > after+1 {
		t.Fatalf("timestamp %d outside [%d, %d]", ms, before-1, after+1)
	}
}

func TestPrefixFormatParse(t *testing.T) {
	s := New("msg")
	if !strings.HasPrefix(s, "msg_") {
		t.Fatalf("expected msg_ prefix, got %q", s)
	}
	u, ok := Parse(s)
	if !ok {
		t.Fatalf("Parse failed for %q", s)
	}
	if Format("msg", u) != s {
		t.Fatalf("Format(Parse(x)) != x")
	}
	// Bare uuid accepted too.
	if _, ok := Parse(u.String()); !ok {
		t.Fatal("bare uuid should parse")
	}
	if _, ok := Parse("not-a-uuid"); ok {
		t.Fatal("garbage should not parse")
	}
}
