// Tracking URL construction (Section 9.1). URLs are HMAC-signed so the isolated
// tracker can verify them without database credentials; signatures prevent
// enumeration and open-redirect abuse.
package outbound

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"synkra.email/mail/internal/krypto"
)

// PixelURL: https://trk.../o/{pixelID}.gif?s={sig}
func PixelURL(trackerURL string, key []byte, pixelID uuid.UUID) string {
	sig := krypto.HMACSign(key, "o|"+pixelID.String())
	return fmt.Sprintf("%s/o/%s.gif?s=%s", strings.TrimRight(trackerURL, "/"), pixelID, sig)
}

// ClickURL: https://trk.../c/{linkID}?u={b64url(target)}&s={sig}
func ClickURL(trackerURL string, key []byte, linkID uuid.UUID, target string) string {
	payload := "c|" + linkID.String() + "|" + target
	sig := krypto.HMACSign(key, payload)
	u := base64.RawURLEncoding.EncodeToString([]byte(target))
	return fmt.Sprintf("%s/c/%s?u=%s&s=%s", strings.TrimRight(trackerURL, "/"), linkID, u, sig)
}

// VerifyPixelSig checks a pixel request signature.
func VerifyPixelSig(key []byte, pixelID string, sig string) bool {
	return krypto.HMACVerify(key, "o|"+pixelID, sig)
}

// VerifyClickSig checks a click request signature.
func VerifyClickSig(key []byte, linkID, target, sig string) bool {
	return krypto.HMACVerify(key, "c|"+linkID+"|"+target, sig)
}

// RewriteLinks rewrites hrefs in HTML through the click tracker. The callback
// returns the tracked URL for a target.
var hrefRe = regexp.MustCompile(`(?i)href\s*=\s*("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|[^\s>]+)`)

func RewriteLinks(html string, track func(target string) string) string {
	return hrefRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := hrefRe.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		quote := ""
		val := sub[1]
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') {
			quote = string(val[0])
			val = val[1 : len(val)-1]
		}
		lower := strings.ToLower(val)
		// Never rewrite mailto, our own tracker, or empty anchors.
		if val == "" || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "#") ||
			strings.Contains(lower, "/o/") || strings.Contains(lower, "/c/") {
			return m
		}
		return "href=" + quote + track(val) + quote
	})
}

// AppendPixel appends the invisible 1×1 tracking image to the HTML body.
func AppendPixel(raw []byte, pixelURL string) []byte {
	pixel := `<img src="` + pixelURL + `" width="1" height="1" alt="" style="border:0;opacity:0.001" />`
	s := string(raw)
	// Prefer injecting before </body>, else append to the end.
	if i := strings.LastIndex(strings.ToLower(s), "</body>"); i >= 0 {
		return []byte(s[:i] + pixel + s[i:])
	}
	return []byte(s + pixel)
}

// ReplaceHTMLBody swaps the HTML part's content inside a multipart message
// (used after link rewriting). It finds the text/html part by header.
var htmlHeaderRe = regexp.MustCompile(`(?i)content-type:\s*text/html[^\r\n]*(?:\r\n[^\r\n]+)*?\r\n\r\n`)

func ReplaceHTMLBody(raw []byte, newHTML string) []byte {
	loc := htmlHeaderRe.FindIndex(raw)
	if loc == nil {
		return raw
	}
	// The body runs until the next boundary line or EOF.
	rest := raw[loc[1]:]
	end := len(raw)
	if m := boundaryRe.FindIndex(rest); m != nil {
		end = loc[1] + m[0]
	}
	out := make([]byte, 0, len(raw)+len(newHTML))
	out = append(out, raw[:loc[1]]...)
	out = append(out, []byte(newHTML)...)
	out = append(out, raw[end:]...)
	return out
}

var boundaryRe = regexp.MustCompile(`(?m)^\r?\n?--[^;\r\n]+--?\s*(\r\n|$)`)
