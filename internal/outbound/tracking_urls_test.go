package outbound

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPixelURLSigRoundtrip(t *testing.T) {
	key := []byte("track-secret")
	id := uuid.New()
	u := PixelURL("http://127.0.0.1:8081", key, id)
	if !strings.Contains(u, "/o/"+id.String()+".gif?s=") {
		t.Fatalf("pixel url shape: %q", u)
	}
	sig := u[strings.LastIndex(u, "=s=")+3:]
	if strings.HasSuffix(sig, "=") {
		t.Fatalf("sig parsing broken: %q", u)
	}
	sigPart := u[strings.Index(u, "?s=")+3:]
	if !VerifyPixelSig(key, id.String(), sigPart) {
		t.Fatal("pixel sig must verify")
	}
	if VerifyPixelSig([]byte("other"), id.String(), sigPart) {
		t.Fatal("pixel sig must fail with other key")
	}
	if VerifyPixelSig(key, uuid.New().String(), sigPart) {
		t.Fatal("pixel sig must fail for other pixel")
	}
}

func TestClickURLSigRoundtrip(t *testing.T) {
	key := []byte("track-secret")
	id := uuid.New()
	target := "https://example.com/page?a=1&b=2"
	u := ClickURL("http://127.0.0.1:8081", key, id, target)
	if !strings.Contains(u, "/c/"+id.String()+"?u=") {
		t.Fatalf("click url shape: %q", u)
	}
	sigPart := u[strings.LastIndex(u, "&s=")+3:]
	if !VerifyClickSig(key, id.String(), target, sigPart) {
		t.Fatal("click sig must verify")
	}
	if VerifyClickSig(key, id.String(), "https://evil.com", sigPart) {
		t.Fatal("click sig must fail for tampered target")
	}
}

func TestRewriteLinks(t *testing.T) {
	html := `<p><a href="https://a.com/x">A</a> <a href="mailto:hi@there.com">Mail</a> <a href="#top">Top</a> <a href="https://b.com/y?q=1">B</a></p>`
	seen := map[string]string{}
	out := RewriteLinks(html, func(target string) string {
		seen[target] = "TRACKED(" + target + ")"
		return "TRACKED(" + target + ")"
	})
	if _, ok := seen["https://a.com/x"]; !ok {
		t.Fatal("https link not rewritten")
	}
	if _, ok := seen["https://b.com/y?q=1"]; !ok {
		t.Fatal("query link not rewritten")
	}
	if len(seen) != 2 {
		t.Fatalf("unexpected rewrite set: %v", seen)
	}
	if !strings.Contains(out, `href="mailto:hi@there.com"`) {
		t.Fatal("mailto must not be rewritten")
	}
	if !strings.Contains(out, `href="#top"`) {
		t.Fatal("anchor must not be rewritten")
	}
}

func TestAppendPixelAndReplaceHTMLBody(t *testing.T) {
	raw := []byte("Content-Type: text/html; charset=utf-8\r\n\r\n<p>hello</p>")
	with := AppendPixel(raw, "http://trk/o/abc.gif?s=zz")
	if !strings.Contains(string(with), `src="http://trk/o/abc.gif?s=zz"`) {
		t.Fatalf("pixel missing: %q", with)
	}
	// Replace body: header must survive, body swapped.
	replaced := ReplaceHTMLBody(with, "<p>new</p>")
	if !strings.Contains(string(replaced), "Content-Type: text/html") {
		t.Fatal("header lost in ReplaceHTMLBody")
	}
	if !strings.Contains(string(replaced), "<p>new</p>") {
		t.Fatalf("new body missing: %q", replaced)
	}
	if strings.Contains(string(replaced), "<p>hello</p>") {
		t.Fatal("old body still present")
	}
}
