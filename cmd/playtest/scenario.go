package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"gopkg.in/yaml.v3"
)

// ScenarioSpec is playtest/scenarios/<name>/scenario.yaml: a named starting
// state. Its issue fixtures live in a folder that is not called `issues/`
// (the sparse-checkout pattern `!issues/` would hide it in dispatched
// worktrees); they are copied to issues/ in the throwaway project.
type ScenarioSpec struct {
	Title   string `yaml:"title"`
	Project struct {
		Name string `yaml:"name"`
		Slug string `yaml:"slug"`
	} `yaml:"project"`
	Issues   string `yaml:"issues"`
	Workflow string `yaml:"workflow"`
	Docs     string `yaml:"docs"`
}

// LoadScenarioSpec reads dir/scenario.yaml and fills defaults.
func LoadScenarioSpec(dir string) (*ScenarioSpec, error) {
	data, err := os.ReadFile(filepath.Join(dir, "scenario.yaml"))
	if err != nil {
		return nil, err
	}
	var s ScenarioSpec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %v", filepath.Join(dir, "scenario.yaml"), err)
	}
	if s.Project.Slug == "" {
		s.Project.Slug = "demo"
	}
	if s.Project.Name == "" {
		s.Project.Name = "Playtest " + s.Project.Slug
	}
	if s.Issues == "" {
		s.Issues = "tracker"
	}
	if s.Workflow == "" {
		s.Workflow = "workflow.yaml"
	}
	return &s, nil
}

// Binaries are issue-viewer and issue-cli built from the current checkout,
// once per run.
type Binaries struct {
	Dir    string
	Viewer string
	CLI    string
}

// BuildBinaries runs `go build` for both binaries into dir.
func BuildBinaries(repo, dir string, log io.Writer) (*Binaries, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	b := &Binaries{Dir: dir, Viewer: filepath.Join(dir, "issue-viewer"), CLI: filepath.Join(dir, "issue-cli")}
	for _, t := range []struct{ out, pkg string }{{b.Viewer, "."}, {b.CLI, "./cmd/issue-cli"}} {
		cmd := exec.Command("go", "build", "-o", t.out, t.pkg)
		cmd.Dir = repo
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("go build %s: %v", t.pkg, err)
		}
	}
	return b, nil
}

// markerName proves a temp dir (and its tmux server) belongs to a playtest
// scenario, so teardown never removes or kills anything it didn't create.
const markerName = ".playtest-scenario"

// Scenario is one running copy of a scenario: a throwaway git project, its
// own projects.yaml, HOME and XDG dirs, a private tmux server, and a board
// on a free port. Every process it starts gets Env(), never the caller's.
type Scenario struct {
	Name string
	Spec *ScenarioSpec
	Root string // temp dir, holds the marker
	Proj string // the git project (issues/, workflow.yaml, docs/)
	Port int
	Base string // http://127.0.0.1:<port>

	bins  *Binaries
	board *exec.Cmd
	log   io.Writer
	env   []string
}

