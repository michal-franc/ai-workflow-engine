package checks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The library's behaviour, from fixture scripts run under bash. Machine runs
// get stdin from a pipe; owner runs need a terminal, which script(1) gives.

type libRun struct {
	out  string
	code int
	root string
}

func needBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
}

// fakeIssueCLI writes an issue-cli stand-in that records its arguments and
// the environment the lib restored for it.
func fakeIssueCLI(t *testing.T, dir string) (bin, calls string) {
	t.Helper()
	calls = filepath.Join(dir, "calls.txt")
	bin = filepath.Join(dir, "issue-cli")
	body := `#!/bin/sh
echo "$* | TMUX_TMPDIR=${TMUX_TMPDIR:-unset} XDG_STATE_HOME=${XDG_STATE_HOME:-unset}" >> ` + calls + `
if [ "$1" = init ]; then mkdir -p issues docs && echo "statuses: []" > workflow.yaml; fi
`
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, calls
}

func libEnv(t *testing.T, root, bin string, extra ...string) []string {
	t.Helper()
	lib := filepath.Join(t.TempDir(), "lib.sh")
	if err := os.WriteFile(lib, Lib, 0o644); err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") && !strings.HasPrefix(kv, "TMUX_TMPDIR=") && !strings.HasPrefix(kv, "CHECKS_") {
			env = append(env, kv)
		}
	}
	env = append(env,
		"CHECKS_LIB="+lib, "CHECKS_SLUG=demo/thing", "CHECKS_PROJECT=demo",
		"CHECKS_ROOT="+root, "CHECKS_ISSUE_CLI="+bin, "NO_COLOR=1",
		"XDG_STATE_HOME=/real/state", "TMPDIR="+t.TempDir())
	return append(env, extra...)
}

func runMachine(t *testing.T, body string, extra ...string) libRun {
	t.Helper()
	needBash(t)
	root := t.TempDir()
	bin, _ := fakeIssueCLI(t, t.TempDir())
	path := writeScript(t, root, "demo", "#!/usr/bin/env bash\n# issue: demo/thing\nsource \"$CHECKS_LIB\"\n"+body)
	cmd := exec.Command("bash", path)
	cmd.Dir = root
	cmd.Env = libEnv(t, root, bin, extra...)
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return libRun{out: string(out), code: code, root: root}
}

// runOwner runs body on a pseudo-terminal with input typed line by line.
func runOwner(t *testing.T, body, input string) (libRun, string) {
	t.Helper()
	needBash(t)
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("no script(1) for a pseudo-terminal")
	}
	root := t.TempDir()
	bin, calls := fakeIssueCLI(t, t.TempDir())
	path := writeScript(t, root, "demo", "#!/usr/bin/env bash\n# issue: demo/thing\nsource \"$CHECKS_LIB\"\n"+body)
	cmd := exec.Command("script", "-qec", "bash "+path, "/dev/null")
	cmd.Dir = root
	cmd.Env = libEnv(t, root, bin)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(calls)
	return libRun{out: string(out), code: code, root: root}, string(data)
}

func record(t *testing.T, root, mode string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(root, RecordDir, "demo", "*-"+mode+".txt"))
	if len(files) != 1 {
		t.Fatalf("want one %s record, got %v", mode, files)
	}
	data, _ := os.ReadFile(files[0])
	return string(data)
}

func contains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("output lacks %q:\n%s", w, got)
		}
	}
}

func TestLibMachineRunVerdictsAndBoxes(t *testing.T) {
	r := runMachine(t, `
checks_intro "Demo."
step S1 "passes" --box TP2 --what "says hi"
run echo hi
expect '[[ $out == hi && $code == 0 ]]' "said hi"
step S2 "fails" --box TP3,TP4
run sh -c 'echo oops; exit 3'
expect '[ $code = 0 ]' "exit 0"
step S3 "needs eyes" --box TP5
ask "Looks right?"
step S1 "second part" --box TP6
expect true "fine"
summary --all TP1
`)
	if r.code != 1 {
		t.Fatalf("exit %d, want 1 (a machine check failed)\n%s", r.code, r.out)
	}
	contains(t, r.out,
		"Machine run: machine checks only",
		"━━ S1 · passes ━━  (box TP2)",
		"$ sh -c 'echo oops; exit 3'", "    oops", "(exit 3)", "✗ exit 0",
		"? Looks right? (needs your eyes)",
		"━━ S1 · second part ━━  (box TP2, TP6)",
		"✓ S1 pass", "✗ S2 fail: exit 0", "? S3 needs your eyes",
		"Boxes passed: TP2 TP6\n",
		"Machine run: 1 failed, 1 need the owner's eyes, 0 skipped. Nothing ticked.")
	rec := record(t, r.root, "machine")
	contains(t, rec, "issue: demo/thing", "mode: machine", "step S1 pass boxes=TP2,TP6", "step S2 fail boxes=TP3,TP4 note=exit 0", "step S3 eyes boxes=TP5", "boxes passed: TP2 TP6")
}

