// Package blob: content-addressed immutable blob storage (design doc Section 4.4).
// Blobs are keyed by SHA-256, written once, never mutated. The NVMe/S3 split is a
// load-model rule: Postgres + search index on NVMe, bodies/attachments in S3.
package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

var ErrNotFound = errors.New("blob not found")

// Store is the storage interface. Implementations: fsblob (dev/local scratch),
// s3blob (Contabo Z1 S3 in production — Section 14.1).
type Store interface {
	// Put stores content and returns its hex sha256 + size.
	Put(ctx context.Context, r io.Reader) (sha string, size int64, err error)
	// Open returns a reader for the blob.
	Open(ctx context.Context, sha string) (io.ReadCloser, error)
	// Stat reports size.
	Stat(ctx context.Context, sha string) (int64, error)
	// Delete removes the blob (GC only; customer mail is never auto-deleted).
	Delete(ctx context.Context, sha string) error
}

// Layout: blobs/{aa}/{sha256} — flat sharding is enough at Stage-A scale.
func PathFor(sha string) string {
	if len(sha) < 2 {
		return ""
	}
	return fmt.Sprintf("%s/%s", sha[:2], sha)
}

// HashReader computes sha256 while reading.
type HashReader struct {
	r io.Reader
	h interface {
		io.Writer
		Sum(b []byte) []byte
	}
	n int64
}

func NewHashReader(r io.Reader) *HashReader {
	return &HashReader{r: r, h: sha256.New()}
}

func (hr *HashReader) Read(p []byte) (int, error) {
	n, err := hr.r.Read(p)
	if n > 0 {
		hr.h.Write(p[:n])
		hr.n += int64(n)
	}
	return n, err
}

func (hr *HashReader) Sum() string { return hex.EncodeToString(hr.h.Sum(nil)) }
func (hr *HashReader) Size() int64 { return hr.n }
