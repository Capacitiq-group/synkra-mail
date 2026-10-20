// E2E harness helpers: raw SMTP dialogues + shared fixtures.
package e2e

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"strings"
	"time"

	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

func getenv(k string) string { return os.Getenv(k) }

func tTemp() string {
	d, err := os.MkdirTemp("", "synkra-blobs-*")
	if err != nil {
		panic(err)
	}
	return d
}

func hashPassword(pw string) string { return krypto.HashPassword(pw) }

// smtpDialogue runs a minimal SMTP conversation and returns the final response
// code to DATA (or to RCPT when the dialogue fails early).
func smtpDialogue(addr, mailFrom, rcptTo, data string) (int, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	r := textproto.NewReader(bufio.NewReader(conn))
	if _, err := expectCode(r, 220); err != nil {
		return 0, err
	}
	conn.Write([]byte("EHLO test.synkra.email\r\n"))
	if err := readEHLO(r); err != nil {
		return 0, err
	}
	conn.Write([]byte("MAIL FROM:<" + mailFrom + ">\r\n"))
	code, err := expectCode(r, 250)
	if err != nil {
		return code, fmt.Errorf("mail from: %w", err)
	}
	conn.Write([]byte("RCPT TO:<" + rcptTo + ">\r\n"))
	code, err = expectCode(r, -1)
	if err != nil || code != 250 {
		// Early rejection at RCPT: report its code (550/554/...).
		return code, fmt.Errorf("rcpt: code=%d err=%v", code, err)
	}
	conn.Write([]byte("DATA\r\n"))
	if code, err := expectCode(r, 354); err != nil {
		return code, fmt.Errorf("data cmd: %w", err)
	}
	dotted := strings.ReplaceAll(data, "\r\n.", "\r\n..")
	conn.Write([]byte(dotted + "\r\n.\r\n"))
	code, err = expectCode(r, -1)
	if err != nil {
		return code, err
	}
	conn.Write([]byte("QUIT\r\n"))
	return code, nil
}

// smtpSubmission: EHLO + AUTH PLAIN + MAIL/RCPT/DATA.
func smtpSubmission(addr, username, secret, mailFrom, rcptTo, data string) (int, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	r := textproto.NewReader(bufio.NewReader(conn))
	if _, err := expectCode(r, 220); err != nil {
		return 0, err
	}
	conn.Write([]byte("EHLO client.example\r\n"))
	if err := readEHLO(r); err != nil {
		return 0, err
	}
	auth := base64.StdEncoding.EncodeToString([]byte("\x00" + username + "\x00" + secret))
	conn.Write([]byte("AUTH PLAIN " + auth + "\r\n"))
	code, err := expectCode(r, -1)
	if err != nil || code != 235 {
		return code, fmt.Errorf("auth: code=%d err=%v", code, err)
	}
	conn.Write([]byte("MAIL FROM:<" + mailFrom + ">\r\n"))
	if code, err := expectCode(r, 250); err != nil {
		return code, fmt.Errorf("mail from: %w", err)
	}
	conn.Write([]byte("RCPT TO:<" + rcptTo + ">\r\n"))
	if code, err := expectCode(r, 250); err != nil {
		return code, fmt.Errorf("rcpt: %w", err)
	}
	conn.Write([]byte("DATA\r\n"))
	if code, err := expectCode(r, 354); err != nil {
		return code, fmt.Errorf("data: %w", err)
	}
	dotted := strings.ReplaceAll(data, "\r\n.", "\r\n..")
	conn.Write([]byte(dotted + "\r\n.\r\n"))
	code, err = expectCode(r, -1)
	if err != nil {
		return code, err
	}
	conn.Write([]byte("QUIT\r\n"))
	return code, nil
}

// readEHLO swallows the multiline EHLO response.
func readEHLO(r *textproto.Reader) error {
	for {
		line, err := r.ReadLine()
		if err != nil {
			return err
		}
		if len(line) < 4 || line[3] != '-' {
			return nil
		}
	}
}

// expectCode parses "250-..." continuation lines; want=-1 accepts any final code.
func expectCode(r *textproto.Reader, want int) (int, error) {
	for {
		line, err := r.ReadLine()
		if err != nil {
			return 0, err
		}
		if len(line) < 3 {
			return 0, fmt.Errorf("short smtp line: %q", line)
		}
		var code int
		if _, err := fmt.Sscanf(line[:3], "%d", &code); err != nil {
			return 0, fmt.Errorf("bad smtp line: %q", line)
		}
		if len(line) > 3 && line[3] == '-' {
			continue
		}
		if want > 0 && code != want {
			return code, fmt.Errorf("expected %d, got %d", want, code)
		}
		return code, nil
	}
}

// staticTransport: a configurable fake for the breaker test.
type staticTransport struct {
	code int
	msg  string
}

func (t *staticTransport) Deliver(ctx context.Context, envFrom, rcptTo string, raw []byte) (int, string, error) {
	return t.code, t.msg, fmt.Errorf("static %d %s", t.code, t.msg)
}

// ownMailbox finds the user's own user-kind mailbox.
func (s *stack) ownMailbox(user *model.User) (*model.Mailbox, error) {
	mbs, err := store.MailboxesForUser(context.Background(), s.st.Pool, user.ID)
	if err != nil {
		return nil, err
	}
	for i := range mbs {
		mb := &mbs[i]
		if mb.Kind == "user" && mb.OwnerUserID != nil && *mb.OwnerUserID == user.ID {
			return mb, nil
		}
	}
	return nil, fmt.Errorf("no own mailbox")
}

// primaryAddressFor resolves the user's primary address (mirrors the API default).
func (s *stack) primaryAddressFor(user *model.User) (string, error) {
	mb, err := s.ownMailbox(user)
	if err != nil {
		return "", err
	}
	addrs, err := store.ListAddressesByOrg(context.Background(), s.st.Pool, user.OrgID)
	if err != nil {
		return "", err
	}
	for _, a := range addrs {
		if a.MailboxID == mb.ID {
			var dname string
			if err := s.st.Pool.QueryRow(context.Background(), `select name from mail.domain where id=$1`, a.DomainID).Scan(&dname); err != nil {
				return "", err
			}
			return string(a.Local) + "@" + dname, nil
		}
	}
	return "", fmt.Errorf("no address for mailbox")
}
