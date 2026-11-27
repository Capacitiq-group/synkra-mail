// Package mimeutil: MIME parse (via enmime — Section 3.1 "adopt a library") and
// outbound message building. Our policy lives here; the library never decides.
package mimeutil

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jhillyerd/enmime"
)

// Parsed is the ingest-time extraction (Section 4.4: computed once at receive).
type Parsed struct {
	MessageID   string
	InReplyTo   string
	Refs        []string
	Subject     string
	FromEmail   string
	FromName    string
	To          []Addr
	Cc          []Addr
	Text        string
	HTML        string
	Attachments []Att
	SizeBytes   int64
	Date        time.Time
}

// HasAttachments reports whether the message carries real attachments
// (inlines are not attachments).
func (p *Parsed) HasAttachments() bool {
	for _, a := range p.Attachments {
		if !a.IsInline {
			return true
		}
	}
	return false
}

type Addr struct {
	Name  string
	Email string
}

type Att struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Content     []byte `json:"content"`
	IsInline    bool   `json:"is_inline"`
	ContentID   string `json:"content_id"`
}

// Parse decodes an RFC 822 message.
func Parse(raw []byte) (*Parsed, error) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("mime parse: %w", err)
	}
	p := &Parsed{
		MessageID: env.GetHeader("Message-Id"),
		InReplyTo: env.GetHeader("In-Reply-To"),
		Subject:   env.GetHeader("Subject"),
		SizeBytes: int64(len(raw)),
	}
	p.Refs = parseReferences(env.GetHeader("References"))
	if from, err := env.AddressList("From"); err == nil && len(from) > 0 {
		p.FromEmail = from[0].Address
		p.FromName = from[0].Name
	}
	if to, err := env.AddressList("To"); err == nil {
		for _, a := range to {
			p.To = append(p.To, Addr{Name: a.Name, Email: a.Address})
		}
	}
	if cc, err := env.AddressList("Cc"); err == nil {
		for _, a := range cc {
			p.Cc = append(p.Cc, Addr{Name: a.Name, Email: a.Address})
		}
	}
	p.Text = strings.TrimSpace(env.Text)
	p.HTML = strings.TrimSpace(env.HTML)

	for _, a := range env.Inlines {
		p.Attachments = append(p.Attachments, Att{
			Filename: a.FileName, ContentType: a.ContentType,
			Content: a.Content, IsInline: true, ContentID: strings.Trim(a.ContentID, "<>"),
		})
	}
	for _, a := range env.Attachments {
		p.Attachments = append(p.Attachments, Att{
			Filename: a.FileName, ContentType: a.ContentType,
			Content: a.Content, IsInline: false,
		})
	}
	if d, derr := env.Date(); derr == nil && !d.IsZero() {
		p.Date = d
	} else {
		p.Date = time.Now()
	}
	return p, nil
}

func parseReferences(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	fields := strings.FieldsFunc(v, func(r rune) bool { return r == '<' || r == '>' || r == ' ' || r == '\t' || r == '\r' || r == '\n' })
	var out []string
	for _, f := range fields {
		if strings.TrimSpace(f) != "" {
			out = append(out, strings.TrimSpace(f))
		}
	}
	return out
}

// NormalizeSubject strips Re:/Fwd: prefixes for threading (Section 4.2).
var subjPrefix = regexp.MustCompile(`(?i)^\s*((re|fwd?|aw|sv)(\[\d+\])?\s*:\s*)+`)

// NormalizeWire rewrites bare LF line endings to CRLF (RFC 5321 2.3.8: mail
// data is SMTP-normalised before storage or relay). go-smtp passes DATA bytes
// through unmodified, so a client sending bare LF would otherwise store and
// relay it verbatim (build register 3.7). Bare CR bytes are left untouched.
func NormalizeWire(raw []byte) []byte {
	if bytes.IndexByte(raw, '\n') < 0 {
		return raw
	}
	step := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(step, []byte("\n"), []byte("\r\n"))
}

