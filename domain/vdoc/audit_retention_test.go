package vdoc

import (
	"testing"
	"time"
)

func TestAuditRetentionUsesBeijingCalendarDates(t *testing.T) {
	for _, test := range []struct {
		now  string
		days int
		want string
	}{
		{"2026-11-02T17:00:00Z", 1, "2026-11-02T00:00:00+08:00"},
		{"2026-11-03T18:05:00+08:00", 1, "2026-11-02T00:00:00+08:00"},
		{"2026-11-03T18:05:00+08:00", 30, "2026-10-04T00:00:00+08:00"},
		{"2028-03-01T01:00:00+08:00", 1, "2028-02-29T00:00:00+08:00"},
		{"2027-01-01T01:00:00+08:00", 1, "2026-12-31T00:00:00+08:00"},
		{"2026-11-02T17:12:34Z", 0, "2026-11-02T17:12:34Z"},
	} {
		now, err := time.Parse(time.RFC3339, test.now)
		if err != nil {
			t.Fatal(err)
		}
		want, err := time.Parse(time.RFC3339, test.want)
		if err != nil {
			t.Fatal(err)
		}
		if got := AuditRetentionCutoff(now, test.days); !got.Equal(want) {
			t.Errorf("%s days=%d: %s, want %s", test.now, test.days, got, want)
		}
	}
}

func TestNextAuditCleanupIsDailyAtOneBeijingTime(t *testing.T) {
	for _, test := range []struct{ now, want string }{
		{"2026-11-02T16:59:59Z", "2026-11-03T01:00:00+08:00"},
		{"2026-11-02T17:00:00Z", "2026-11-04T01:00:00+08:00"},
		{"2026-11-03T23:59:59+08:00", "2026-11-04T01:00:00+08:00"},
		{"2026-12-31T23:00:00+08:00", "2027-01-01T01:00:00+08:00"},
	} {
		now, err := time.Parse(time.RFC3339, test.now)
		if err != nil {
			t.Fatal(err)
		}
		want, err := time.Parse(time.RFC3339, test.want)
		if err != nil {
			t.Fatal(err)
		}
		if got := NextAuditCleanup(now); !got.Equal(want) {
			t.Errorf("%s: got %s, want %s", test.now, got, want)
		}
	}
}
