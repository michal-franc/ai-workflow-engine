package timeline

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestLoadCommitsFirstParent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "base")
	run("checkout", "-q", "-b", "work/ui/x")
	run("commit", "-q", "--allow-empty", "-m", "mine 1")
	run("checkout", "-q", "main")
	run("commit", "-q", "--allow-empty", "-m", "other issue")
	run("checkout", "-q", "work/ui/x")
	run("merge", "-q", "--no-edit", "main")
	run("commit", "-q", "--allow-empty", "-m", "mine 2")

	ref, name := GitRef(dir, "ui/x", nil)
	if ref == "" || name != "work/ui/x" {
		t.Fatalf("ref = %q %q", ref, name)
	}
	commits, err := LoadCommits(dir, ref, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, c := range commits {
		s := c.Subject
		if c.Merge {
			s = "merge"
		}
		subjects = append(subjects, s)
	}
	if got := strings.Join(subjects, ","); got != "base,mine 1,merge,mine 2" {
		t.Errorf("commits = %s; the merge is one line and the other issue's commit never shows", got)
	}
	if ref, _ := GitRef(dir, "ui/gone", nil); ref != "" {
		t.Errorf("a missing branch with no recorded head = %q", ref)
	}
}

// The game runner (RogueTech) writes started: without an offset, modes
// ux-test / playtest, shots as screenshots: beside results.yaml, and checks
// as kind/subject/passed. The fixture is its real results.yaml files.
func TestLoadPlaytestRunsGameSchema(t *testing.T) {
	const slug = "api/reproducible-scenarios-and-playtest-cases-for-battles-and-screens"
	runs, problems := LoadPlaytestRuns(nil, slug, "testdata/roguetech")
	if len(runs) != 4 {
		t.Fatalf("runs = %d, want 4 (two ux-test, the cached one, two playtest less the bad date)", len(runs))
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "20261024-090000-ux-test") || !strings.Contains(problems[0], "this morning") {
		t.Errorf("problems = %q, want the run whose started: can't be read", problems)
	}
	first := runs[0]
	if want := time.Date(2026, 10, 24, 10, 47, 8, 0, time.Local); first.Mode != "auto" || !first.Started.Equal(want) {
		t.Errorf("cached run = %s %s, want auto at %s local", first.Mode, first.Started, want)
	}
	ux := runs[1]
	if ux.Mode != "auto" || !strings.HasSuffix(ux.Dir, "20261024-110508-ux-test") || len(ux.Steps) != 1 {
		t.Fatalf("ux-test run = %+v", ux)
	}
	st := ux.Steps[0]
	if st.ID != "MV7" || st.Status != "pass" || !st.Judgement {
		t.Errorf("step = %+v, want MV7 pass, a judgement step", st)
	}
	if len(st.Viewports) != 1 {
		t.Fatalf("viewports = %+v, want one at 1920x1080", st.Viewports)
	}
	v := st.Viewports[0]
	if v.Viewport != "1920x1080" || v.Screenshot != "MV7@1920x1080.png" || v.Status != "pass" {
		t.Errorf("viewport = %+v", v)
	}
	if len(v.Checks) != 1 || !v.Checks[0].Pass || !strings.HasPrefix(v.Checks[0].Check, "state battle.ai.last.1 has") {
		t.Errorf("checks = %+v, want the state check, passed", v.Checks)
	}
	if runs[2].Mode != "judge" || len(runs[2].Steps) != 0 {
		t.Errorf("header-only playtest run = %+v, want judge with no steps", runs[2])
	}
	judged := runs[3]
	if judged.Mode != "judge" || len(judged.Steps) != 1 || judged.Steps[0].Judge == nil || judged.Steps[0].Judge.Status != "pass" {
		t.Errorf("playtest run = %+v, want judge with verdict pass", judged)
	}
	if len(judged.Steps) == 1 && len(judged.Steps[0].Viewports) != 0 {
		t.Errorf("playtest step viewports = %+v, want none (no shot, no layout)", judged.Steps[0].Viewports)
	}
}

func TestLoadPlaytestRunsGameLayoutFailure(t *testing.T) {
	runs, _ := LoadPlaytestRuns(nil, "combat/eldritch-nexus-and-glitcher-3d-enemy-units", "testdata/roguetech")
	if len(runs) != 1 || len(runs[0].Steps) != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	vps := runs[0].Steps[0].Viewports
	if len(vps) != 2 || vps[0].Viewport != "1920x1080" || vps[1].Viewport != "2560x1440" || vps[1].Screenshot != "MV2@2560x1440.png" {
		t.Fatalf("viewports = %+v, want 1920x1080 and 2560x1440 with their shots", vps)
	}
	for _, v := range vps {
		if v.Status != "fail" || len(v.Layout) != 1 || v.Layout[0].Rule != "inside_viewport" || !strings.Contains(v.Layout[0].Detail, "battle.topbar.menu") {
			t.Errorf("viewport %s = %+v, want failed on inside_viewport", v.Viewport, v)
		}
	}
}

func TestParseRunStarted(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Time
	}{
		{"2026-10-24T11:04:20Z", time.Date(2026, 10, 24, 11, 4, 20, 0, time.UTC)},
		{"2026-10-24T11:04:20+02:00", time.Date(2026, 10, 24, 9, 4, 20, 0, time.UTC)},
		{"2026-10-24T11:04:20", time.Date(2026, 10, 24, 11, 4, 20, 0, time.Local)},
		{"2026-10-24T11:04:20.250", time.Date(2026, 10, 24, 11, 4, 20, 250e6, time.Local)},
	} {
		got, err := parseRunStarted(tc.in)
		if err != nil || !got.Equal(tc.want) {
			t.Errorf("%s: %s %v, want %s", tc.in, got, err, tc.want)
		}
	}
	if _, err := parseRunStarted("this morning"); err == nil {
		t.Error("this morning parsed")
	}
}