// StartScenario copies the fixtures from scenarioDir/<name> into a fresh
// temp project and starts the board on it.
func StartScenario(scenarioDir, name string, bins *Binaries, log io.Writer) (*Scenario, error) {
	src := filepath.Join(scenarioDir, name)
	spec, err := LoadScenarioSpec(src)
	if err != nil {
		return nil, err
	}
	// A short path under the system temp dir: tmux sockets must fit in ~108
	// bytes, which a worktree path often doesn't.
	root, err := os.MkdirTemp("", "pt-")
	if err != nil {
		return nil, err
	}
	s := &Scenario{Name: name, Spec: spec, Root: root, Proj: filepath.Join(root, "proj"), bins: bins, log: log}
	fail := func(err error) (*Scenario, error) {
		s.Stop(false)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, markerName), []byte(name+"\n"), 0o644); err != nil {
		return fail(err)
	}
	for _, d := range []string{"home", "state", "config", "cache", "tmux", "proj/docs"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return fail(err)
		}
	}
	if err := os.WriteFile(filepath.Join(s.TmuxDir(), markerName), []byte(name+"\n"), 0o644); err != nil {
		return fail(err)
	}
	if err := copyTree(filepath.Join(src, spec.Issues), filepath.Join(s.Proj, "issues")); err != nil {
		return fail(fmt.Errorf("scenario %s: issues: %v", name, err))
	}
	if err := pinIssueTimes(filepath.Join(s.Proj, "issues")); err != nil {
		return fail(err)
	}
	if err := copyFile(filepath.Join(src, spec.Workflow), filepath.Join(s.Proj, "workflow.yaml")); err != nil {
		return fail(fmt.Errorf("scenario %s: workflow: %v", name, err))
	}
	if spec.Docs != "" {
		if err := copyTree(filepath.Join(src, spec.Docs), filepath.Join(s.Proj, "docs")); err != nil {
			return fail(fmt.Errorf("scenario %s: docs: %v", name, err))
		}
	}
	cfg := fmt.Sprintf("projects:\n  - name: %q\n    slug: %q\n    issues: %q\n    docs: %q\n    workdir: %q\n",
		spec.Project.Name, spec.Project.Slug, filepath.Join(s.Proj, "issues"), filepath.Join(s.Proj, "docs"), s.Proj)
	if err := os.WriteFile(s.ConfigPath(), []byte(cfg), 0o644); err != nil {
		return fail(err)
	}
	if s.Port, err = freePort(); err != nil {
		return fail(err)
	}
	s.Base = fmt.Sprintf("http://127.0.0.1:%d", s.Port)
	s.env = scenarioEnv(os.Environ(), s)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"commit", "-q", "-m", "scenario " + name},
	} {
		if out, err := s.command("git", args...).CombinedOutput(); err != nil {
			return fail(fmt.Errorf("git %s: %v: %s", args[0], err, out))
		}
	}
	s.board = s.command(bins.Viewer, "-config", s.ConfigPath(), "-port", fmt.Sprint(s.Port))
	s.board.Stdout, s.board.Stderr = log, log
	s.board.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := s.board.Start(); err != nil {
		return fail(fmt.Errorf("start board: %v", err))
	}
	if err := waitHTTP(s.Base+"/", 10*time.Second); err != nil {
		return fail(fmt.Errorf("board on %s: %v", s.Base, err))
	}
	return s, nil
}

// ConfigPath is the scenario's projects.yaml.
func (s *Scenario) ConfigPath() string { return filepath.Join(s.Root, "projects.yaml") }

// TmuxDir is the scenario's TMUX_TMPDIR: its tmux server's socket lives here.
func (s *Scenario) TmuxDir() string { return filepath.Join(s.Root, "tmux") }

// IssueFile is the path of an issue in the scenario project, found the way
// the board finds it (slugs lower-case the system folder).
func (s *Scenario) IssueFile(slug string) (string, error) {
	issues, err := tracker.LoadIssues(filepath.Join(s.Proj, "issues"))
	if err != nil {
		return "", err
	}
	for _, is := range issues {
		if is.Slug == slug {
			return is.FilePath, nil
		}
	}
	return "", fmt.Errorf("no issue %q in the scenario", slug)
}

// createdRe reads an issue's created: date.
var createdRe = regexp.MustCompile(`(?m)^created:\s*"?(\d{4}-\d{2}-\d{2})`)

// pinIssueTimes sets each issue file's mtime from its created: date. The
// board sorts issues by mtime, so copied fixtures would otherwise come out
// in whatever order the copy happened to write them.
func pinIssueTimes(dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		t := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		if m := createdRe.FindSubmatch(data); m != nil {
			if c, err := time.Parse("2006-01-02", string(m[1])); err == nil {
				t = c.Add(12 * time.Hour)
			}
		}
		return os.Chtimes(p, t, t)
	})
}

// Env is the environment every scenario process runs with.
func (s *Scenario) Env() []string { return append([]string(nil), s.env...) }

// isolatedVars are dropped from the caller's environment: they would point a
// scenario process at the real config, the real tmux server, or tag it as a
// dispatched agent.
var isolatedVars = []string{
	"TMUX", "TMUX_PANE", "TMUX_TMPDIR", "HOME", "XDG_STATE_HOME", "XDG_CONFIG_HOME",
	"XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR", "ISSUE_VIEWER_CONFIG", "ISSUE_VIEWER_URL",
	"ISSUE_VIEWER_ISSUE_SLUG", "ISSUE_VIEWER_SERVER_PWD", "ISSUE_CLI_LOG", "AGENT_NAME",
	"AI_AGENT", "CLAUDECODE", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
}

