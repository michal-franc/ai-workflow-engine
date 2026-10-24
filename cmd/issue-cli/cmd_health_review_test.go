package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestHealthReviewFlow(t *testing.T) {
	proj, root := makeCIFixture(t, "")
	ctx, _, _ := newTestContext(proj, false)
	if err := runHealth(ctx, []string{"review"}); err == nil || !strings.Contains(err.Error(), "No health snapshot yet") {
		t.Fatalf("review without a snapshot: %v", err)
	}
	ctx, _, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"snapshot"}); err != nil {
		t.Fatal(err)
	}

	ctx, stdout, _ := newTestContext(proj, false)
	if err := runHealth(ctx, []string{"review"}); err != nil {
		t.Fatal(err)
	}
	brief := stdout.String()
	assertContains(t, brief, "Read the evidence pack first: "+filepath.Join(root, ".health", "reviews"))
	assertContains(t, brief, "health review record <file>")

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"review", "--schema"}); err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &schema); err != nil {
		t.Fatalf("--schema is not JSON: %v", err)
	}

	review := filepath.Join(t.TempDir(), "review.json")
	write := func(o health.Opinion) {
		data, _ := json.Marshal(o)
		os.WriteFile(review, data, 0o644)
	}
	op := health.Opinion{Score: 72, Assessment: strings.Repeat("fine ", 70), Recommendations: []health.Recommendation{
		{ID: "R1", Kind: "config", Title: "Keep the workflow small", Effort: "S", ExpectedEffect: "nothing breaks",
			Evidence: []health.Evidence{{Path: "workflow.yaml", Shows: "the config"}, {Metric: "codebase.lines{kind=data}", Shows: "7 lines"}}},
	}}
	bad := op
	bad.Recommendations = []health.Recommendation{op.Recommendations[0]}
	bad.Recommendations[0].Evidence = []health.Evidence{{Path: "missing.go", Shows: "x"}}
	write(bad)
	ctx, _, _ = newTestContext(proj, false)
	err := runHealth(ctx, []string{"review", "record", review})
	if err == nil || !strings.Contains(err.Error(), `path "missing.go" does not exist at the main head`) || !strings.Contains(err.Error(), "nothing recorded") {
		t.Fatalf("bad evidence: %v", err)
	}
	if o, _ := health.LatestOpinion(root, ctx.now()); o != nil {
		t.Fatal("a refused review was stored")
	}

	write(op)
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"review", "record", review, "--reviewer", "agent-x"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "✓ Recorded the review: score 72, 1 recommendation(s)")
	assertContains(t, stdout.String(), "Nothing becomes an issue by itself")
	snap, _, _ := health.Latest(root)
	if v, ok := snap.Value("opinion.score", nil); !ok || v != 72 || snap.Run.Trigger != health.TriggerReview {
		t.Fatalf("the review snapshot: opinion.score %v %v, trigger %q", v, ok, snap.Run.Trigger)
	}

	ctx, _, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"review", "file", "R1"}); err == nil || !strings.Contains(err.Error(), "needs the owner's approval") {
		t.Fatalf("file without --quote: %v", err)
	}
	ctx, stdout, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"review", "file", "R1", "--quote", "go ahead"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "✓ Filed R1 as keep-the-workflow-small")
	if _, err := os.Stat(filepath.Join(proj.IssueDir, "keep-the-workflow-small.md")); err != nil {
		t.Fatalf("no issue file: %v", err)
	}

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"review", "show"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "R1 [config, effort S, filed → keep-the-workflow-small]")

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runHealth(ctx, nil); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	assertContains(t, out, "Scores:")
	assertContains(t, out, "agent opinion    72")

	ctx, stdout, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"--family", "opinion"}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout.String(), "opinion.score")
	ctx, _, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"--family", "nope"}); err == nil || !strings.Contains(err.Error(), "families: ") {
		t.Fatalf("unknown family: %v", err)
	}
}

func TestHealthReviewSpawn(t *testing.T) {
	proj, root := makeCIFixture(t, "")
	ctx, _, _ := newTestContext(proj, false)
	if err := runHealth(ctx, []string{"snapshot"}); err != nil {
		t.Fatal(err)
	}
	var got agentrun.Spec
	orig := startAgent
	startAgent = func(_ *tracker.Project, spec agentrun.Spec) agentrun.Response {
		got = spec
		return agentrun.Response{Status: "started", Session: spec.Session}
	}
	t.Cleanup(func() { startAgent = orig })
	// A human starts every agent: no --quote, or a worker, starts nothing.
	ctx, _, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"review", "--spawn"}); exitOf(err) != 2 || !strings.Contains(err.Error(), "needs --quote") || got.Session != "" {
		t.Fatalf("no quote: err = %v, started %+v", err, got)
	}
	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "ui/a")
	ctx, _, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"review", "--spawn", "--quote", "review it"}); err == nil || !strings.Contains(err.Error(), "workers can't start agents") || got.Session != "" {
		t.Fatalf("worker: err = %v, started %+v", err, got)
	}
	t.Setenv("ISSUE_VIEWER_ISSUE_SLUG", "")

	ctx, stdout, _ := newTestContext(proj, false)
	if err := runHealth(ctx, []string{"review", "--spawn", "--agent", "codex", "--quote", "run a health review"}); err != nil {
		t.Fatal(err)
	}
	if got.Session != "review-"+proj.Slug || got.AgentType != "codex" || !got.Headless || got.WorkDir != root || !strings.Contains(got.Prompt, "evidence pack") {
		t.Errorf("spec = %+v", got)
	}
	if !strings.Contains(got.Prompt, `Started on the human's words: "run a health review" (owner's shell, `) {
		t.Errorf("prompt doesn't carry the human's words: %q", got.Prompt)
	}
	assertContains(t, stdout.String(), "✓ Started review agent review-"+proj.Slug)
	assertContains(t, stdout.String(), `Started on the human's words: "run a health review"`)
	ctx, _, _ = newTestContext(proj, false)
	if err := runHealth(ctx, []string{"review", "bogus"}); err == nil || !strings.Contains(err.Error(), "unknown health review subcommand") {
		t.Errorf("bogus subcommand: %v", err)
	}
}
