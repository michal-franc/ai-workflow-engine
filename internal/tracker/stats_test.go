package tracker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatsSidecarPath(t *testing.T) {
	cases := map[string]string{
		"issues/42.md":         "issues/42.stats.json",
		"foo/bar/my-issue.md":  "foo/bar/my-issue.stats.json",
		"issues/no-extension":  "issues/no-extension.stats.json",
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