func NormalizeSubject(s string) string {
	s = strings.TrimSpace(s)
	for subjPrefix.MatchString(s) {
		s = subjPrefix.ReplaceAllString(s, "")
	}
	return strings.TrimSpace(s)
}

// Snippet: first ~200 chars of text body, whitespace collapsed.
func Snippet(text string) string {
	s := strings.Join(strings.Fields(text), " ")
	r := []rune(s)
	if len(r) > 200 {
		r = r[:200]
	}
	return string(r)
}

// ---- outbound building ----

// ComposeInput describes an outbound message to build.
type ComposeInput struct {
	From        Addr
	To          []Addr
	Cc          []Addr
	Bcc         []Addr // envelope only, never a header
	Subject     string
	Text        string
	HTML        string
	InReplyTo   string
	References  []string
	Attachments []Att
	Date        time.Time

	// MessageIDDomain is the domain of the minted Message-ID. Empty: derived
	// from the From address's domain; no domain there either: "localhost".
	MessageIDDomain string
}

// Build renders an RFC 822 message (quoted-printable text, multipart when
// attachments/HTML exist). Message-ID is minted here and returned.
func Build(in ComposeInput) (raw []byte, messageID string, err error) {
	if in.Date.IsZero() {
		in.Date = time.Now()
	}
	messageID = NewMessageID(messageIDDomain(in))
	h := textproto.MIMEHeader{}
	h.Set("From", formatAddr(in.From))
	// A message with no visible recipients (Bcc only, Section 3.6.3) carries
	// no To header — an empty one would be invalid.
	if len(in.To) > 0 {
		h.Set("To", formatAddrList(in.To))
	}
	if len(in.Cc) > 0 {
		h.Set("Cc", formatAddrList(in.Cc))
	}
	h.Set("Subject", mime.QEncoding.Encode("utf-8", in.Subject))
	h.Set("Date", in.Date.Format(time.RFC1123Z))
	h.Set("Message-ID", messageID)
	h.Set("MIME-Version", "1.0")
	if in.InReplyTo != "" {
		h.Set("In-Reply-To", ensureAngle(in.InReplyTo))
	}
	if len(in.References) > 0 {
		refs := make([]string, 0, len(in.References))
		for _, r := range in.References {
			refs = append(refs, ensureAngle(r))
		}
		h.Set("References", strings.Join(refs, " "))
	}
	h.Set("X-Mailer", "Synkra-Email/1.0")

	hasAttachments := false
	for _, a := range in.Attachments {
		if !a.IsInline {
			hasAttachments = true
			break
		}
	}

	var buf bytes.Buffer

	if !hasAttachments && len(in.HTML) == 0 {
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		if err := writeHeader(&buf, h); err != nil {
			return nil, "", err
		}
		qp := quotedprintable.NewWriter(&buf)
		if _, err := qp.Write([]byte(in.Text)); err != nil {
			return nil, "", err
		}
		if err := qp.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), messageID, nil
	}

	boundary := "synkra-" + uuid.NewString()
	h.Set("Content-Type", fmt.Sprintf("multipart/mixed; boundary=%q", boundary))
	if err := writeHeader(&buf, h); err != nil {
		return nil, "", err
	}

	mw := multipart.NewWriter(&buf)
	mw.SetBoundary(boundary)

	// Body part: multipart/alternative when HTML present, else plain text part.
	if len(in.HTML) > 0 {
		altBoundary := "synkra-alt-" + uuid.NewString()
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Type", fmt.Sprintf("multipart/alternative; boundary=%q", altBoundary))
		pw, err := mw.CreatePart(hdr)
		if err != nil {
			return nil, "", err
		}
		altW := multipart.NewWriter(pw)
		altW.SetBoundary(altBoundary)
		ph := textproto.MIMEHeader{}
		ph.Set("Content-Type", "text/plain; charset=utf-8")
		ph.Set("Content-Transfer-Encoding", "quoted-printable")
		p1, _ := altW.CreatePart(ph)
		q1 := quotedprintable.NewWriter(p1)
		q1.Write([]byte(in.Text))
		q1.Close()
		ph2 := textproto.MIMEHeader{}
		ph2.Set("Content-Type", "text/html; charset=utf-8")
		ph2.Set("Content-Transfer-Encoding", "quoted-printable")
		p2, _ := altW.CreatePart(ph2)
		q2 := quotedprintable.NewWriter(p2)
		q2.Write([]byte(in.HTML))
		q2.Close()
		altW.Close()
	} else {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Type", "text/plain; charset=utf-8")
		hdr.Set("Content-Transfer-Encoding", "quoted-printable")
		pw, err := mw.CreatePart(hdr)
		if err != nil {
			return nil, "", err
		}
		qp := quotedprintable.NewWriter(pw)
		qp.Write([]byte(in.Text))
		qp.Close()
	}

	for _, a := range in.Attachments {
		if a.IsInline {
			continue
		}
		hdr := textproto.MIMEHeader{}
		if a.ContentType != "" {
			hdr.Set("Content-Type", a.ContentType)
		} else {
			hdr.Set("Content-Type", "application/octet-stream")
		}
		hdr.Set("Content-Transfer-Encoding", "base64")
		hdr.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", a.Filename))
		pw, err := mw.CreatePart(hdr)
		if err != nil {
			return nil, "", err
		}
		if _, err := pw.Write(base64Lines(a.Content)); err != nil {
			return nil, "", err
		}
	}

	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), messageID, nil
}

