package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withCwd switches into dir for the duration of the test. The bootstrap path
// in loadProjectOrErr keys off ./issues/ so tests must run from a directory
// without one to exercise the projects.yaml branch.
func withCwd(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// writeProjectsConfig drops a projects.yaml with the given slugs into dir
// and creates the matching issues directories so tracker.LoadIssues won't
// fail when a command does end up resolving against one of them.
func writeProjectsConfig(t *testing.T, dir string, slugs ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("projects:\n")
	for _, s := range slugs {
		issuesDir := filepath.Join(dir, s+"-issues")
		if err := os.MkdirAll(issuesDir, 0755); err != nil {
			t.Fatalf("mkdir issues: %v", err)
		}
		b.WriteString("  - name: " + strings.ToUpper(s[:1]) + s[1:] + "\n")
		b.WriteString("    slug: " + s + "\n")
		b.WriteString("    issues: " + issuesDir + "\n")
	}
	cfg := filepath.Join(dir, "projects.yaml")
	if err := os.WriteFile(cfg, []byte(b.String()), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfg
}

func TestLoadProjectOrErr_MultiProjectNoSlugIsAmbiguous(t *testing.T) {
	dir := t.TempDir()
	withCwd(t, dir)
	cfg := writeProjectsConfig(t, dir, "alpha", "beta")

	proj, all, err := loadProjectOrErr(cfg, "")
	if !errors.Is(err, errAmbiguousProject) {
		t.Fatalf("expected errAmbiguousProject, got %v", err)
	}
	if proj != nil {
		t.Fatalf("expected nil project on ambiguity, got %+v", proj)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 projects in list, got %d", len(all))
	}
	msg := err.Error()
	for _, want := range []string{"alpha (default)", "beta", "Available projects:", "issue-cli --project <slug>"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error message missing %q\n%s", want, msg)
		}
	}
}

func TestLoadProjectOrErr_SingleProjectStillSilentDefault(t *testing.T) {
	// Regression guard for the "single-project setups produce identical
	// output" acceptance criterion. The historical silent fallback to
	// projects[0] must still happen when only one project is configured.
	dir := t.TempDir()
	withCwd(t, dir)
	cfg := writeProjectsConfig(t, dir, "only")

	proj, all, err := loadProjectOrErr(cfg, "")
	if err != nil {
		t.Fatalf("expected nil error in single-project mode, got %v", err)
	}
	if proj == nil || proj.Slug != "only" {
		t.Fatalf("expected silent fallback to 'only', got %+v", proj)
	}
	if len(all) != 1 || all[0].Slug != "only" {
		t.Fatalf("expected single-element project list, got %+v", all)
	}
}

func TestLoadProjectOrErr_ExplicitSlugMatchesInMultiProject(t *testing.T) {
	dir := t.TempDir()
	withCwd(t, dir)
	cfg := writeProjectsConfig(t, dir, "alpha", "beta")

	proj, all, err := loadProjectOrErr(cfg, "beta")
	if err != nil {
		t.Fatalf("expected nil error with explicit slug, got %v", err)
	}
	if proj == nil || proj.Slug != "beta" {
		t.Fatalf("expected resolved 'beta', got %+v", proj)
	}
	if len(all) != 2 {
		t.Fatalf("expected full project list, got %d", len(all))
	}
}

func TestLoadProjectOrErr_UnknownSlugListsProjects(t *testing.T) {
	dir := t.TempDir()
	withCwd(t, dir)
	cfg := writeProjectsConfig(t, dir, "alpha", "beta")

	_, _, err := loadProjectOrErr(cfg, "ghost")
	if err == nil {
		t.Fatalf("expected error for unknown slug")
	}
	for _, want := range []string{`project "ghost" not found`, "alpha", "beta", "Available projects:"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %s", want, err.Error())
		}
	}
}

func TestRun_AmbiguousProjectFailsLoudForIssueCommands(t *testing.T) {
	// End-to-end check via the run() entry point: in multi-project setups
	// every issue-operating command must refuse to silently pick projects[0].
	dir := t.TempDir()
	withCwd(t, dir)
	writeProjectsConfig(t, dir, "alpha", "beta")

	var stdout, stderr bytes.Buffer
	err := run([]string{"--config", "projects.yaml", "show", "anything"}, strings.NewReader(""), &stdout, &stderr)
	if !errors.Is(err, errAmbiguousProject) {
		t.Fatalf("expected errAmbiguousProject from run(), got %v", err)
	}
}

func TestRun_AmbiguousProjectAllowsHelp(t *testing.T) {
	// Help is allow-listed: a bot orienting itself in a multi-project setup
	// needs --help to render the project list, which is the very thing it's
	// missing. Blocking help would create a chicken-and-egg failure mode.
	dir := t.TempDir()
	withCwd(t, dir)
	writeProjectsConfig(t, dir, "alpha", "beta")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--config", "projects.yaml", "help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run help: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"Configured projects:", "alpha", "beta", "(historical default)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help output missing %q\n%s", want, out)
		}
	}
}

