package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatsSidecarPath(t *testing.T) {
	cases := map[string]string{
		"issues/42.md":        "issues/42.stats.json",
		"foo/bar/my-issue.md": "foo/bar/my-issue.stats.json",
		"issues/no-extension": "issues/no-extension.stats.json",
	}
	for in, want := range cases {
		if got := StatsSidecarPath(in); got != want {
			t.Errorf("StatsSidecarPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadStats_MissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := LoadStats(filepath.Join(dir, "ghost.md"))
	if err != nil {
		t.Fatalf("LoadStats on missing file returned error: %v", err)
	}
	if len(store.Transitions) != 0 {
		t.Fatalf("expected empty store, got %d transitions", len(store.Transitions))
	}
}

func TestAppendTransitionStat_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	issuePath := filepath.Join(dir, "42.md")

	stat := TransitionStat{
		From:          "idea",
		To:            "in design",
		TS:            time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		StaticTokens:  100,
		DynamicTokens: 250,
	}
	if err := AppendTransitionStat(issuePath, stat); err != nil {
		t.Fatalf("AppendTransitionStat: %v", err)
	}

	store, err := LoadStats(issuePath)
	if err != nil {
		t.Fatalf("LoadStats: %v", err)
	}
	if len(store.Transitions) != 1 {
		t.Fatalf("got %d transitions, want 1", len(store.Transitions))
	}
	got := store.Transitions[0]
	if got.From != stat.From || got.To != stat.To || got.StaticTokens != stat.StaticTokens || got.DynamicTokens != stat.DynamicTokens {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, stat)
	}
	if got.ActualTokens != nil {
		t.Errorf("ActualTokens should be nil by default, got %v", *got.ActualTokens)
	}
}

func TestAppendTransitionStat_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	issuePath := filepath.Join(dir, "42.md")

	first := TransitionStat{From: "idea", To: "in design", TS: time.Now(), StaticTokens: 1, DynamicTokens: 2}
	second := TransitionStat{From: "in design", To: "backlog", TS: time.Now(), StaticTokens: 3, DynamicTokens: 4}

	if err := AppendTransitionStat(issuePath, first); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := AppendTransitionStat(issuePath, second); err != nil {
		t.Fatalf("second append: %v", err)
	}

	store, err := LoadStats(issuePath)
	if err != nil {
		t.Fatalf("LoadStats: %v", err)
	}
	if len(store.Transitions) != 2 {
		t.Fatalf("got %d transitions, want 2", len(store.Transitions))
	}
	if store.Transitions[0].From != "idea" || store.Transitions[1].From != "in design" {
		t.Errorf("transitions out of order: %+v", store.Transitions)
	}
}

func TestStaticTransitionCost_NonZeroForRealTransition(t *testing.T) {
	wf := DefaultWorkflow()
	cost := StaticTransitionCost(wf, "idea", "in design")
	if cost <= 0 {
		t.Fatalf("expected non-zero static cost for idea→in design, got %d", cost)
	}
}

func TestStaticTransitionCost_NilWorkflow(t *testing.T) {
	if cost := StaticTransitionCost(nil, "idea", "in design"); cost != 0 {
		t.Fatalf("expected 0 for nil workflow, got %d", cost)
	}
}

func TestAgentDispatchPromptStaticCost_NonZero(t *testing.T) {
	if AgentDispatchPromptStaticCost() <= 0 {
		t.Fatalf("expected dispatch prompt cost to be > 0, got %d", AgentDispatchPromptStaticCost())
	}
}

func TestApplyTransitionToFile_WritesStatsSidecar(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "sample.md")
	content := "---\n" +
		"title: \"sample\"\n" +
		"status: \"idea\"\n" +
		"---\n" +
		"\n" +
		"Some idea body text that contains enough characters to register a non-zero token estimate.\n"
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	wf := DefaultWorkflow()
	if _, _, err := wf.ApplyTransitionToFile(fp, "in design"); err != nil {
		t.Fatalf("ApplyTransitionToFile: %v", err)
	}

	store, err := LoadStats(fp)
	if err != nil {
		t.Fatalf("LoadStats: %v", err)
	}
	if len(store.Transitions) != 1 {
		t.Fatalf("expected 1 transition recorded, got %d", len(store.Transitions))
	}
	rec := store.Transitions[0]
	if rec.From != "idea" || rec.To != "in design" {
		t.Errorf("from/to = %q/%q, want idea/in design", rec.From, rec.To)
	}
	if rec.StaticTokens <= 0 {
		t.Errorf("static_tokens = %d, want > 0", rec.StaticTokens)
	}
	if rec.DynamicTokens < rec.StaticTokens {
		t.Errorf("dynamic_tokens (%d) should be >= static_tokens (%d)", rec.DynamicTokens, rec.StaticTokens)
	}
	if rec.ActualTokens != nil {
		t.Errorf("actual_tokens should be nil, got %v", *rec.ActualTokens)
	}
	if rec.TS.IsZero() {
		t.Errorf("TS should be populated, got zero time")
	}
}

