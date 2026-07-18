package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTerminalContextSurvivesCallerCancellationAndIsBounded(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	ctx, cancel := terminalContext(parent)
	defer cancel()
	if err := ctx.Err(); err != nil {
		t.Fatalf("terminal context inherits cancellation: %v", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > terminalWriteTimeout {
		t.Fatalf("terminal context deadline = %v, ok=%v", deadline, ok)
	}
}

func TestViewTimesMatchNodeISOString(t *testing.T) {
	value := time.Date(2026, 7, 17, 8, 9, 10, 987654321, time.FixedZone("test", 8*60*60))
	encoded, err := json.Marshal(struct {
		Site         SiteView             `json:"site"`
		Checkin      CheckinView          `json:"checkin"`
		Announcement AnnouncementView     `json:"announcement"`
		Sync         AnnouncementSyncView `json:"sync"`
	}{
		Site:         SiteView{CreatedAt: isoTime(value), UpdatedAt: isoTime(value)},
		Checkin:      CheckinView{StartedAt: isoTime(value)},
		Announcement: AnnouncementView{PublishedAt: nullableISOTime(&value), FirstSeenAt: isoTime(value), LastSeenAt: isoTime(value)},
		Sync:         AnnouncementSyncView{StartedAt: isoTime(value)},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := string(encoded)
	if count := strings.Count(payload, `"2026-07-17T00:09:10.987Z"`); count != 7 {
		t.Fatalf("millisecond ISO timestamp count = %d, want 7; payload=%s", count, payload)
	}
	for _, field := range []string{`"finishedAt":null`, `"readAt":null`} {
		if !strings.Contains(payload, field) {
			t.Fatalf("payload missing %s: %s", field, payload)
		}
	}
}
