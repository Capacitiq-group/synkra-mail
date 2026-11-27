// Package s3blob: S3-compatible blob store for production (blob.Store).
// A minimal SigV4 client against the S3 REST API — no SDK dependency —
// compatible with AWS S3, Contabo Object Storage, MinIO and other
// S3-compatible endpoints (design doc Sections 4.4 and 14.1).
//
// Objects are keyed by their hex SHA-256 under blob.PathFor sharding
// ("aa/<sha256>"), written once and never mutated, mirroring fsblob.
package s3blob

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"synkra.email/mail/internal/blob"
	"synkra.email/mail/internal/config"
)

const (
	emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	timeFormat         = "20060102T150405Z"
	dateFormat         = "20060102"
)

type Client struct {
	cfg    config.S3
	http   *http.Client
	base   *url.URL // endpoint without bucket
	host   string   // host used in URLs and the canonical request
	bucket string
}

// New validates the endpoint and returns a client. No network I/O happens here.
func New(cfg config.S3) (*Client, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("s3blob: endpoint, bucket, access key and secret key are required")
	}
	base, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("s3blob: bad endpoint: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("s3blob: endpoint scheme must be http or https")
	}
	host := base.Host
	if !cfg.PathStyle {
		// Virtual-host style: bucket becomes a subdomain.
		host = cfg.Bucket + "." + base.Host
	}
	return &Client{
		cfg:    cfg,
		http:   &http.Client{Timeout: 120 * time.Second},
		base:   base,
		host:   host,
		bucket: cfg.Bucket,
	}, nil
}

// objectURL builds the request URL for an object key.
func (c *Client) objectURL(key string) *url.URL {
	u := *c.base
	if c.cfg.PathStyle {
		u.Path = "/" + c.bucket + "/" + key
	} else {
		u.Path = "/" + key
	}
	u.Host = c.host
	return &u
}

// Put stores content and returns its hex sha256 + size. The body is buffered
// (message bodies are capped at MaxMessageBytes, ~25 MB) so the SHA-256 can
// double as the SigV4 payload hash and the object key in one pass.
func (c *Client) Put(ctx context.Context, r io.Reader) (string, int64, error) {
	var buf bytes.Buffer
	hr := blob.NewHashReader(r)
	if _, err := io.Copy(&buf, hr); err != nil {
		return "", 0, err
	}
	sha := hr.Sum()
	key := blob.PathFor(sha)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.objectURL(key).String(), bytes.NewReader(buf.Bytes()))
	if err != nil {
		return "", 0, err
	}
	req.ContentLength = hr.Size()
	c.sign(req, sha, time.Now())

	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("s3blob: put %s: %w", key, err)
	}
	defer drainClose(resp)
	if resp.StatusCode/100 != 2 {
		return "", 0, c.httpError("put", key, resp)
	}
	return sha, hr.Size(), nil
}

// keyFor validates the sha and returns the object key ("" keys never reach
// the object store — blob.PathFor refuses non-hex input).
func keyFor(sha string) (string, error) {
	key := blob.PathFor(sha)
	if key == "" {
		return "", blob.ErrNotFound
	}
	return key, nil
}

// Open returns a reader over the object body; the caller must close it.
func (c *Client) Open(ctx context.Context, sha string) (io.ReadCloser, error) {
	key, err := keyFor(sha)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.objectURL(key).String(), nil)
	if err != nil {
		return nil, err
	}
	c.sign(req, emptyPayloadSHA256, time.Now())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3blob: get %s: %w", key, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		drainClose(resp)
		return nil, blob.ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		defer drainClose(resp)
		return nil, c.httpError("get", key, resp)
	}
	return resp.Body, nil
}

// Stat reports the object size via HEAD.
func (c *Client) Stat(ctx context.Context, sha string) (int64, error) {
	key, err := keyFor(sha)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.objectURL(key).String(), nil)
	if err != nil {
		return 0, err
	}
	c.sign(req, emptyPayloadSHA256, time.Now())

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("s3blob: head %s: %w", key, err)
	}
	defer drainClose(resp)
	if resp.StatusCode == http.StatusNotFound {
		return 0, blob.ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		return 0, c.httpError("head", key, resp)
	}
	return resp.ContentLength, nil
}

// Delete removes the object. Missing objects are a no-op, matching fsblob
// semantics (GC only; customer mail is never auto-deleted).
func (c *Client) Delete(ctx context.Context, sha string) error {
	key, err := keyFor(sha)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.objectURL(key).String(), nil)
	if err != nil {
		return err
	}
	c.sign(req, emptyPayloadSHA256, time.Now())

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("s3blob: delete %s: %w", key, err)
	}
	defer drainClose(resp)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode/100 == 2 {
		return nil
	}
	return c.httpError("delete", key, resp)
}

func (c *Client) httpError(op, key string, resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("s3blob: %s %s: HTTP %d: %s", op, key, resp.StatusCode, snippet)
}

// sign applies AWS Signature Version 4 to the request. The canonical request
// covers host, x-amz-content-sha256 and x-amz-date; query strings are empty
// for every operation this client performs.
func (c *Client) sign(req *http.Request, payloadHash string, now time.Time) {
	amzDate := now.UTC().Format(timeFormat)
	dateStamp := now.UTC().Format(dateFormat)

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	canonicalURI := escapePath(req.URL.Path)
	canonicalHeaders := "host:" + c.host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		"", // canonical query string: no query params are ever used
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + c.cfg.Region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hexSHA256([]byte(canonicalRequest)),
	}, "\n")

	signingKey := hmacSHA256([]byte("AWS4"+c.cfg.SecretKey), []byte(dateStamp))
	signingKey = hmacSHA256(signingKey, []byte(c.cfg.Region))
	signingKey = hmacSHA256(signingKey, []byte("s3"))
	signingKey = hmacSHA256(signingKey, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential="+c.cfg.AccessKey+"/"+scope+
			", SignedHeaders="+signedHeaders+
			", Signature="+signature)
}

// escapePath URI-encodes each path segment (S3 canonical URI form).
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

func hexSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

// drainClose consumes and closes a response body so the connection can be reused.
func drainClose(resp *http.Response) {
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
}