func TestLibRepeatedStepFailsWhenEitherPartFails(t *testing.T) {
	r := runMachine(t, `
step TP7 "first"
expect false "first part"
step TP7 "again"
expect true "second part"
summary --all HT1
`)
	contains(t, r.out, "✗ TP7 fail: first part", "Boxes passed: none")
}

func TestLibAllBoxNeedsEveryStep(t *testing.T) {
	r := runMachine(t, `
step MV1 "a" --box HT2
expect true ok
step MV2 "b" --box HT3
expect true ok
summary --all HT1
`)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.out)
	}
	contains(t, r.out, "Boxes passed: HT2 HT3 HT1")
}

func TestLibTouchesRealIsSkippedInAMachineRun(t *testing.T) {
	r := runMachine(t, `
if step MV9 "real" --touches-real "a window in your session"; then
	echo REAL-RAN
fi
run echo after-skip
expect false "never evaluated"
step MV10 "next"
expect true ok
summary
`)
	if strings.Contains(r.out, "REAL-RAN") || strings.Contains(r.out, "after-skip") || strings.Contains(r.out, "never evaluated") {
		t.Fatalf("a skipped step ran:\n%s", r.out)
	}
	contains(t, r.out, "Touches your real environment: a window in your session", "- MV9 skipped: touches your real environment", "✓ MV10 pass")
	if r.code != 0 {
		t.Fatalf("a skip is not a failure: exit %d", r.code)
	}
}

func TestLibYesForcesAMachineRun(t *testing.T) {
	// CHECKS_YES=1 wins even on a terminal; here stdin is a pipe anyway, so
	// check the mode line is there and no question blocks.
	r := runMachine(t, "step A x\nask 'q?'\nsummary\n", "CHECKS_YES=1")
	contains(t, r.out, "? q? (needs your eyes)")
}

func TestLibCleanupRunsLastFirstOnExit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "order")
	r := runMachine(t, `
on_exit sh -c "echo one >> `+marker+`"
on_exit sh -c "echo two >> `+marker+`"
echo body
`)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.out)
	}
	data, _ := os.ReadFile(marker)
	if string(data) != "two\none\n" {
		t.Fatalf("cleanup order = %q, want two then one", data)
	}
}

func TestLibCleanupRunsOnInterrupt(t *testing.T) {
	needBash(t)
	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "cleaned")
	bin, _ := fakeIssueCLI(t, t.TempDir())
	path := writeScript(t, root, "demo", "#!/usr/bin/env bash\nsource \"$CHECKS_LIB\"\non_exit touch "+marker+"\necho started\nsleep 30 & wait\n")
	cmd := exec.Command("bash", path)
	cmd.Env = libEnv(t, root, bin)
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	stdout.Read(buf) // "started": the traps are set
	cmd.Process.Signal(syscall.SIGINT)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 130 {
			t.Fatalf("exit = %v, want 130", err)
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("the script didn't stop on SIGINT")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("on_exit didn't run on SIGINT")
	}
}

func TestLibOwnerRunTicksPassedBoxesWithTheRealEnvironment(t *testing.T) {
	r, calls := runOwner(t, `
checks_intro "Owner."
step S1 "ok" --box TP2
private_tmux 2>/dev/null || true
expect true fine
ask "Fine?"
step S2 "bad" --box TP3
ask "Broken?"
summary --all TP1
`, "\ny\n\nn\n\ny\ny\n")
	contains(t, r.out, "Tick the passed boxes (TP2) on demo/thing?", "Comment the failures on demo/thing")
	contains(t, calls,
		"--project demo check demo/thing TP2 | TMUX_TMPDIR=unset XDG_STATE_HOME=/real/state",
		"--project demo comment demo/thing checks: demo: S2 failed: you answered no: Broken?")
	contains(t, record(t, r.root, "owner"), "mode: owner", "boxes passed: TP2")
}

func TestLibOwnerQStopsAndCleansUp(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cleaned")
	r, calls := runOwner(t, `
on_exit touch `+marker+`
step S1 "first"
ask "Go on?"
step S2 "never"
summary
`, "q\n")
	if r.code != 130 {
		t.Fatalf("exit %d, want 130\n%s", r.code, r.out)
	}
	if strings.Contains(r.out, "S2 · never") || calls != "" {
		t.Fatalf("ran past q:\n%s\ncalls: %s", r.out, calls)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("q didn't run the cleanup")
	}
	if files, _ := filepath.Glob(filepath.Join(r.root, RecordDir, "demo", "*-owner.txt")); len(files) != 0 {
		t.Fatal("a stopped run left an owner record")
	}
}

