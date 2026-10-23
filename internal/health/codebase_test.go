package health

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func gitT(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeT(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitAt commits everything with author and committer date at.
func commitAt(t *testing.T, root string, at time.Time, msg string) {
	t.Helper()
	date := at.Format(time.RFC3339)
	env := []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
	gitT(t, root, env, "add", "-A")
	gitT(t, root, env, "commit", "-q", "-m", msg)
}

func lines(n int) string { return strings.Repeat("x\n", n) }

// healthRepo is a git repo on main whose first commit is 40 days before now.
func healthRepo(t *testing.T, now time.Time) (string, *tracker.Project) {
	t.Helper()
	root := t.TempDir()
	gitT(t, root, nil, "init", "-q", "-b", "main")
	gitT(t, root, nil, "config", "user.email", "h@test")
	gitT(t, root, nil, "config", "user.name", "h")
	gitT(t, root, nil, "config", "commit.gpgsign", "false")
	writeT(t, root, "workflow.yaml", "statuses:\n  - name: idea\ntransitions: []\nhealth:\n  big_file_lines: 50\n  exclude: [\"vendor/**\"]\n")
	writeT(t, root, "main.go", lines(10))
	commitAt(t, root, now.AddDate(0, 0, -40), "first")

	writeT(t, root, "cmd/app/app.go", lines(60)) // a big file
	writeT(t, root, "cmd/app/app_test.go", lines(30))
	writeT(t, root, "docs/readme.md", "# hi\nno trailing newline")
	writeT(t, root, "data/x.json", "{}\n")
	writeT(t, root, "Makefile", "all:\n")
	writeT(t, root, "assets/logo.png", "\x89PNG\x00\x01binary")
	writeT(t, root, "vendor/lib/lib.go", lines(500))      // excluded by config
	writeT(t, root, "issues/Core/an-issue.md", lines(20)) // the issues dir
	writeT(t, root, "notes.unknownext", "a\nb\n")
	commitAt(t, root, now.AddDate(0, 0, -3), "second")

	proj := &tracker.Project{Slug: "h", IssueDir: filepath.Join(root, "issues"), WorkflowFile: filepath.Join(root, "workflow.yaml"), WorkDir: root}
	return root, proj
}

func TestCodebaseFamilyCountsTheCommittedTree(t *testing.T) {
	now := time.Now()
	root, proj := healthRepo(t, now)
	writeT(t, root, "dirty.go", lines(1000)) // uncommitted: never counted

	env, err := NewEnv(proj, root, now)
	if err != nil {
		t.Fatal(err)
	}
	snap, _, err := take(context.Background(), env, TriggerManual, []registered{{"codebase", codebaseFamily}})
	if err != nil {
		t.Fatal(err)
	}
	if e := snap.Families["codebase"].Error; e != "" {
		t.Fatalf("codebase error: %s", e)
	}
	want := map[string]float64{
		"codebase.lines{kind=code}":                    71, // main.go 10 + app.go 60 + Makefile 1
		"codebase.files{kind=code}":                    3,
		"codebase.lines{kind=test}":                    30,
		"codebase.lines{kind=docs}":                    2, // last line without a newline still counts
		"codebase.lines{kind=data}":                    7, // workflow.yaml 6 + x.json 1
		"codebase.lines{kind=other}":                   2,
		"codebase.lines{area=cmd}":                     90,
		"codebase.lines{area=.}":                       19, // main.go, Makefile, notes, workflow.yaml
		"codebase.files{lang=go}":                      3,
		"codebase.lines{lang=make}":                    1,
		"codebase.binary_files":                        1,
		"codebase.source_lines":                        101,
		"codebase.test_ratio":                          0.423,
		"codebase.big_files":                           1,
		"codebase.big_file.lines{path=cmd/app/app.go}": 60,
		// growth: main 7 and 30 days ago was the first commit (main.go,
		// workflow.yaml); no snapshots exist yet.
		"codebase.growth.lines{kind=code,window=7d}":  61,
		"codebase.growth.files{kind=test,window=30d}": 1,
	}
	got := map[string]float64{}
	for _, r := range snap.Metrics {
		got[r.ID] = r.Value
	}
	for id, v := range want {
		if g, ok := got[id]; !ok || g != v {
			t.Errorf("%s = %v (present %v), want %v", id, g, ok, v)
		}
	}
	for id := range got {
		if strings.Contains(id, "vendor") || strings.Contains(id, "issues") || strings.Contains(id, "dirty") {
			t.Errorf("excluded path counted: %s", id)
		}
	}
	// git wrote the reflog with the commit dates, so it says exactly where
	// main stood 7 days ago.
	if age := got["codebase.growth.base_age_days{window=7d}"]; age != 7 {
		t.Errorf("7d base age = %v, want 7 (from the reflog)", age)
	}
}

// With snapshots on disk, growth compares with them instead of git.
func TestCodebaseGrowthUsesEarlierSnapshots(t *testing.T) {
	now := time.Now()
	root, proj := healthRepo(t, now)
	env, err := NewEnv(proj, root, now)
	if err != nil {
		t.Fatal(err)
	}
	old := &Snapshot{
		Run:      RunInfo{Date: now.AddDate(0, 0, -8).Format(dateLayout)},
		Families: map[string]FamilyResult{"codebase": {}},
	}
	for _, k := range kinds {
		for _, m := range []string{"codebase.files", "codebase.lines"} {
			old.Metrics = append(old.Metrics, Row{ID: RowID(m, map[string]string{"kind": k}), Metric: m, Labels: map[string]string{"kind": k}, Value: 1})
		}
	}
	if _, err := Write(root, old, now.AddDate(0, 0, -8)); err != nil {
		t.Fatal(err)
	}
	snap, _, err := take(context.Background(), env, TriggerManual, []registered{{"codebase", codebaseFamily}})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := snap.Value("codebase.growth.lines", map[string]string{"kind": KindCode, "window": "7d"}); v != 70 {
		t.Errorf("7d code growth = %v, want 71-1", v)
	}
	if v, _ := snap.Value("codebase.growth.base_age_days", map[string]string{"window": "7d"}); v != 8 {
		t.Errorf("7d base age = %v, want 8", v)
	}
	// 30 days back has no snapshot: git's first commit stands in.
	if v, _ := snap.Value("codebase.growth.lines", map[string]string{"kind": KindCode, "window": "30d"}); v != 61 {
		t.Errorf("30d code growth = %v, want 61", v)
	}
}

// The branch's reflog says where it stood, even when a fast-forward put
// older commits on top.
func TestCommitBeforeReadsTheReflog(t *testing.T) {
	now := time.Now()
	root, _ := healthRepo(t, now)
	head := gitT(t, root, nil, "rev-parse", "main")
	first := gitT(t, root, nil, "rev-parse", "main~1")
	// The reflog says main was moved to head 5 days ago, at first before.
	logPath := filepath.Join(root, ".git", "logs", "refs", "heads", "main")
	zero := strings.Repeat("0", 40)
	entry := func(from, to string, at time.Time) string {
		return from + " " + to + " h <h@test> " + strconv.FormatInt(at.Unix(), 10) + " +0000\tupdate\n"
	}
	reflog := entry(zero, first, now.AddDate(0, 0, -40)) + entry(first, head, now.AddDate(0, 0, -5))
	if err := os.WriteFile(logPath, []byte(reflog), 0o644); err != nil {
		t.Fatal(err)
	}
	at := now.AddDate(0, 0, -7)
	sha, when, err := commitBefore(context.Background(), root, "main", head, at)
	if err != nil || sha != first || !when.Equal(at) {
		t.Fatalf("7d ago = %s %v %v, want %s at %v", sha, when, err, first, at)
	}
	// Before the reflog starts, the commit dates decide: no commit that old.
	sha, _, err = commitBefore(context.Background(), root, "main", head, now.AddDate(0, 0, -50))
	if err != nil || sha != "" {
		t.Fatalf("50d ago = %q %v, want none", sha, err)
	}
}
