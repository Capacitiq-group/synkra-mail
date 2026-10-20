package api

import (
	"testing"
	"time"
)

func TestClassifyOpenMatrix(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		ua     string
		repeat bool
		first  time.Time
		has    bool
		intern bool
		want   string
	}{
		{"empty ua", "", false, time.Time{}, false, false, "possibly_automated"},
		{"google proxy", "Mozilla/5.0 (compatible; GoogleImageProxy)", false, time.Time{}, false, false, "possibly_automated"},
		{"bot", "SomeBot/1.0", false, time.Time{}, false, false, "possibly_automated"},
		{"internal human", "Mozilla/5.0 Chrome/120", false, time.Time{}, false, true, "opened"},
		{"external human", "Mozilla/5.0 Chrome/120", false, time.Time{}, false, false, "likely_opened"},
		{"rapid repeat", "Mozilla/5.0 Chrome/120", true, now.Add(-5 * time.Second), true, false, "possibly_automated"},
		{"apple mpp-ish repeat", "Mozilla/5.0 (Macintosh; Intel Mac OS X) mail", true, now.Add(-30 * time.Second), true, false, "possibly_automated"},
		{"old repeat external", "Mozilla/5.0 Chrome/120", true, now.Add(-6 * time.Hour), true, false, "likely_opened"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyOpen(c.ua, c.first, c.has, c.intern)
			if got != c.want {
				t.Fatalf("ClassifyOpen(%q) = %q, want %q", c.ua, got, c.want)
			}
		})
	}
}
