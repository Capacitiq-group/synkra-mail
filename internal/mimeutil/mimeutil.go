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
	Filename    string
	ContentType string
	Content     []byte
	IsInline    bool
	ContentID   string
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
}

// Build renders an RFC 822 message (quoted-printable text, multipart when
// attachments/HTML exist). Message-ID is minted here and returned.
func Build(in ComposeInput) (raw []byte, messageID string, err error) {
	if in.Date.IsZero() {
		in.Date = time.Now()
	}
	messageID = fmt.Sprintf("<%s.mail.synkra.co.za>", uuid.New())
	h := textproto.MIMEHeader{}
	h.Set("From", formatAddr(in.From))
	h.Set("To", formatAddrList(in.To))
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

func writeHeader(buf *bytes.Buffer, h textproto.MIMEHeader) error {
	for k, vs := range h {
		for _, v := range vs {
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

func formatAddr(a Addr) string {
	if a.Name == "" {
		return a.Email
	}
	return fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", a.Name), a.Email)
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
