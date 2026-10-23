package clibug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestTitle(t *testing.T) {
	long := strings.Repeat("word ", 30)
	for text, want := range map[string]string{
		"Check ticks the wrong box. It also logs twice.": "Check ticks the wrong box",
		"Why does approve fail? No idea":                 "Why does approve fail",
		"first line\nsecond line":                        "first line",
		"issue-cli v0.52.1 crashes on init":              "issue-cli v0.52.1 crashes on init",
		"  trailing space.  ":                            "trailing space",
	} {
		if got := (Report{Text: text}).Title(); got != want {
			t.Errorf("Title(%q) = %q, want %q", text, got, want)
		}
	}
	got := Report{Text: long}.Title()
	if len([]rune(got)) > titleLimit+1 || !strings.HasSuffix(got, "word…") {
		t.Errorf("long title = %q (%d runes)", got, len([]rune(got)))
	}
}

func TestNormalizeTitle(t *testing.T) {
	a := NormalizeTitle("Check: ticks the WRONG box,  when ids repeat!")
	b := NormalizeTitle("check ticks the wrong box when ids repeat")
	if a != b {
		t.Fatalf("%q != %q", a, b)
	}
	if NormalizeTitle("check ticks the right box") == b {
		t.Fatal("different titles normalized equal")
	}
}

func TestFindEngine(t *testing.T) {
	projects := []tracker.Project{
		{Slug: "roguetech", Repo: "someone/roguetech"},
		{Slug: "issue-viewer", Repo: "michal-franc/ai-native-project-viewer"},
	}
	if p := FindEngine(&tracker.ProjectsConfig{Projects: projects}); p == nil || p.Slug != "issue-viewer" {
		t.Fatalf("old repo name: got %+v", p)
	}
	if p := FindEngine(&tracker.ProjectsConfig{EngineProject: "roguetech", Projects: projects}); p == nil || p.Slug != "roguetech" {
		t.Fatalf("engine_project: got %+v", p)
	}
	if p := FindEngine(&tracker.ProjectsConfig{EngineProject: "missing", Projects: projects}); p != nil {
		t.Fatalf("unknown engine_project should not fall back, got %+v", p)
	}
	if p := FindEngine(&tracker.ProjectsConfig{Projects: projects[:1]}); p != nil {
		t.Fatalf("no engine: got %+v", p)
	}
	if FindEngine(nil) != nil {
		t.Fatal("nil config")
	}
}

func TestReportedFrom(t *testing.T) {
	for r, want := range map[Report]string{
		{Project: "rt", IssueSlug: "a/b"}: "rt:a/b",
		{Project: "rt"}:                   "rt",
		{IssueSlug: "a/b"}:                "unknown:a/b",
		{}:                                "",
	} {
		if got := r.ReportedFrom(); got != want {
			t.Errorf("%+v: %q, want %q", r, got, want)
		}
	}
}

func TestQueueRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	p1, err := Enqueue(Report{At: at, Text: "one", Project: "rt"})
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := Enqueue(Report{At: at, Text: "two"})
	if p1 == p2 || !strings.HasPrefix(p1, filepath.Join(dir, "issue-cli", "bug-reports")) {
		t.Fatalf("paths %q %q", p1, p2)
	}
	os.WriteFile(filepath.Join(QueueDir(), "zz-broken.json"), []byte("{"), 0644)
	pending := Pending()
	if len(pending) != 2 || pending[0].Report.Text != "one" || pending[0].Report.Project != "rt" {
		t.Fatalf("pending = %+v", pending)
	}
	if n := PendingCount(); n != 3 {
		t.Fatalf("PendingCount = %d, want 3 (counts files, broken included)", n)
	}
}

func TestWithLockRuns(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ran := false
	if err := WithLock(func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("ran=%v err=%v", ran, err)
	}
}
