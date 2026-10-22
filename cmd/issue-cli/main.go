package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

//go:embed CHANGELOG.md
var changelogMD string

// releasesRepo is the GitHub repo `process changes` pulls release history
// from. CHANGELOG.md is the offline fallback when the API is unreachable.
const releasesRepo = "michal-franc/ai-workflow-engine"

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
}

// fetchReleases is a package-level var so tests can stub network calls.
var fetchReleases = fetchReleasesFromGitHub

func fetchReleasesFromGitHub(repo string) ([]githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=20", repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api %s", resp.Status)
	}
	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}

// logAction records every CLI invocation to a session log so countRecentRetries
// can detect when the user is retrying the same failing command.
func logAction(args []string) {
	entry := map[string]interface{}{
		"ts":   time.Now().UTC().Format(time.RFC3339),
		"pid":  os.Getpid(),
		"ppid": os.Getppid(),
		"args": args,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')

	logDir := filepath.Join(os.TempDir(), "issue-cli-logs")
	os.MkdirAll(logDir, 0755)
	if f, err := os.OpenFile(filepath.Join(logDir, "actions.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		f.Write(line)
		f.Close()
	}
	if cliLog := os.Getenv("ISSUE_CLI_LOG"); cliLog != "" {
		if f, err := os.OpenFile(cliLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			f.Write(line)
			f.Close()
		}
	}
	if agentLog := resolveAgentClilog(args); agentLog != "" {
		os.MkdirAll(filepath.Dir(agentLog), 0755)
		if f, err := os.OpenFile(agentLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			f.Write(line)
			f.Close()
		}
	}
}

// resolveAgentClilog derives the per-agent clilog path from the invocation
// args alone — independent of ISSUE_CLI_LOG. The viewer's tmux dispatch sets
// that env var, but agents that run `issue-cli` via tooling outside the tmux
// session (Claude Code's Bash tool, IDE terminals, etc.) don't inherit it, so
// transitions/checks vanish from the per-agent timeline. By looking up the
// issue's assignee through --project + slug, every invocation can write to
// the right clilog regardless of how the shell was launched.
//
// Returns "" when the path can't be derived (no project, command isn't
// issue-targeting, issue not found, issue has no assignee) — those cases are
// the same as today and fall through silently.
func resolveAgentClilog(args []string) string {
	stripped := stripCLIGlobalFlags(args)
	if len(stripped) < 2 {
		return ""
	}
	slug := stripped[1]
	if strings.HasPrefix(slug, "-") {
		return ""
	}
	configPath := findConfig(extractCLIFlag(args, "--config")).Path
	if configPath == "" {
		return ""
	}
	projSlug := extractCLIFlag(args, "--project")
	projects, err := tracker.LoadProjects(configPath)
	if err != nil || len(projects) == 0 {
		return ""
	}
	var proj *tracker.Project
	for i := range projects {
		if projSlug != "" {
			if projects[i].Slug == projSlug {
				proj = &projects[i]
				break
			}
		} else if i == 0 {
			proj = &projects[i]
		}
	}
	if proj == nil || proj.IssueDir == "" {
		return ""
	}
	// Issues are named by `<number>.md` or `<slug>.md` depending on project
	// convention, so scan the directory and match on the parsed slug rather
	// than guessing a filename. LoadIssues is cheap (one read per issue file).
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return ""
	}
	var issue *tracker.Issue
	for _, it := range issues {
		if it.Slug == slug {
			issue = it
			break
		}
	}
	if issue == nil {
		return ""
	}
	return agentClilogPath(proj, issue)
}

// agentClilogPath is the clilog of issue's assignee, "" without one.
func agentClilogPath(proj *tracker.Project, issue *tracker.Issue) string {
	if strings.TrimSpace(issue.Assignee) == "" {
		return ""
	}
	workDir := proj.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	return filepath.Join(workDir, ".agent-logs", issue.Assignee, issue.Assignee+".clilog")
}

// stripCLIGlobalFlags strips --json/--debug/--config/--project so the residual args
// start with the subcommand. Mirrors agent_timeline.go's stripGlobalFlags;
// duplicated here because that lives in the viewer's main package.
func stripCLIGlobalFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json", "--debug":
			continue
		case "--config", "--project":
			if i+1 < len(args) {
				i++
			}
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func extractCLIFlag(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// countRecentRetries reports how many consecutive identical invocations
// preceded this one in the action log (matched by parent PID + args).
func countRecentRetries() int {
	logFile := filepath.Join(os.TempDir(), "issue-cli-logs", "actions.jsonl")
	data, err := os.ReadFile(logFile)
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return 0
	}
	type entry struct {
		Args []string `json:"args"`
		PPID int      `json:"ppid"`
	}
	var current entry
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &current); err != nil {
		return 0
	}
	count := 0
	for i := len(lines) - 2; i >= 0; i-- {
		var prev entry
		if err := json.Unmarshal([]byte(lines[i]), &prev); err != nil {
			break
		}
		if prev.PPID != current.PPID {
			break
		}
		if len(prev.Args) != len(current.Args) {
			break
		}
		same := true
		for j := range prev.Args {
			if prev.Args[j] != current.Args[j] {
				same = false
				break
			}
		}
		if !same {
			break
		}
		count++
	}
	return count
}

