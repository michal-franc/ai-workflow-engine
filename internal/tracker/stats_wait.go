package tracker

import (
	"sort"
	"time"
)

// GateWait is how long the owner held one approval gate.
type GateWait struct {
	From, To string
	// Since is when the agent stopped: the end of its last issue-cli call in
	// From before the approval, or when the issue entered From when the agent
	// made no call there.
	Since      time.Time
	ApprovedAt time.Time
	Wait       time.Duration
}

// OwnerWaits derives "waiting on you" for every transition row that consumed
// an approval: ApprovedAt minus the agent's last call in the row's From status
// before the approval. calls are the end times of the agent's issue-cli calls
// (clilog Record.End), in any order. Nothing is recorded for this: an agent
// that asks for approval stops, so its last call is when the owner became the
// blocker. An approval given while the agent was still working comes out
// near 0; one given while the agent sat idle away from a gate counts that idle
// time as waiting.
func OwnerWaits(store StatsStore, calls []time.Time) []GateWait {
	sorted := append([]time.Time(nil), calls...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })

	var waits []GateWait
	for i, row := range store.Transitions {
		if row.ApprovedAt == nil {
			continue
		}
		approved := *row.ApprovedAt
		var entered time.Time
		if i > 0 {
			entered = store.Transitions[i-1].TS
		}
		since := entered
		for _, c := range sorted {
			if c.After(approved) {
				break
			}
			if !c.Before(entered) {
				since = c
			}
		}
		if since.IsZero() {
			continue // no call and no earlier row: nothing to measure from
		}
		wait := approved.Sub(since)
		if wait < 0 {
			wait = 0
		}
		waits = append(waits, GateWait{From: row.From, To: row.To, Since: since, ApprovedAt: approved, Wait: wait})
	}
	return waits
}
