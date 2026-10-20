// Package events: Postgres LISTEN/NOTIFY fan-out hub. The ingest trigger
// (mail.notify_item_change) publishes mailbox changes; IMAP IDLE watchers and
// webmail long-poll subscribe here. No polling loops (Section 6.2).
package events

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// ItemChange describes one mailbox mutation.
type ItemChange struct {
	MailboxID string
	FolderID  string
	Kind      string // append | flags | expunge
	UID       uint32
}

type Subscriber struct {
	MailboxID string
	ch        chan ItemChange
}

func (s *Subscriber) C() <-chan ItemChange { return s.ch }

// Hub multiplexes one pg LISTEN connection to many in-process subscribers.
type Hub struct {
	mu          sync.Mutex
	byMailbox   map[string]map[*Subscriber]struct{}
	startedAt   time.Time
	connectedAt time.Time
}

func NewHub() *Hub {
	return &Hub{byMailbox: map[string]map[*Subscriber]struct{}{}}
}

// Run consumes notifications until ctx is done. Reconnects on failure.
func (h *Hub) Run(ctx context.Context, connCfg string) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := h.listenOnce(ctx, connCfg)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(2 * time.Second) // reconnect backoff
		_ = err
	}
}

func (h *Hub) listenOnce(ctx context.Context, connCfg string) error {
	conn, err := pgx.Connect(ctx, connCfg)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "listen mail_events"); err != nil {
		return err
	}
	h.mu.Lock()
	h.connectedAt = time.Now()
	h.mu.Unlock()

	for {
		raw, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		ch := parsePayload(raw.Payload)
		if ch == nil {
			continue
		}
		h.dispatch(*ch)
	}
}

func parsePayload(p string) *ItemChange {
	// payload: mailboxID:kind:folderID:uid
	parts := strings.Split(p, ":")
	if len(parts) != 4 {
		return nil
	}
	uid, _ := strconv.ParseUint(parts[3], 10, 32)
	return &ItemChange{
		MailboxID: parts[0],
		Kind:      parts[1],
		FolderID:  parts[2],
		UID:       uint32(uid),
	}
}

func (h *Hub) dispatch(ch ItemChange) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.byMailbox[strings.ToLower(ch.MailboxID)]
	for s := range subs {
		select {
		case s.ch <- ch:
		default: // slow consumer: drop; Poll will catch up
		}
	}
}

// Subscribe registers a watcher for a mailbox.
func (h *Hub) Subscribe(mailboxID string) *Subscriber {
	s := &Subscriber{MailboxID: strings.ToLower(mailboxID), ch: make(chan ItemChange, 32)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byMailbox[s.MailboxID] == nil {
		h.byMailbox[s.MailboxID] = map[*Subscriber]struct{}{}
	}
	h.byMailbox[s.MailboxID][s] = struct{}{}
	return s
}

func (h *Hub) Unsubscribe(s *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m := h.byMailbox[s.MailboxID]; m != nil {
		delete(m, s)
		if len(m) == 0 {
			delete(h.byMailbox, s.MailboxID)
		}
	}
}