func main() {
	logAction(os.Args[1:])
	start := time.Now()
	inv := &invocation{}
	defer func() {
		// A panicking command still gets a telemetry event; the panic then
		// propagates exactly as before.
		if r := recover(); r != nil {
			recordTelemetry(inv, os.Args[1:], nil, start, true)
			panic(r)
		}
	}()
	err := runTraced(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, inv)
	recordTelemetry(inv, os.Args[1:], err, start, false)
	if err != nil {
		var codeErr *exitCodeError
		if errors.As(err, &codeErr) {
			if codeErr.Msg != "" {
				fmt.Fprintln(os.Stderr, codeErr.Msg)
			}
			os.Exit(codeErr.Code)
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		if retries := countRecentRetries(); retries >= 2 {
			printRetryHint(os.Stderr, retries+1, err)
		}
		os.Exit(processExitCode(err))
	}
}

// processExitCode maps a run error to the process exit code: an exitCodeError's own
// code, 2 for a malformed invocation (unknown or removed flag, bad flag
// value, stray argument), and 1 for every other failure.
func processExitCode(err error) int {
	var codeErr *exitCodeError
	if errors.As(err, &codeErr) {
		return codeErr.Code
	}
	var ue *usageError
	var removed *removedFlagError
	if errors.As(err, &ue) || errors.As(err, &removed) {
		return 2
	}
	return 1
}

// printRetryHint nudges an agent stuck re-running the same failing command.
// A missing approval is the common case and retrying cannot fix it, so tell
// the agent to stop, as decorateApprovalError already did.
func printRetryHint(w io.Writer, attempts int, err error) {
	fmt.Fprintf(w, "\nhint: this same command has failed %d times in a row from this session.\n", attempts)
	if errors.Is(err, tracker.ErrApprovalMissing) {
		fmt.Fprintln(w, "hint: retrying cannot help until a human approves — tell the human what to approve,")
		fmt.Fprintln(w, "      then stop and end your turn instead of polling.")
		return
	}
	fmt.Fprintln(w, "hint: try a different approach — run 'issue-cli process' to review the workflow,")
	fmt.Fprintln(w, "      or 'issue-cli checklist <slug>' to see what's blocking.")
}

// run is the testable entry point. It separates global-flag parsing from
// per-command flag parsing so the registry can dispatch a Context-bound Run.
func run(args []string, in io.Reader, out, errw io.Writer) error {
	return runTraced(args, in, out, errw, &invocation{})
}

// runTraced is run plus bookkeeping: it fills inv with what it resolved
// (command, context, failure phase) so main can record telemetry afterwards.
func runTraced(args []string, in io.Reader, out, errw io.Writer, inv *invocation) error {
	global := flag.NewFlagSet("issue-cli", flag.ContinueOnError)
	global.SetOutput(errw)
	// The config path itself comes from findConfig (config_lookup.go):
	// --config, $ISSUE_VIEWER_CONFIG, ./projects.yaml, the running viewer's
	// config, the user config.
	configFlag := global.String("config", "", "path to projects.yaml")
	projectSlug := global.String("project", "", "select project (default: first in config)")
	jsonOut := global.Bool("json", false, "output as JSON")
	debug := global.Bool("debug", false, "print which config was used and why (stderr)")

	rest, err := splitGlobalAndCommand(args)
	if err != nil {
		inv.phase = phaseSplit
		return err
	}
	inv.global, inv.command, inv.args = rest.global, rest.command, rest.args
	if err := global.Parse(rest.global); err != nil {
		inv.phase = phaseSplit
		return err
	}
	cfgSrc := findConfig(*configFlag)
	configPath := &cfgSrc.Path
	if *debug {
		fmt.Fprintln(errw, "debug: "+cfgSrc.describe())
		if cfgSrc.Path == "" {
			fmt.Fprintln(errw, "debug: "+strings.ReplaceAll(cfgSrc.notFound().Error(), "\n", "\ndebug: "))
		}
	}
	if rest.command == "" {
		// Help must work even when --project is missing in a multi-project
		// setup, so we deliberately swallow the resolution error and surface
		// the project list (loaded best-effort) to the bot reading --help.
		proj, allProjects, _ := loadProjectOrErr(*configPath, *projectSlug)
		inv.ctx = &Context{Project: proj}
		return printHelp(out, allProjects, *projectSlug)
	}

	cmd := lookupCommand(rest.command)
	if cmd == nil {
		inv.phase = phaseUnknownCommand
		// Best-effort project so the unknown-command event lands in the
		// project's telemetry file rather than the global fallback.
		if proj, _, perr := loadProjectOrErr(*configPath, *projectSlug); perr == nil {
			inv.ctx = &Context{Project: proj}
		}
		return fmt.Errorf("unknown command: %s\n\nRun: issue-cli help", rest.command)
	}

	inv.cmd = cmd
	if rest.command != "help" && wantsHelp(rest.args) {
		ctx := &Context{Stdout: out, Stderr: errw, Stdin: in, Now: time.Now}
		inv.ctx = ctx
		return printCommandHelp(ctx, cmd, rest.command)
	}

	proj, allProjects, projErr := loadProjectOrErr(*configPath, *projectSlug)
	// Help, process, and projects are project-agnostic surfaces (top-level
	// help, releases, workflow reference, project listing); init/workflow/
	// upgrade fall back to ./workflow.yaml in the current directory; report-bug
	// files to the engine project whatever this one is. Every other
	// command must surface project-resolution errors so a nil project doesn't
	// propagate into a downstream nil-pointer panic — covers ambiguous
	// multi-project, missing config file, and unknown --project slug.
	// When --project was passed explicitly the user wants that project, so
	// always surface the error — the project-agnostic bypass is only for
	// discovery without --project.
	inv.cmd = cmd
	bypassProjErr := rest.command == "help" || rest.command == "process" || rest.command == "projects" || rest.command == "version" || rest.command == "init" || rest.command == "workflow" || rest.command == "upgrade" || rest.command == "telemetry" || rest.command == "report-bug"
	if errors.Is(projErr, errNoConfig) {
		projErr = cfgSrc.notFound()
	}
	if projErr != nil && (!bypassProjErr || *projectSlug != "") {
		inv.phase = phaseProject
		return projErr
	}
	ctx := &Context{
		JSONOutput:  *jsonOut,
		Stdout:      out,
		Stderr:      errw,
		Stdin:       in,
		Project:     proj,
		AllProjects: allProjects,
		ConfigPath:  *configPath,
		Config:      cfgSrc,
		ProjectSlug: *projectSlug,
		Now:         time.Now,
	}
	inv.ctx = ctx

	err = cmd.Run(ctx, rest.args)
	if errors.Is(err, flag.ErrHelp) {
		// FlagSet already wrote usage to ctx.Stderr; suppress the duplicate
		// error message in main()'s error path.
		return nil
	}
	return decorateApprovalError(err, ctx)
}

// decorateApprovalError appends a deep-link to the issue viewer when err
// represents a missing human-approval gate. The original error is preserved
// in the wrap chain so errors.Is/As callers (and tests) keep working.
func decorateApprovalError(err error, ctx *Context) error {
	if err == nil {
		return nil
	}
	var approvalErr *tracker.ApprovalMissingError
	if !errors.As(err, &approvalErr) {
		return err
	}
	hint := approvalHint(ctx.Project, approvalErr.Slug, approvalErr.Required)
	hint += "\n\nTell the human what needs approving, then stop and end your turn.\nDo not wait or poll: re-run this command only after the human says it is approved."
	return fmt.Errorf("%w\n\n%s", err, hint)
}

// splitArgs separates the global flags (everything before the first non-flag
// or "help") from the command name and its remaining args. The order is
// preserved so the global FlagSet sees only the global flags.
type splitArgs struct {
	global  []string
	command string
	args    []string
}

func splitGlobalAndCommand(args []string) (splitArgs, error) {
	var split splitArgs
	i := 0
	for i < len(args) {
		a := args[i]
		switch a {
		case "--config", "--project":
			if i+1 >= len(args) {
				return split, fmt.Errorf("%s requires a value", a)
			}
			split.global = append(split.global, a, args[i+1])
			i += 2
		case "--json", "--debug":
			split.global = append(split.global, a)
			i++
		case "help", "--help", "-h":
			// Treat "help" and "--help" both as the help command.
			split.command = "help"
			split.args = append([]string{}, args[i+1:]...)
			return split, nil
		default:
			split.command = a
			split.args = append([]string{}, args[i+1:]...)
			return split, nil
		}
	}
	return split, nil
}