// headerOrder fixes the wire order of our own headers: deterministic output
// for tests, DKIM and spam filters. Keys are canonical MIME header names;
// anything not listed is appended after them, alphabetically.
var headerOrder = []string{
	"From", "To", "Cc", "Subject", "Date", "Message-Id", "In-Reply-To",
	"References", "Mime-Version", "Content-Type", "Content-Transfer-Encoding",
	"Content-Disposition", "Content-Id", "X-Mailer",
}

func headerRank(k string) int {
	k = textproto.CanonicalMIMEHeaderKey(k)
	for i, name := range headerOrder {
		if name == k {
			return i
		}
	}
	return len(headerOrder)
}

func writeHeader(buf *bytes.Buffer, h textproto.MIMEHeader) error {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri, rj := headerRank(keys[i]), headerRank(keys[j])
		if ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		for _, v := range h[k] {
			if _, err := fmt.Fprintf(buf, "%s: %s\r\n", k, v); err != nil {
				return err
			}
		}
	}
	buf.WriteString("\r\n")
	return nil
}

func base64Lines(b []byte) []byte {
	const lineLen = 76
	var out bytes.Buffer
	for len(b) > 0 {
		n := lineLen / 4 * 3
		if n > len(b) {
			n = len(b)
		}
		out.WriteString(base64.StdEncoding.EncodeToString(b[:n]))
		out.WriteString("\r\n")
		b = b[n:]
	}
	return out.Bytes()
}

// addrSpecials: characters that change header parsing (RFC 5322 3.2.3);
// a display name containing one must be quoted.
const addrSpecials = `()<>[]:;@\,.">`

func formatAddr(a Addr) string {
	if a.Name == "" {
		return a.Email
	}
	// Header values are single lines: folded whitespace becomes spaces.
	name := strings.Join(strings.Fields(a.Name), " ")
	if name == "" {
		return a.Email
	}
	ascii := true
	for _, r := range name {
		if r < 0x20 || r > 0x7e {
			ascii = false
			break
		}
	}
	if !ascii {
		return fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", name), a.Email)
	}
	if strings.ContainsAny(name, addrSpecials) {
		return fmt.Sprintf("%q <%s>", name, a.Email)
	}
	return fmt.Sprintf("%s <%s>", name, a.Email)
}

func formatAddrList(l []Addr) string {
	if len(l) == 0 {
		return ""
	}
	parts := make([]string, 0, len(l))
	for _, a := range l {
		parts = append(parts, formatAddr(a))
	}
	return strings.Join(parts, ", ")
}

