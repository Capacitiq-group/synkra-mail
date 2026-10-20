package s3blob

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"synkra.email/mail/internal/blob"
	"synkra.email/mail/internal/config"
)

// fakeS3 is a minimal in-process S3-compatible server for tests. It stores
// objects and records every request so tests can assert signing and URL shape.
type fakeS3 struct {
	mu       sync.Mutex
	objects  map[string][]byte
	requests []*http.Request
	bodies   map[*http.Request][]byte
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string][]byte{}, bodies: map[*http.Request][]byte{}}
}

func (f *fakeS3) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, r)
		f.bodies[r] = body
		f.mu.Unlock()

		key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")
		switch r.Method {
		case http.MethodPut:
			f.mu.Lock()
			f.objects[key] = body
			f.mu.Unlock()
			w.Header().Set("ETag", `"fake-etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			f.mu.Lock()
			obj, ok := f.objects[key]
			f.mu.Unlock()
			if !ok {
				http.Error(w, "NoSuchKey", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", itoa(len(obj)))
			w.Write(obj)
		case http.MethodHead:
			f.mu.Lock()
			obj, ok := f.objects[key]
			f.mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", itoa(len(obj)))
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			f.mu.Lock()
			_, ok := f.objects[key]
			delete(f.objects, key)
			f.mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func (f *fakeS3) lastRequest(t *testing.T) (*http.Request, []byte) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("no requests recorded")
	}
	r := f.requests[len(f.requests)-1]
	return r, f.bodies[r]
}

// verifySigV4 recomputes the expected signature from the AWS SigV4 spec
// independently of the client implementation, using the request's own
// x-amz-date and payload hash headers. Catches signing regressions.
func verifySigV4(t *testing.T, r *http.Request, body []byte, accessKey, secretKey, region, bucket string) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("missing SigV4 authorization header: %q", auth)
	}
	m := regexp.MustCompile(`Signature=([0-9a-f]{64})`).FindStringSubmatch(auth)
	if m == nil {
		t.Fatalf("no signature in authorization header: %q", auth)
	}
	gotSig := m[1]

	amzDate := r.Header.Get("X-Amz-Date")
	payloadHash := r.Header.Get("X-Amz-Content-Sha256")
	if amzDate == "" || payloadHash == "" {
		t.Fatal("missing x-amz-date or x-amz-content-sha256")
	}
	if len(body) > 0 || r.Method != http.MethodPut {
		// The declared payload hash must match the actual payload.
		h := sha256.Sum256(body)
		if payloadHash != hex.EncodeToString(h[:]) {
			t.Fatalf("payload hash mismatch: header=%s actual=%s", payloadHash, hex.EncodeToString(h[:]))
		}
	}

	dateStamp := amzDate[:8]
	canonicalRequest := strings.Join([]string{
		r.Method,
		r.URL.Path, // keys are plain hex/shard paths; no escaping needed
		"",
		"host:" + r.Host + "\n" +
			"x-amz-content-sha256:" + payloadHash + "\n" +
			"x-amz-date:" + amzDate + "\n",
		"host;x-amz-content-sha256;x-amz-date",
		payloadHash,
	}, "\n")

	crHash := sha256.Sum256([]byte(canonicalRequest))
	scope := dateStamp + "/" + region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(crHash[:])

	mac := hmac.New(sha256.New, []byte("AWS4"+secretKey))
	mac.Write([]byte(dateStamp))
	k := mac.Sum(nil)
	mac = hmac.New(sha256.New, k)
	mac.Write([]byte(region))
	k = mac.Sum(nil)
	mac = hmac.New(sha256.New, k)
	mac.Write([]byte("s3"))
	k = mac.Sum(nil)
	mac = hmac.New(sha256.New, k)
	mac.Write([]byte("aws4_request"))
	k = mac.Sum(nil)
	mac = hmac.New(sha256.New, k)
	mac.Write([]byte(stringToSign))

	if want := hex.EncodeToString(mac.Sum(nil)); gotSig != want {
		t.Fatalf("SigV4 signature mismatch:\n got %s\nwant %s", gotSig, want)
	}
	if !strings.Contains(auth, "Credential="+accessKey+"/"+scope) {
		t.Fatalf("credential scope wrong in %q (want scope %s)", auth, scope)
	}
}

func testConfig(endpoint string) config.S3 {
	return config.S3{
		Endpoint:  endpoint,
		Region:    "eu2",
		Bucket:    "test-bucket",
		AccessKey: "AKIA-TEST",
		SecretKey: "test-secret",
		PathStyle: true,
	}
}

func TestRoundTripAndSigV4(t *testing.T) {
	fake := newFakeS3()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c, err := New(testConfig(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	content := []byte("From: a@b.c\r\n\r\nhello s3 blob store\r\n")
	sha, size, err := c.Put(t.Context(), bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if size != int64(len(content)) {
		t.Fatalf("Put size = %d, want %d", size, len(content))
	}
	sum := sha256.Sum256(content)
	if sha != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha mismatch: %s", sha)
	}

	// Verify the PUT request: path-style URL, SigV4 signature, payload hash.
	req, body := fake.lastRequest(t)
	if req.URL.Path != "/test-bucket/"+blob.PathFor(sha) {
		t.Fatalf("unexpected object path %q", req.URL.Path)
	}
	if req.Host != strings.TrimPrefix(srv.URL, "http://") {
		t.Fatalf("host header = %q, want %q", req.Host, srv.URL)
	}
	verifySigV4(t, req, body, "AKIA-TEST", "test-secret", "eu2", "test-bucket")

	// Open roundtrip.
	rc, err := c.Open(t.Context(), sha)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("roundtrip content mismatch")
	}

	// Stat.
	n, err := c.Stat(t.Context(), sha)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if n != int64(len(content)) {
		t.Fatalf("Stat = %d, want %d", n, len(content))
	}

	// Delete then confirm not-found; deleting again is a no-op.
	if err := c.Delete(t.Context(), sha); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.Open(t.Context(), sha); err != blob.ErrNotFound {
		t.Fatalf("Open after delete = %v, want ErrNotFound", err)
	}
	if _, err := c.Stat(t.Context(), sha); err != blob.ErrNotFound {
		t.Fatalf("Stat after delete = %v, want ErrNotFound", err)
	}
	if err := c.Delete(t.Context(), sha); err != nil {
		t.Fatalf("idempotent Delete = %v, want nil", err)
	}
}

func TestNotFoundMapping(t *testing.T) {
	fake := newFakeS3()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	c, err := New(testConfig(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	missing := strings.Repeat("ab", 32) // well-formed but absent sha
	if _, err := c.Open(t.Context(), missing); err != blob.ErrNotFound {
		t.Fatalf("Open missing = %v, want ErrNotFound", err)
	}
	if _, err := c.Stat(t.Context(), missing); err != blob.ErrNotFound {
		t.Fatalf("Stat missing = %v, want ErrNotFound", err)
	}
}

func TestVirtualHostStyle(t *testing.T) {
	// Virtual-host addressing (bucket as subdomain) cannot be exercised over
	// HTTP against an IP-bound httptest server ("bucket.127.0.0.1" has no
	// DNS), so assert URL construction directly — production endpoints are
	// hostnames where this form is valid.
	base := "https://eu2.contabostorage.com"

	cfg := testConfig(base)
	cfg.PathStyle = false
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	u := c.objectURL("aa/" + strings.Repeat("ab", 32))
	if u.Host != "test-bucket.eu2.contabostorage.com" {
		t.Fatalf("virtual host = %q, want test-bucket.eu2.contabostorage.com", u.Host)
	}
	if u.Path != "/aa/"+strings.Repeat("ab", 32) {
		t.Fatalf("virtual path must exclude the bucket, got %q", u.Path)
	}

	cfgPath := testConfig(base)
	cfgPath.PathStyle = true
	cPath, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	uPath := cPath.objectURL("aa/deadbeef")
	if uPath.Host != "eu2.contabostorage.com" {
		t.Fatalf("path-style host = %q, want eu2.contabostorage.com", uPath.Host)
	}
	if uPath.Path != "/test-bucket/aa/deadbeef" {
		t.Fatalf("path-style path = %q, want /test-bucket/aa/deadbeef", uPath.Path)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(config.S3{}); err == nil {
		t.Fatal("empty config must be rejected")
	}
	if _, err := New(testConfig("contabostorage.com")); err == nil {
		t.Fatal("endpoint without scheme must be rejected")
	}
	if _, err := New(testConfig("https://eu2.contabostorage.com")); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestSignDeterminism(t *testing.T) {
	// Same inputs must yield the same signature (guards against clock/format drift).
	cfg := testConfig("https://eu2.contabostorage.com")
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	r1, _ := http.NewRequest(http.MethodGet, "https://test-bucket.eu2.contabostorage.com/aa/deadbeef", nil)
	c.sign(r1, emptyPayloadSHA256, now)
	r2, _ := http.NewRequest(http.MethodGet, "https://test-bucket.eu2.contabostorage.com/aa/deadbeef", nil)
	c.sign(r2, emptyPayloadSHA256, now)
	a1 := r1.Header.Get("Authorization")
	a2 := r2.Header.Get("Authorization")
	if a1 != a2 {
		t.Fatalf("signing not deterministic:\n%s\n%s", a1, a2)
	}
	if !strings.Contains(a1, "eu2/s3/aws4_request") {
		t.Fatalf("scope missing from %q", a1)
	}
}