func TestLibOwnerTouchesRealRunsOnlyOnY(t *testing.T) {
	r, _ := runOwner(t, `
if step MV9 "real" --touches-real "a window"; then echo REAL-RAN; fi
if step MV10 "real too" --touches-real "another"; then echo REAL2-RAN; fi
summary
`, "y\n\n\n\nn\n\n")
	contains(t, r.out, "REAL-RAN", "- MV10 skipped: you chose not to run it")
	if strings.Contains(r.out, "REAL2-RAN") {
		t.Fatalf("ran without a y:\n%s", r.out)
	}
}

func TestLibCleanShellSkipsTheProfileAndFindsTheStandIn(t *testing.T) {
	// The owner's profile and rc put a "real" claude first on PATH; the
	// stand-in must still win, and the profile must not run.
	home := t.TempDir()
	realBin := filepath.Join(home, "real-bin")
	os.MkdirAll(realBin, 0o755)
	os.WriteFile(filepath.Join(realBin, "claude"), []byte("#!/bin/sh\necho REAL-CLAUDE\n"), 0o755)
	rc := "export PATH=" + realBin + ":$PATH\necho PROFILE-RAN\n"
	for _, f := range []string{".bash_profile", ".profile", ".bashrc"} {
		os.WriteFile(filepath.Join(home, f), []byte(rc), 0o644)
	}
	r := runMachine(t, `
step S1 "stand-in"
stand_in claude 'echo STAND-IN'
run clean_shell sh -c 'claude; $SHELL -lic claude'
expect '[[ $out != *REAL-CLAUDE* && $out != *PROFILE-RAN* ]]' "no profile, no real claude"
expect_stand_in_ran claude
summary
`, "HOME="+home)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.out)
	}
	contains(t, r.out, "STAND-IN", "✓ the stand-in claude ran, not the real one")
}

func TestLibStandInCheckFailsWhenItNeverRan(t *testing.T) {
	r := runMachine(t, "step S1 x\nstand_in claude\nexpect_stand_in_ran claude \"the stand-in claude ran, not the real one\" 1\nsummary\n")
	contains(t, r.out, "✗ the stand-in claude ran, not the real one")
}

func TestLibSandboxProjectLivesOutsideWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	// A TMPDIR inside a .worktrees/ folder must not be used.
	wtTmp := filepath.Join(t.TempDir(), ".worktrees", "x", "tmp")
	os.MkdirAll(wtTmp, 0o755)
	r := runMachine(t, `
sandbox_project sbx
echo "DIR=$SANDBOX_PROJECT_DIR"
echo "CFG=$SANDBOX_CONFIG"
echo "STATE=$XDG_STATE_HOME VIEWER=$ISSUE_VIEWER_CONFIG"
cat "$SANDBOX_CONFIG"
git -C "$SANDBOX_PROJECT_DIR" log --oneline | wc -l
sic list
`, "TMPDIR="+wtTmp)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.out)
	}
	if strings.Contains(r.out, "/.worktrees/") {
		t.Fatalf("the sandbox is under .worktrees/:\n%s", r.out)
	}
	contains(t, r.out, `slug: "sbx"`, "/sbx/issues", "workdir: ")
	dir := between(r.out, "DIR=", "\n")
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("the sandbox %s outlived the script", dir)
	}
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	if j := strings.Index(s, b); j >= 0 {
		return s[:j]
	}
	return s
}

func TestLibPrivateTmuxKillsOnlyItsOwnServer(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	// Another private server (standing in for the owner's) must survive.
	other, err := os.MkdirTemp("/tmp", "ckother-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(other)
	otherTmux := func(args ...string) error {
		c := exec.Command("tmux", args...)
		c.Env = append(os.Environ(), "TMUX_TMPDIR="+other)
		c.Env = append(c.Env, "TMUX=")
		return c.Run()
	}
	if err := otherTmux("new-session", "-d", "-s", "owner", "sleep 60"); err != nil {
		t.Skipf("tmux can't start here: %v", err)
	}
	defer otherTmux("kill-server")

	r := runMachine(t, `
private_tmux
echo "TMUXDIR=$TMUX_TMPDIR"
ptmux new-session -d -s sbx 'sleep 60'
ptmux has-session -t sbx && echo PRIVATE-UP
`, "TMUX_TMPDIR="+other)
	contains(t, r.out, "PRIVATE-UP")
	dir := between(r.out, "TMUXDIR=", "\n")
	if dir == other || dir == "" {
		t.Fatalf("private_tmux reused the caller's TMUX_TMPDIR: %q", dir)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatalf("the private tmux dir %s outlived the script", dir)
	}
	if err := otherTmux("has-session", "-t", "owner"); err != nil {
		t.Fatal("the other tmux server was killed")
	}
}

func TestLibNeedsBash4(t *testing.T) {
	if !strings.Contains(string(Lib), `"${BASH_VERSINFO[0]}" -lt 4`) {
		t.Fatal("the bash 4 guard is gone")
	}
}

func TestLibPassesShellcheck(t *testing.T) {
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("no shellcheck")
	}
	path := filepath.Join(t.TempDir(), "lib.sh")
	os.WriteFile(path, Lib, 0o644)
	if out, err := exec.Command("shellcheck", "-s", "bash", "-S", "warning", path).CombinedOutput(); err != nil {
		t.Fatalf("shellcheck:\n%s", out)
	}
}