func TestDynamicTransitionCost_AtLeastStatic(t *testing.T) {
	wf := DefaultWorkflow()
	static := StaticTransitionCost(wf, "idea", "in design")
	dynamic := DynamicTransitionCost(wf, "idea", "in design", "issue body text here", []Comment{
		{Text: "first comment"},
		{Text: "second comment"},
	})
	if dynamic <= static {
		t.Fatalf("dynamic (%d) should exceed static (%d) when body+comments are non-empty", dynamic, static)
	}
}

func TestLoadStats_IgnoresRemovedWaitFields(t *testing.T) {
	// Sidecars written while issue-cli had --wait (before v0.43.0) carry
	// pending_wait and per-row wait_started_at. They must still load, and the
	// next save drops them.
	issuePath := filepath.Join(t.TempDir(), "old.md")
	old := `{
  "transitions": [
    {
      "from": "in design",
      "to": "backlog",
      "ts": "2026-10-02T09:10:00Z",
      "static_tokens": 10,
      "dynamic_tokens": 20,
      "actual_tokens": null,
      "wait_started_at": "2026-10-02T09:00:00Z",
      "approved_at": "2026-10-02T09:10:00Z"
    }
  ],
  "pending_wait": {"to": "in progress", "started_at": "2026-10-02T09:20:00Z"},
  "last_approval": {"status": "in progress", "at": "2026-10-02T09:30:00Z"}
}
`
	if err := os.WriteFile(StatsSidecarPath(issuePath), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStats(issuePath)
	if err != nil {
		t.Fatalf("old sidecar must load: %v", err)
	}
	if len(store.Transitions) != 1 || store.Transitions[0].To != "backlog" || store.Transitions[0].ApprovedAt == nil {
		t.Fatalf("transitions = %+v", store.Transitions)
	}
	if store.LastApproval == nil || store.LastApproval.Status != "in progress" {
		t.Fatalf("last_approval = %+v", store.LastApproval)
	}
	if err := SaveStats(issuePath, store); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(StatsSidecarPath(issuePath))
	if strings.Contains(string(data), "pending_wait") || strings.Contains(string(data), "wait_started_at") {
		t.Fatalf("removed wait fields survived a save:\n%s", data)
	}
}

func TestRecordApproval_SetAndClear(t *testing.T) {
	issuePath := filepath.Join(t.TempDir(), "a.md")
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	if err := RecordApproval(issuePath, "backlog", at); err != nil {
		t.Fatal(err)
	}
	store, _ := LoadStats(issuePath)
	if store.LastApproval == nil || store.LastApproval.Status != "backlog" || !store.LastApproval.At.Equal(at) {
		t.Fatalf("LastApproval = %+v", store.LastApproval)
	}
	if store.Transitions == nil {
		t.Fatal("transitions must persist as [] not null")
	}
	if err := RecordApproval(issuePath, "", at); err != nil {
		t.Fatal(err)
	}
	if store, _ := LoadStats(issuePath); store.LastApproval != nil {
		t.Fatalf("toggled-off approval not cleared: %+v", store.LastApproval)
	}
}

func TestAppendTransitionStatLocked_AttributesApproval(t *testing.T) {
	issuePath := filepath.Join(t.TempDir(), "s.md")
	approvedAt := time.Date(2026, 10, 2, 9, 10, 0, 0, time.UTC)
	if err := RecordApproval(issuePath, "backlog", approvedAt); err != nil {
		t.Fatal(err)
	}

	if err := appendTransitionStatLocked(issuePath, TransitionStat{From: "in design", To: "backlog"}, "backlog"); err != nil {
		t.Fatal(err)
	}
	store, _ := LoadStats(issuePath)
	row := store.Transitions[0]
	if row.ApprovedAt == nil || !row.ApprovedAt.Equal(approvedAt) {
		t.Fatalf("row = %+v", row)
	}
	if store.LastApproval != nil {
		t.Fatalf("scratch state not cleared: %+v", store)
	}
}

func TestAppendTransitionStatLocked_IgnoresMismatchedScratch(t *testing.T) {
	issuePath := filepath.Join(t.TempDir(), "m.md")
	RecordApproval(issuePath, "done", time.Now())

	// A transition elsewhere that consumed no approval: ApprovedAt stays nil
	// and the unrelated approval is kept.
	if err := appendTransitionStatLocked(issuePath, TransitionStat{From: "in design", To: "obsolete"}, ""); err != nil {
		t.Fatal(err)
	}
	store, _ := LoadStats(issuePath)
	if row := store.Transitions[0]; row.ApprovedAt != nil {
		t.Fatalf("row = %+v, want no approval", row)
	}
	if store.LastApproval == nil {
		t.Fatal("approval not consumed by this transition must be kept")
	}
}
