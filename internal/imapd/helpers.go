// Header/date parsing helpers for SEARCH + ENVELOPE extraction.
package imapd

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"time"

	gotextproto "github.com/emersion/go-message/textproto"
	"github.com/google/uuid"
)

func parseID(s string) (uuid.UUID, bool) {
	if i := strings.IndexByte(s, '_'); i >= 0 {
		s = s[i+1:]
	}
	u, err := uuid.Parse(s)
	return u, err == nil
}

// parseHeader parses the RFC822 header block with go-message's textproto
// (folded-header aware — Section 3.1: never hand-roll MIME codecs).
func parseHeader(raw []byte) (gotextproto.Header, error) {
	br := bufio.NewReader(bytes.NewReader(raw))
	return gotextproto.ReadHeader(br)
}

func headerDate(hdr gotextproto.Header) (time.Time, error) {
	d := hdr.Get("Date")
	if d == "" {
		return time.Time{}, errors.New("no date")
	}
	return time.Parse(time.RFC1123Z, d)
}