func ensureAngle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if !strings.HasPrefix(s, "<") {
		s = "<" + s
	}
	if !strings.HasSuffix(s, ">") {
		s = s + ">"
	}
	return s
}

// NewMessageID mints an RFC 5322 msg-id for the given domain.
func NewMessageID(domain string) string {
	return fmt.Sprintf("<%s@%s>", uuid.NewString(), domain)
}

// messageIDDomain resolves the Message-ID domain: the explicit override, else
// the From address's domain, else localhost (nothing leaves without one).
func messageIDDomain(in ComposeInput) string {
	if in.MessageIDDomain != "" {
		return in.MessageIDDomain
	}
	if i := strings.LastIndex(in.From.Email, "@"); i > 0 && i < len(in.From.Email)-1 {
		return in.From.Email[i+1:]
	}
	return "localhost"
}

// ---- header patching (the submission path keeps the client's message) ----

// HasHeader reports whether raw already carries a header with that name
// (case-insensitive; folded continuation lines are skipped).
func HasHeader(raw []byte, name string) bool {
	nameL := strings.ToLower(name)
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 {
			break // header block ended
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue // continuation
		}
		if i := bytes.IndexByte(line, ':'); i > 0 && strings.EqualFold(string(line[:i]), nameL) {
			return true
		}
	}
	return false
}

// Header is one name/value pair for AddMissingHeaders.
type Header struct {
	Name  string
	Value string
}

// AddMissingHeaders prepends headers the client omitted. Nothing the client
// wrote is rewritten, and values are stripped of CR/LF (header injection).
func AddMissingHeaders(raw []byte, headers ...Header) []byte {
	if len(headers) == 0 {
		return raw
	}
	var b bytes.Buffer
	for _, h := range headers {
		v := strings.Join(strings.Fields(strings.ReplaceAll(h.Value, "\r", " ")), " ")
		if v == "" {
			continue
		}
		b.WriteString(h.Name)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	b.Write(raw)
	return b.Bytes()
}

// ---- address validation (RFC 5322 3.4.1 dot-atom, RFC 5321 4.1.2/4.1.3) ----

var literalRe = regexp.MustCompile(`^(?:\[[0-9]{1,3}(?:\.[0-9]{1,3}){3}\]|\[IPv6:[0-9A-Fa-f:.]{2,39}\])$`)

// isAtext reports whether r may appear in an atom (RFC 5322 3.2.3 atext).
func isAtext(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return true
	}
	return strings.ContainsRune("!#$%&'*+-/=?^_`{|}~", r)
}

// ValidAddress accepts exactly what we will put on the wire: a dot-atom local
// part, a dotted domain or an address literal, 254 octets overall, local part
// at most 64. Display names, folding whitespace and control characters are
// rejected outright.
func ValidAddress(s string) bool {
	if len(s) > 254 {
		return false
	}
	i := strings.LastIndexByte(s, '@')
	if i <= 0 || i > 64 || i == len(s)-1 {
		return false
	}
	// A dotted local part (first.last+tag) is fine; a dot may not lead,
	// trail or double up.
	local := s[:i]
	for n := 0; n < len(local); n++ {
		c := local[n]
		if c == '.' {
			if n == 0 || n == len(local)-1 || local[n-1] == '.' {
				return false
			}
			continue
		}
		if !isAtext(rune(c)) {
			return false
		}
	}
	domain := s[i+1:]
	if strings.HasPrefix(domain, "[") {
		return literalRe.MatchString(domain)
	}
	labels := strings.Split(domain, ".")
	top := len(labels) - 1
	for n, label := range labels {
		if label == "" || len(label) > 63 {
			return false
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' {
				continue
			}
			if r >= '0' && r <= '9' {
				if n == top {
					return false // all-numeric TLD
				}
				continue
			}
			return false
		}
	}
	return top >= 1 && len(labels[top]) >= 2
}
