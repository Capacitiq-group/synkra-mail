// Package fsblob: filesystem blob store (dev + single-VPS scratch). Production
// Stage A swaps in the S3 adapter via config; the Store interface is identical.
package fsblob

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"synkra.email/mail/internal/blob"
)

type Store struct {
	root string
}

func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func (s *Store) path(sha string) string {
	return filepath.Join(s.root, blob.PathFor(sha))
}

func (s *Store) Put(ctx context.Context, r io.Reader) (string, int64, error) {
	hr := blob.NewHashReader(r)
	tmp, err := os.CreateTemp(s.root, ".tmp-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())

	size, err := io.Copy(tmp, hr)
	if err != nil {
		tmp.Close()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	sha := hr.Sum()
	dst := s.path(sha)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, err
	}
	// Content-addressed: identical content overwrites itself harmlessly.
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", 0, err
	}
	return sha, size, nil
}

func (s *Store) Open(ctx context.Context, sha string) (io.ReadCloser, error) {
	f, err := os.Open(s.path(sha))
	if errors.Is(err, os.ErrNotExist) {
		return nil, blob.ErrNotFound
	}
	return f, err
}

func (s *Store) Stat(ctx context.Context, sha string) (int64, error) {
	fi, err := os.Stat(s.path(sha))
	if errors.Is(err, os.ErrNotExist) {
		return 0, blob.ErrNotFound
	}
	return fi.Size(), err
}

func (s *Store) Delete(ctx context.Context, sha string) error {
	err := os.Remove(s.path(sha))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