func TestRun_NoArgsPrintsProjectListInMultiProject(t *testing.T) {
	dir := t.TempDir()
	withCwd(t, dir)
	writeProjectsConfig(t, dir, "alpha", "beta")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--config", "projects.yaml"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run with no args: %v", err)
	}
	if !strings.Contains(stdout.String(), "Configured projects:") {
		t.Fatalf("expected --help output to enumerate projects:\n%s", stdout.String())
	}
}

func TestRun_SingleProjectHelpOmitsProjectSection(t *testing.T) {
	// Regression guard: when only one project is configured the help output
	// should look exactly like before — no Configured projects: section.
	dir := t.TempDir()
	withCwd(t, dir)
	writeProjectsConfig(t, dir, "only")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--config", "projects.yaml", "help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run help: %v", err)
	}
	if strings.Contains(stdout.String(), "Configured projects:") {
		t.Fatalf("single-project help should NOT print Configured projects:\n%s", stdout.String())
	}
}

func TestNotFoundError_SingleProjectUnchanged(t *testing.T) {
	// The single-project not-found message is part of the byte-identical
	// regression guard — bots already grep for "Run: issue-cli list".
	ctx := &Context{}
	got := notFoundError(ctx, "missing-slug").Error()
	want := "issue not found: missing-slug\n\nRun: issue-cli list"
	if got != want {
		t.Fatalf("single-project not-found drift\n got: %q\nwant: %q", got, want)
	}
}

func TestLoadProjectOrErr_ExplicitSlugOverridesBootstrap(t *testing.T) {
	// A bot inside one project workdir (with its own ./issues/) must still be
	// able to query a sibling project by passing --project. The bootstrap
	// auto-detection on cwd should yield to an explicit flag.
	dir := t.TempDir()
	withCwd(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "issues"), 0755); err != nil {
		t.Fatalf("mkdir local issues: %v", err)
	}
	cfg := writeProjectsConfig(t, dir, "alpha", "beta")

	proj, all, err := loadProjectOrErr(cfg, "beta")
	if err != nil {
		t.Fatalf("expected explicit --project to override bootstrap, got %v", err)
	}
	if proj == nil || proj.Slug != "beta" {
		t.Fatalf("expected resolved 'beta' from config, got %+v", proj)
	}
	if len(all) != 2 {
		t.Fatalf("expected full project list when --project overrides bootstrap, got %d", len(all))
	}
}

func TestLoadProjectOrErr_NoSlugStillBootstrapsWhenIssuesPresent(t *testing.T) {
	// Regression guard: without --project, the existing bootstrap path must
	// keep working in cwd-with-./issues/ setups.
	dir := t.TempDir()
	withCwd(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "issues"), 0755); err != nil {
		t.Fatalf("mkdir local issues: %v", err)
	}

	proj, all, err := loadProjectOrErr("projects.yaml", "")
	if err != nil {
		t.Fatalf("bootstrap should still resolve without --project, got %v", err)
	}
	if proj == nil || proj.IssueDir != "./issues" {
		t.Fatalf("expected synthesized cwd project, got %+v", proj)
	}
	if len(all) != 1 {
		t.Fatalf("bootstrap should produce single-element list, got %d", len(all))
	}
}

func TestRunProjects_ListsConfiguredProjects(t *testing.T) {
	dir := t.TempDir()
	withCwd(t, dir)
	cfg := writeProjectsConfig(t, dir, "alpha", "beta")
	_, all, _ := loadProjectOrErr(cfg, "alpha")

	ctx, stdout, _ := newTestContext(&all[0], false)
	ctx.AllProjects = all
	ctx.ProjectSlug = "alpha"
	if err := runProjects(ctx, nil); err != nil {
		t.Fatalf("runProjects: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"== Configured Projects ==", "alpha (active)", "beta", "issues:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("projects output missing %q\n%s", want, out)
		}
	}
}

func TestRunProjects_BootstrapModeShowsSynthesizedProject(t *testing.T) {
	// In a cwd-with-./issues/ setup `issue-cli projects` should still work
	// and show the single bootstrap project as active.
	dir := t.TempDir()
	withCwd(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "issues"), 0755); err != nil {
		t.Fatalf("mkdir local issues: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := run([]string{"projects"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run projects in bootstrap mode: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"== Configured Projects ==", "(active)", "issues: ./issues"} {
		if !strings.Contains(out, want) {
			t.Fatalf("bootstrap projects output missing %q\n%s", want, out)
		}
	}
}