// scenarioEnv builds a scenario's environment from base: the isolated
// variables are replaced so nothing reaches ~/Work/projects.yaml, the user's
// state, or the default tmux socket.
func scenarioEnv(base []string, s *Scenario) []string {
	drop := map[string]bool{}
	for _, k := range isolatedVars {
		drop[k] = true
	}
	var env []string
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if drop[k] || strings.HasPrefix(k, "GIT_AUTHOR_") || strings.HasPrefix(k, "GIT_COMMITTER_") {
			continue
		}
		if k == "PATH" && s.bins != nil {
			kv = "PATH=" + s.bins.Dir + string(os.PathListSeparator) + strings.TrimPrefix(kv, "PATH=")
		}
		env = append(env, kv)
	}
	return append(env,
		"HOME="+filepath.Join(s.Root, "home"),
		"XDG_STATE_HOME="+filepath.Join(s.Root, "state"),
		"XDG_CONFIG_HOME="+filepath.Join(s.Root, "config"),
		"XDG_CACHE_HOME="+filepath.Join(s.Root, "cache"),
		"TMUX_TMPDIR="+s.TmuxDir(),
		"ISSUE_VIEWER_CONFIG="+s.ConfigPath(),
		"ISSUE_VIEWER_URL="+s.Base,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=playtest", "GIT_AUTHOR_EMAIL=playtest@example.invalid",
		"GIT_COMMITTER_NAME=playtest", "GIT_COMMITTER_EMAIL=playtest@example.invalid",
		"TZ=UTC",
	)
}

// command builds a process that runs inside the scenario: its env, cwd in
// the project.
func (s *Scenario) command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Dir = s.Proj
	cmd.Env = s.Env()
	return cmd
}

// Run runs a command in the scenario and returns its combined output and
// exit code.
func (s *Scenario) Run(ctx context.Context, name string, args ...string) (string, int, error) {
	cmd := s.command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return "", -1, err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				return buf.String(), -1, err
			}
		}
		return buf.String(), code, nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return buf.String(), -1, ctx.Err()
	}
}

// CLI runs the scenario's branch-built issue-cli.
func (s *Scenario) CLI(ctx context.Context, args ...string) (string, int, error) {
	return s.Run(ctx, s.bins.CLI, args...)
}

// Tmux runs tmux against the scenario's private server.
func (s *Scenario) Tmux(ctx context.Context, args ...string) (string, int, error) {
	if err := s.guardTmux(); err != nil {
		return "", -1, err
	}
	return s.Run(ctx, "tmux", args...)
}

// guardTmux refuses to talk to a tmux server whose socket dir is not this
// scenario's marked private dir.
func (s *Scenario) guardTmux() error {
	dir := s.TmuxDir()
	if !strings.HasPrefix(dir, s.Root+string(os.PathSeparator)) {
		return fmt.Errorf("tmux dir %s is outside the scenario", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, markerName)); err != nil {
		return fmt.Errorf("tmux dir %s has no %s marker; refusing to touch it", dir, markerName)
	}
	return nil
}

// Expand replaces {project}, {base} and {dir} in a session string.
func (s *Scenario) Expand(v string) string {
	return strings.NewReplacer("{project}", s.Spec.Project.Slug, "{base}", s.Base, "{dir}", s.Proj).Replace(v)
}

// Stop kills the board and the private tmux server, then removes the temp
// dir unless keep is set.
func (s *Scenario) Stop(keep bool) {
	if s.board != nil && s.board.Process != nil {
		_ = syscall.Kill(-s.board.Process.Pid, syscall.SIGKILL)
		_ = s.board.Wait()
		s.board = nil
	}
	if s.guardTmux() == nil {
		if _, err := exec.LookPath("tmux"); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _, _ = s.Run(ctx, "tmux", "kill-server")
			cancel()
		}
	}
	if keep || s.Root == "" {
		return
	}
	if _, err := os.Stat(filepath.Join(s.Root, markerName)); err == nil {
		_ = os.RemoveAll(s.Root)
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitHTTP(url string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	var last error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
			last = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			last = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("not ready after %s: %v", budget, last)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		return copyFile(p, filepath.Join(dst, rel))
	})
}
