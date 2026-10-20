package mimeutil

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseSimpleText(t *testing.T) {
	raw := []byte("From: Alice <alice@example.com>\r\n" +
		"To: Bob <bob@dest.org>\r\n" +
		"Subject: Hello there\r\n" +
		"Message-ID: <m1@example.com>\r\n" +
		"Date: Mon, 20 Oct 2025 10:00:00 +0000\r\n" +
		"\r\n" +
		"Line one.\r\nLine two with more words to make a snippet.\r\n")
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.FromEmail != "alice@example.com" || p.FromName != "Alice" {
		t.Fatalf("from: %+v", p.FromEmail)
	}
	if len(p.To) != 1 || p.To[0].Email != "bob@dest.org" {
		t.Fatalf("to: %+v", p.To)
	}
	if p.Subject != "Hello there" {
		t.Fatalf("subject: %q", p.Subject)
	}
	if p.MessageID != "<m1@example.com>" {
		t.Fatalf("message-id: %q", p.MessageID)
	}
	if !strings.Contains(p.Text, "Line two") {
		t.Fatalf("text: %q", p.Text)
	}
}

func TestParseMultipartWithAttachment(t *testing.T) {
	raw := []byte("From: a@b.c\r\nTo: d@e.f\r\nSubject: att\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"XYZ\"\r\n\r\n" +
		"--XYZ\r\nContent-Type: text/plain\r\n\r\nbody text here\r\n" +
		"--XYZ\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"report.pdf\"\r\n\r\n" +
		"%PDF-fake-content\r\n--XYZ--\r\n")
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(p.Text, "body text") {
		t.Fatalf("text missing: %q", p.Text)
	}
	if len(p.Attachments) != 1 {
		t.Fatalf("attachments: %d", len(p.Attachments))
	}
	a := p.Attachments[0]
	if a.Filename != "report.pdf" || a.ContentType != "application/pdf" || len(a.Content) == 0 {
		t.Fatalf("attachment: %+v", a)
	}
	if !p.HasAttachments() {
		t.Fatal("HasAttachments should be true")
	}
}

func TestBuildRoundtrip(t *testing.T) {
	in := ComposeInput{
		From:    Addr{Email: "from@x.y", Name: "From Person"},
		To:      []Addr{{Email: "to@a.b", Name: "To Person"}},
		Cc:      []Addr{{Email: "cc@a.b"}},
		Subject: "Round trip: naïve résumé",
		Text:    "Line 1\nLine 2 with unicode ✓",
	}
	raw, msgID, err := Build(in)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if msgID == "" || !strings.HasPrefix(msgID, "<") {
		t.Fatalf("message id: %q", msgID)
	}
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse built: %v", err)
	}
	if p.FromEmail != "from@x.y" {
		t.Fatalf("from: %q", p.FromEmail)
	}
	if len(p.To) != 1 || p.To[0].Email != "to@a.b" {
		t.Fatalf("to: %+v", p.To)
	}
	if !strings.Contains(p.Subject, "Round trip") {
		t.Fatalf("subject: %q", p.Subject)
	}
	if !strings.Contains(p.Text, "Line 2 with unicode") {
		t.Fatalf("text lost: %q", p.Text)
	}
}

func TestBuildWithAttachmentAndHTML(t *testing.T) {
	in := ComposeInput{
		From:    Addr{Email: "f@x.y"},
		To:      []Addr{{Email: "t@x.y"}},
		Subject: "html+att",
		Text:    "plain",
		HTML:    "<p>rich</p>",
		Attachments: []Att{
			{Filename: "data.bin", ContentType: "application/octet-stream", Content: bytes.Repeat([]byte{7}, 2000)},
		},
	}
	raw, _, err := Build(in)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(p.HTML, "rich") {
		t.Fatalf("html: %q", p.HTML)
	}
	if len(p.Attachments) != 1 || len(p.Attachments[0].Content) != 2000 {
		t.Fatalf("attachment content: %d parts", len(p.Attachments))
	}
}

func TestNormalizeSubject(t *testing.T) {
	cases := map[string]string{
		"Re: Hello":            "Hello",
		"RE: RE: Fwd: Hello":   "Hello",
		"Fwd: Re[2]: Thing":    "Thing",
		"No prefix":            "No prefix",
		"  Re:   Spaced  Out ": "Spaced  Out",
	}
	for in, want := range cases {
		if got := NormalizeSubject(in); got != want {
			t.Fatalf("NormalizeSubject(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSnippet(t *testing.T) {
	long := strings.Repeat("word ", 100)
	s := Snippet(long)
	if len([]rune(s)) > 200 {
		t.Fatalf("snippet too long: %d", len(s))
	}
	if Snippet("a\n\n b\t c") != "a b c" {
		t.Fatalf("snippet collapse: %q", Snippet("a\n\n b\t c"))
	}
}