func TestLibDefaultBoxAndSeveralAllBoxes(t *testing.T) {
	r := runMachine(t, `
checks_default_box TP2
step S1 a
expect true ok
step S2 b --box TP9
expect true ok
summary --all TP3,TP4 --all TP5
`)
	contains(t, r.out, "(box TP2)", "(box TP9)", "Boxes passed: TP2 TP9 TP3 TP4 TP5\n")
}

func TestLibStepActiveAndStepEnd(t *testing.T) {
	r := runMachine(t, `
step_active && echo BEFORE-ANY-STEP
step S1 a
step_active && echo IN-S1
skip "not here"
step_active && echo AFTER-SKIP
step S2 b
expect true ok
step_end
echo AFTER-END
step_active && echo STILL-ACTIVE
summary
`)
	contains(t, r.out, "IN-S1", "- S1 skipped: not here")
	for _, bad := range []string{"BEFORE-ANY-STEP", "AFTER-SKIP", "STILL-ACTIVE"} {
		if strings.Contains(r.out, bad) {
			t.Errorf("step_active wrong: %s\n%s", bad, r.out)
		}
	}
	if i, j := strings.Index(r.out, "S2 passed"), strings.Index(r.out, "AFTER-END"); i < 0 || j < i {
		t.Errorf("step_end didn't close S2 before the next line:\n%s", r.out)
	}
}

func TestLibEventuallyRetries(t *testing.T) {
	flag := filepath.Join(t.TempDir(), "later")
	r := runMachine(t, `
step S1 a
( sleep 0.5; touch `+flag+` ) &
eventually '[ -e `+flag+` ]' "the file appears" 5
eventually false "never" 1
summary
`)
	contains(t, r.out, "✓ the file appears", "✗ never")
}

func TestLibThenIsTheOwnersClosingLine(t *testing.T) {
	r, _ := runOwner(t, `
step S1 a
expect true ok
summary --then "Approve documentation when you're happy."
`, "\n\n")
	contains(t, r.out, "Approve documentation when you're happy.")
}

func TestLibSandboxProjectOptions(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	wf := filepath.Join(t.TempDir(), "wf.yaml")
	os.WriteFile(wf, []byte("statuses: [{name: idea}]\n"), 0o644)
	r := runMachine(t, `
sandbox_project sand --workflow `+wf+` --name Sand
cat "$SANDBOX_CONFIG"
cat "$SANDBOX_PROJECT_DIR/workflow.yaml"
ls "$SANDBOX_PROJECT_DIR"
`)
	contains(t, r.out, `name: "Sand"`, `slug: "sand"`, `terminal: "none"`, "statuses: [{name: idea}]", "issues")
}

func TestLibAgentStandInStaysAndLogsInput(t *testing.T) {
	r := runMachine(t, `
step S1 a
stand_in --agent claude
mkfifo "$CHECKS_STAND_IN_DIR/fifo"
background pid clean_shell sh -c 'claude --model x < "$CHECKS_STAND_IN_DIR/fifo"'
exec 3> "$CHECKS_STAND_IN_DIR/fifo"
echo "the brief" >&3
expect_stand_in_ran claude
expect_stand_in_got claude "the brief"
expect 'kill -0 $pid' "the stand-in is still alive"
exec 3>&-
summary
`)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.out)
	}
}

func TestLibBackgroundPidIsTheCommand(t *testing.T) {
	r := runMachine(t, `
background pid clean_shell sleep 30
sleep 0.3
ps -o comm= -p "$pid"
free_port p
echo "PORT=$p"
`)
	contains(t, r.out, "sleep\n", "PORT=")
}

func TestLibCleanPrivateTmuxGivesPanesTheStandIns(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	r := runMachine(t, `
stand_in claude
private_tmux --clean
ptmux new-session -d -s w 'sh -c "command -v claude > $CHECKS_STAND_IN_DIR/which; sleep 5"'
sleep 0.5
cat "$CHECKS_STAND_IN_DIR/which"
`)
	if !strings.Contains(r.out, "/bin/claude") || !strings.Contains(r.out, "ck-") {
		t.Fatalf("the pane didn't find the stand-in first:\n%s", r.out)
	}
}
