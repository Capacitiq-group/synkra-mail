package ingest

import "bytes"

// NewReader wraps bytes.NewReader for blob.Put — keeps the ingest file readable.
func NewReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
