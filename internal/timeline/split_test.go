package timeline

import (
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var t0 = time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func ptr(t time.Time) *time.Time { return &t }

func TestClassifyGaps(t *testing.T) {
	bands := []Band{
		{Status: "idea", From: at(0), To: at(100)},
		{Status: "in design", From: at(100), To: at(300), Gated: true},
	}
	segs := ClassifyGaps(
		[]time.Time{at(5), at(30), at(40), at(110), at(150), at(160), at(200)},
		[]time.Time{at(190)},
		bands, at(0), at(210))
	want := []Seg{
		{at(0), at(5), KindAgent},
		{at(5), at(30), KindIdle},       // 25m, not gated
		{at(30), at(40), KindAgent},     // 10m < 15m
		{at(40), at(110), KindIdle},     // starts in idea: not gated
		{at(110), at(150), KindWaiting}, // 40m in a gated status
		{at(150), at(160), KindAgent},
		{at(160), at(190), KindWaiting}, // 30m in a gated status, up to the approval
		{at(190), at(200), KindAgent},   // after the approval: its own 10m gap
		{at(200), at(210), KindAgent},
	}
	if len(segs) != len(want) {
		t.Fatalf("got %d segments, want %d: %+v", len(segs), len(want), segs)
	}
	for i := range want {
		if !segs[i].From.Equal(want[i].From) || !segs[i].To.Equal(want[i].To) || segs[i].Kind != want[i].Kind {
			t.Errorf("segment %d = %v→%v %s, want %v→%v %s", i, segs[i].From, segs[i].To, segs[i].Kind, want[i].From, want[i].To, want[i].Kind)
		}
	}
}

// The wait at an approval is tracker.OwnerWaits' (from the agent's last call

// in the status to the approval), laid over whatever the gap rules said.

func TestOverlayWaits(t *testing.T) {
	segs := []Seg{{at(0), at(60), KindAgent}, {at(60), at(90), KindIdle}}
	store := tracker.StatsStore{Transitions: []tracker.TransitionStat{
		{From: "idea", To: "in design", TS: at(0)},
		{From: "in design", To: "in progress", TS: at(85), ApprovedAt: ptr(at(80))},
	}}
	got := OverlayWaits(segs, tracker.OwnerWaits(store, []time.Time{at(5), at(50)}))
	want := []Seg{{at(0), at(50), KindAgent}, {at(50), at(60), KindWaiting}, {at(60), at(80), KindWaiting}, {at(80), at(90), KindIdle}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("segment %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReworkLoops(t *testing.T) {
	wf := tracker.DefaultWorkflow()
	src := Sources{
		Transitions: []tracker.TransitionStat{
			{From: "in progress", To: "testing", TS: at(60)},
			{From: "testing", To: "in progress", TS: at(70)}, // backward
			{From: "in progress", To: "testing", TS: at(90)},
		},
		Runs: []PlaytestRun{
			{Mode: "auto", Started: at(20), Steps: []PlaytestStep{{ID: "TP1", Status: "fail"}}}, // before testing: not rework
			{Mode: "judge", Started: at(100), Steps: []PlaytestStep{{ID: "TP1", Status: "pass"}, {ID: "TP2", Status: "fail"}}},
			{Mode: "auto", Started: at(105), Steps: []PlaytestStep{{ID: "TP2", Status: "pass"}}}, // another mode
			{Mode: "judge", Started: at(120), Steps: []PlaytestStep{{ID: "TP2", Status: "pass"}}},
		},
	}
	loops := ReworkLoops(src, wf, at(200))
	if len(loops) != 2 {
		t.Fatalf("loops = %+v, want 2", loops)
	}
	if !loops[0].From.Equal(at(70)) || !loops[0].To.Equal(at(90)) || !strings.Contains(loops[0].Label, "back to in progress") {
		t.Errorf("backward loop = %+v", loops[0])
	}
	if !loops[1].From.Equal(at(100)) || !loops[1].To.Equal(at(120)) || loops[1].Label != "your playtest failed TP2" {
		t.Errorf("playtest loop = %+v", loops[1])
	}

	segs := MarkRework([]Seg{{at(60), at(200), KindAgent}}, loops)
	if got := KindWithin(segs, KindRework, at(0), at(200)); got != 40*time.Minute {
		t.Errorf("rework = %v, want 40m", got)
	}
}
