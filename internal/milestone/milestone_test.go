package milestone

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestNormAndCompare(t *testing.T) {
	if Norm("v0.53") != "0.53.0" || Norm("next") != "" {
		t.Fatalf("Norm: %q %q", Norm("v0.53"), Norm("next"))
	}
	if Compare("0.2", "0.10") >= 0 || Compare("1.0.0", "1.0.0") != 0 || Compare("a", "b") >= 0 {
		t.Fatal("Compare order wrong")
	}
}

func issue(version, status string) *tracker.Issue {
	return &tracker.Issue{Version: version, Status: status}
}

func TestPick(t *testing.T) {
	st := StatusesOf(&tracker.WorkflowConfig{Statuses: []tracker.WorkflowStatus{{Name: "Idea"}, {Name: "Done"}, {Name: "Parked", Optional: true}}})
	if st.Final != "done" || !st.Side["parked"] {
		t.Fatalf("statuses = %+v", st)
	}
	by := ByVersion([]*tracker.Issue{
		issue("0.53", "done"), issue("0.53", "done"),
		issue("0.54", "done"), issue("0.54", "idea"), issue("0.54", "parked"),
		issue("0.55", "idea"), issue("", "idea"),
	})
	if v, d, n, _ := Pick(by, st, "v0.53.0"); v != "0.54" || d != 1 || n != 2 {
		t.Fatalf("after 0.53: %s %d/%d", v, d, n)
	}
	if v, _, _, _ := Pick(by, st, ""); v != "0.54" {
		t.Fatalf("no release: %s, want the lowest with an open issue", v)
	}
	if v, _, _, _ := Pick(by, st, "0.55"); v != "" {
		t.Fatalf("after the last: %q", v)
	}
}

// The family reads the release from the changelog and leaves backfills out.
func TestFamily(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	for name, body := range map[string]string{
		"a.md": "---\ntitle: A\nstatus: done\nversion: \"0.54\"\n---\n",
		"b.md": "---\ntitle: B\nstatus: idea\nversion: \"0.54\"\n---\n",
		"c.md": "---\ntitle: C\nstatus: done\nversion: \"0.53\"\n---\n",
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "CHANGELOG.md"), []byte("# Changes\n\n## Unreleased\n\n## v0.53.0 — 2026-10-20\n- x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := health.Env{Root: root, Project: &tracker.Project{Slug: "p", IssueDir: dir}, Workflow: tracker.DefaultWorkflow(), Now: time.Now()}
	got, err := family(context.Background(), env)
	if err != nil || len(got) != 2 {
		t.Fatalf("family = %+v, %v", got, err)
	}
	if got[0].Labels["version"] != "0.54" || got[0].Value != 1 || got[1].Value != 2 {
		t.Fatalf("family = %+v", got)
	}
	env.Backfill = true
	if _, err := family(context.Background(), env); err != health.ErrNoPast {
		t.Fatalf("backfill err = %v, want ErrNoPast", err)
	}
}