func TestRun_MissingConfigFailsLoudInsteadOfPanic(t *testing.T) {
	// Regression guard for the nil-pointer panic that surfaced when the
	// previous version silently swallowed any non-ambiguous projErr. With
	// the gate widened, a missing config + non-help command must return a
	// clean error to the caller rather than letting nil ctx.Project flow
	// into runList / runShow / etc.
	dir := t.TempDir()
	withCwd(t, dir)
	// Deliberately no ./issues/ and no projects.yaml so loadProjectOrErr
	// hits the projects.yaml branch and fails on the missing file.

	var stdout, stderr bytes.Buffer
	err := run([]string{"--config", "nonexistent.yaml", "list"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected missing-config error, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent.yaml") {
		t.Fatalf("expected error to mention config path, got %v", err)
	}
}

func TestRun_ProjectsCommandTolerantOfMissingConfig(t *testing.T) {
	// `issue-cli projects` is the discovery surface a confused bot reaches
	// for first. It must NOT fail loud when the config is missing — that
	// would defeat the discovery use case. (Help and process are in the
	// same allow-list for the same reason.)
	dir := t.TempDir()
	withCwd(t, dir)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--config", "nonexistent.yaml", "projects"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("projects should tolerate missing config, got %v", err)
	}
	if !strings.Contains(stdout.String(), "== Configured Projects ==") {
		t.Fatalf("expected projects header even with missing config\n%s", stdout.String())
	}
}

func TestRunProjects_JSONOutput(t *testing.T) {
	dir := t.TempDir()
	withCwd(t, dir)
	cfg := writeProjectsConfig(t, dir, "alpha", "beta")
	_, all, _ := loadProjectOrErr(cfg, "")

	ctx, stdout, _ := newTestContext(nil, true)
	ctx.AllProjects = all
	ctx.ProjectSlug = ""
	if err := runProjects(ctx, nil); err != nil {
		t.Fatalf("runProjects --json: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{`"slug": "alpha"`, `"slug": "beta"`, `"default": true`} {
		if !strings.Contains(out, want) {
			t.Fatalf("JSON output missing %q\n%s", want, out)
		}
	}
}

func TestNotFoundError_MultiProjectListsProjects(t *testing.T) {
	dir := t.TempDir()
	withCwd(t, dir)
	cfg := writeProjectsConfig(t, dir, "alpha", "beta")
	_, all, _ := loadProjectOrErr(cfg, "alpha")
	ctx := &Context{
		Project:     &all[0],
		AllProjects: all,
		ProjectSlug: "alpha",
	}
	msg := notFoundError(ctx, "missing").Error()
	for _, want := range []string{"issue not found: missing", "Searched project: alpha", "Available projects:", "alpha", "beta", "issue-cli --project <slug>"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("multi-project not-found missing %q\n%s", want, msg)
		}
	}
}

func TestRun_EnvVarSuppliesConfigPath(t *testing.T) {
	// When the viewer dispatches a bot it exports ISSUE_VIEWER_CONFIG so the
	// CLI can resolve --project against the same config the human is browsing.
	// The default "projects.yaml" filename does not exist here — the test
	// passes only if the env var is being honored.
	dir := t.TempDir()
	withCwd(t, dir)
	cfgPath := filepath.Join(dir, "projects-mfranc.yaml")
	if err := os.WriteFile(cfgPath, []byte("projects:\n  - name: Alpha\n    slug: alpha\n    issues: "+filepath.Join(dir, "alpha-issues")+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "alpha-issues"), 0755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}
	t.Setenv("ISSUE_VIEWER_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"projects"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run projects with env config: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "alpha") {
		t.Fatalf("expected env-supplied config to surface 'alpha'\n%s", stdout.String())
	}
}

func TestRun_ExplicitConfigBeatsEnvVar(t *testing.T) {
	// Precedence: explicit --config wins over $ISSUE_VIEWER_CONFIG. A human
	// debugging from a viewer-dispatched shell must still be able to point
	// the CLI at a different config without unsetting the inherited env.
	dir := t.TempDir()
	withCwd(t, dir)
	envCfg := filepath.Join(dir, "env.yaml")
	if err := os.WriteFile(envCfg, []byte("projects:\n  - name: FromEnv\n    slug: fromenv\n    issues: "+filepath.Join(dir, "env-issues")+"\n"), 0644); err != nil {
		t.Fatalf("write env config: %v", err)
	}
	flagCfg := filepath.Join(dir, "flag.yaml")
	if err := os.WriteFile(flagCfg, []byte("projects:\n  - name: FromFlag\n    slug: fromflag\n    issues: "+filepath.Join(dir, "flag-issues")+"\n"), 0644); err != nil {
		t.Fatalf("write flag config: %v", err)
	}
	for _, sub := range []string{"env-issues", "flag-issues"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	t.Setenv("ISSUE_VIEWER_CONFIG", envCfg)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--config", flagCfg, "projects"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run with explicit --config: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "fromflag") {
		t.Fatalf("expected explicit --config to win, got:\n%s", out)
	}
	if strings.Contains(out, "fromenv") {
		t.Fatalf("env-var config should be ignored when --config is explicit, got:\n%s", out)
	}
}
