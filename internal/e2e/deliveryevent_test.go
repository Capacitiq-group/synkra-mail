// Phase 5 of the consolidated build register (§7.3, decision 17.4): the
// delivery event log — the per-message state machine, written in the same
// transactions that move the jobs.
package e2e

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"synkra.email/mail/internal/store"
)

func eventStates(t *testing.T, s *stack, messageID uuid.UUID) []string {
	t.Helper()
	events, err := store.DeliveryEventsForMessage(context.Background(), s.st.Pool, messageID)
	if err != nil {
		t.Fatalf("delivery events: %v", err)
	}
	var out []string
	for _, e := range events {
		out = append(out, e.State)
	}
	return out
}

func TestDeliveryEventLog(t *testing.T) {
	s := boot(t)
	_, user, login, pw := s.provisionTenant(t, "eventlog")
	token := s.login(t, login, pw)
	primary, err := s.primaryAddressFor(user)
	if err != nil {
		t.Fatalf("primary: %v", err)
	}

	// Delivered: to our own address, the loopback delivers immediately.
	st, b1, _ := s.apiReq(t, "POST", "/api/v1/send", token,
		map[string]any{"to": []map[string]string{{"email": primary}}, "subject": "event delivered", "text": "x"}, nil)
	if st != 202 {
		t.Fatalf("send: %d %v", st, b1)
	}
	msgID1, _ := b1["message_id"].(string)

	// Bounced: an unhosted domain is a 554 hard bounce on the loopback.
	st, b2, _ := s.apiReq(t, "POST", "/api/v1/send", token,
		map[string]any{"to": []map[string]string{{"email": "gone@outside.test"}}, "subject": "event bounce", "text": "x"}, nil)
	if st != 202 {
		t.Fatalf("send: %d %v", st, b2)
	}
	msgID2, _ := b2["message_id"].(string)

	s.drainWorker(t)

	id1, _ := uuid.Parse(stripPrefix(msgID1, "msg"))
	got := eventStates(t, s, id1)
	if len(got) < 2 || got[0] != "queued" || got[len(got)-1] != "delivered" {
		t.Fatalf("delivered journey = %v, want queued…delivered", got)
	}
	id2, _ := uuid.Parse(stripPrefix(msgID2, "msg"))
	got = eventStates(t, s, id2)
	if len(got) < 2 || got[0] != "queued" || got[len(got)-1] != "bounced" {
		t.Fatalf("bounced journey = %v, want queued…bounced", got)
	}

	// The events carry the org and mailbox scope (dashboards read by them).
	events, err := store.DeliveryEventsForMessage(context.Background(), s.st.Pool, id1)
	if err != nil || len(events) == 0 {
		t.Fatalf("events: %v", err)
	}
	if events[0].OrgID != user.OrgID {
		t.Fatalf("event org scope wrong: %v", events[0].OrgID)
	}
}

func stripPrefix(s, prefix string) string {
	if len(s) > len(prefix)+1 && s[:len(prefix)+1] == prefix+"_" {
		return s[len(prefix)+1:]
	}
	return s
}
