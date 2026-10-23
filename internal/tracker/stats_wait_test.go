package tracker

import (
	"testing"
	"time"
)

func TestOwnerWaitsFromLastAgentCallBeforeApproval(t *testing.T) {
	at := func(hm string) time.Time {
		t, err := time.Parse("15:04", hm)
		if err != nil {
			panic(err)
		}
		return time.Date(2026, 10, 23, t.Hour(), t.Minute(), 0, 0, time.UTC)
	}
	ptr := func(v time.Time) *time.Time { return &v }
	store := StatsStore{Transitions: []TransitionStat{
		{From: "idea", To: "in design", TS: at("08:00")},
		// The agent's last call in design is 08:40; the owner approves at 09:30.
		{From: "in design", To: "in progress", TS: at("09:31"), ApprovedAt: ptr(at("09:30"))},
		// Approved while the agent was still working: wait 0.
		{From: "in progress", To: "testing", TS: at("10:30"), ApprovedAt: ptr(at("10:10"))},
		// No agent call in the status: measured from when it was entered.
		{From: "testing", To: "done", TS: at("11:30"), ApprovedAt: ptr(at("11:00"))},
	}}
	calls := []time.Time{at("10:20"), at("08:10"), at("08:40"), at("09:35"), at("10:15"), at("10:05")}

	got := OwnerWaits(store, calls)
	want := []struct {
		to   string
		wait time.Duration
	}{{"in progress", 50 * time.Minute}, {"testing", 5 * time.Minute}, {"done", 30 * time.Minute}}
	if len(got) != len(want) {
		t.Fatalf("got %d waits: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].To != w.to || got[i].Wait != w.wait {
			t.Errorf("wait %d: %s %v, want %s %v", i, got[i].To, got[i].Wait, w.to, w.wait)
		}
	}
	// 10:05 is the last call before the 10:10 approval: 5 minutes, not 0.
	if !got[1].Since.Equal(at("10:05")) {
		t.Errorf("since = %v, want 10:05", got[1].Since)
	}
}

func TestOwnerWaitsSkipsRowsWithoutApprovalOrStart(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	store := StatsStore{Transitions: []TransitionStat{
		{From: "idea", To: "in design", TS: now, ApprovedAt: &now}, // first row, no call
		{From: "in design", To: "backlog", TS: now.Add(time.Hour)},
	}}
	if got := OwnerWaits(store, nil); len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
}
