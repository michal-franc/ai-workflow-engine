package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"gopkg.in/yaml.v3"
)

//go:embed CHANGELOG.md
var changelogMD string

// releasesRepo is the GitHub repo `process changes` pulls release history from.
// CHANGELOG.md is the offline fallback when the API is unreachable.
const releasesRepo = "michal-franc/ai-native-project-viewer"

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

var jsonOutput bool

type transitionChecklistItem struct {
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
}

type transitionOutput struct {
	From               string                    `json:"from"`
	To                 string                    `json:"to"`
	Status             string                    `json:"status"`
	StatusOptional     bool                      `json:"status_optional,omitempty"`
	Slug               string                    `json:"slug"`
	File               string                    `json:"file"`
	SideEffects        []string                  `json:"side_effects"`
	Checklist          []transitionChecklistItem `json:"checklist"`
	BodyChanged        bool                      `json:"body_changed"`
	CommentsChanged    bool                      `json:"comments_changed"`
	NextStatus         string                    `json:"next_status,omitempty"`
	NextStatusOptional bool                      `json:"next_status_optional,omitempty"`
	OptionalNextStatuses []string                `json:"optional_next_statuses,omitempty"`
	Guidance           []string                  `json:"guidance,omitempty"`
}

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

	// Default log location
	logDir := filepath.Join(os.TempDir(), "issue-cli-logs")
	os.MkdirAll(logDir, 0755)
	if f, err := os.OpenFile(filepath.Join(logDir, "actions.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		f.Write(line)
		f.Close()
	}

	// Agent session log (set by dispatch handler via ISSUE_CLI_LOG env var)
	if cliLog := os.Getenv("ISSUE_CLI_LOG"); cliLog != "" {
		if f, err := os.OpenFile(cliLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			f.Write(line)
			f.Close()
		}
	}
}

func main() {
	logAction(os.Args[1:])
	args := os.Args[1:]

	// Parse global flags
	var filtered []string
	configPath := "projects.yaml"
	projectSlug := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOutput = true
		case "--config":
			if i+1 < len(args) {
				configPath = args[i+1]
				i++
			}
		case "--project":
			if i+1 < len(args) {
				projectSlug = args[i+1]
				i++
			}
		default:
			filtered = append(filtered, args[i])
		}
	}

	if len(filtered) == 0 {
		printHelp()
		return
	}

	cmd := filtered[0]
	cmdArgs := filtered[1:]

	switch cmd {
	case "help", "--help", "-h":
		if len(cmdArgs) > 0 {
			runProcess(cmdArgs[0], cmdArgs[1:], configPath, projectSlug)
		} else {
			printHelp()
		}
	case "process":
		topic := ""
		var topicArgs []string
		if len(cmdArgs) > 0 {
			topic = cmdArgs[0]
			topicArgs = cmdArgs[1:]
		}
		runProcess(topic, topicArgs, configPath, projectSlug)
	case "next":
		design := false
		for _, a := range cmdArgs {
			if a == "--design" {
				design = true
			}
		}
		proj := loadProject(configPath, projectSlug)
		version := flagValue(cmdArgs, "--version")
		if version == "" {
			version = proj.Version
		}
		if version == "" && !design {
			fatal("--version is required (or set version in project.yaml)\n\nExample:\n  issue-cli next --version 0.1\n  issue-cli next --design --version 0.1")
		}
		runNext(proj, design, version)
	case "start":
		requireArg(cmdArgs, "start", "<slug>")
		proj := loadProject(configPath, projectSlug)
		assignee := flagValue(cmdArgs[1:], "--assignee")
		runStart(proj, cmdArgs[0], assignee)
	case "context", "show":
		requireArg(cmdArgs, cmd, "<slug>")
		proj := loadProject(configPath, projectSlug)
		runContext(proj, cmdArgs[0])
	case "create":
		proj := loadProject(configPath, projectSlug)
		runCreate(proj, cmdArgs)
	case "transition":
		requireArg(cmdArgs, "transition", "<slug>")
		to := flagValue(cmdArgs[1:], "--to")
		if to == "" {
			// Accept positional: transition <slug> <status>
			for _, a := range cmdArgs[1:] {
				if !strings.HasPrefix(a, "--") {
					to = a
					break
				}
			}
		}
		if to == "" {
			fatal("--to is required\n\nExamples:\n  issue-cli transition %s --to \"testing\"\n  issue-cli transition %s --to \"waiting-for-team-input\" --field waiting=\"design review\"", cmdArgs[0], cmdArgs[0])
		}
		fields, ferr := parseFieldFlags(cmdArgs[1:])
		if ferr != nil {
			fatal("%v", ferr)
		}
		proj := loadProject(configPath, projectSlug)
		runTransition(proj, cmdArgs[0], to, fields)
	case "claim":
		requireArg(cmdArgs, "claim", "<slug>")
		assignee := flagValue(cmdArgs[1:], "--assignee")
		if assignee == "" {
			assignee = agentNameForSlug(cmdArgs[0])
		}
		proj := loadProject(configPath, projectSlug)
		runClaim(proj, cmdArgs[0], assignee)
	case "unclaim":
		requireArg(cmdArgs, "unclaim", "<slug>")
		proj := loadProject(configPath, projectSlug)
		runUnclaim(proj, cmdArgs[0])
	case "done":
		requireArg(cmdArgs, "done", "<slug>")
		proj := loadProject(configPath, projectSlug)
		runDone(proj, cmdArgs[0])
	case "comment":
		requireArg(cmdArgs, "comment", "<slug>")
		text := textFlagValue(cmdArgs[1:], "--text")
		if text == "" {
			text = textFlagValue(cmdArgs[1:], "--body")
		}
		if text == "" {
			// Treat all remaining args after slug as the comment text
			var parts []string
			for _, a := range cmdArgs[1:] {
				if !strings.HasPrefix(a, "--") {
					parts = append(parts, a)
				}
			}
			text = strings.Join(parts, " ")
		}
		if text == "" {
			fatal("Text is required\n\nExample:\n  issue-cli comment %s \"your comment here\"\n  issue-cli comment %s --text \"your comment here\"", cmdArgs[0], cmdArgs[0])
		}
		proj := loadProject(configPath, projectSlug)
		runComment(proj, cmdArgs[0], text)
	case "checklist":
		requireArg(cmdArgs, "checklist", "<slug>")
		proj := loadProject(configPath, projectSlug)
		runChecklist(proj, cmdArgs[0])
	case "check":
		requireArg(cmdArgs, "check", "<slug>")
		if len(cmdArgs) < 2 {
			fatal("check requires a query\n\nExample:\n  issue-cli check <slug> \"Code changes complete\"")
		}
		proj := loadProject(configPath, projectSlug)
		query := strings.Join(cmdArgs[1:], " ")
		runCheck(proj, cmdArgs[0], query)
	case "list":
		proj := loadProject(configPath, projectSlug)
		// Inject project version as default if --version not provided
		version := flagValue(cmdArgs, "--version")
		if version == "" && proj.Version != "" {
			cmdArgs = append(cmdArgs, "--version", proj.Version)
		}
		runList(proj, cmdArgs)
	case "search":
		requireArg(cmdArgs, "search", "<query>")
		proj := loadProject(configPath, projectSlug)
		runSearch(proj, strings.Join(cmdArgs, " "))
	case "update":
		requireArg(cmdArgs, "update", "<slug>")
		proj := loadProject(configPath, projectSlug)
		runUpdate(proj, cmdArgs[0], cmdArgs[1:])
	case "set-meta":
		requireArg(cmdArgs, "set-meta", "<slug>")
		key := flagValue(cmdArgs[1:], "--key")
		value := textFlagValue(cmdArgs[1:], "--value")
		clear := hasFlag(cmdArgs[1:], "--clear")
		if key == "" {
			fatal("set-meta requires --key\n\nExamples:\n  issue-cli set-meta %s --key waiting --value \"waiting on design review\"\n  issue-cli set-meta %s --key waiting --clear", cmdArgs[0], cmdArgs[0])
		}
		if clear && value != "" {
			fatal("set-meta: --value and --clear are mutually exclusive")
		}
		if !clear && value == "" {
			fatal("set-meta requires --value or --clear\n\nExamples:\n  issue-cli set-meta %s --key waiting --value \"...\"\n  issue-cli set-meta %s --key waiting --clear", cmdArgs[0], cmdArgs[0])
		}
		proj := loadProject(configPath, projectSlug)
		runSetMeta(proj, cmdArgs[0], key, value, clear)
	case "stats":
		proj := loadProject(configPath, projectSlug)
		runStats(proj)
	case "append":
		requireArg(cmdArgs, "append", "<slug>")
		text := textFlagValue(cmdArgs[1:], "--body")
		section := flagValue(cmdArgs[1:], "--section")
		force := hasFlag(cmdArgs[1:], "--force")
		if text == "" {
			text = textFlagValue(cmdArgs[1:], "--text")
		}
		if text == "" {
			fatal("append requires --body\n\nExamples:\n  issue-cli append <slug> --section \"Design\" --body \"- [ ] edge case covered\"\n  issue-cli append <slug> --body \"## Test Plan\n\n### Automated\n- test 1\"")
		}
		proj := loadProject(configPath, projectSlug)
		runAppend(proj, cmdArgs[0], section, text, force)
	case "replace":
		requireArg(cmdArgs, "replace", "<slug>")
		text := textFlagValue(cmdArgs[1:], "--body")
		section := flagValue(cmdArgs[1:], "--section")
		force := hasFlag(cmdArgs[1:], "--force")
		if text == "" {
			text = textFlagValue(cmdArgs[1:], "--text")
		}
		if strings.TrimSpace(section) == "" {
			fatal("replace requires --section\n\nExample:\n  issue-cli replace %s --section \"Design\" --body \"updated approach\"", cmdArgs[0])
		}
		if text == "" {
			fatal("replace requires --body\n\nExample:\n  issue-cli replace %s --section \"Design\" --body \"updated approach\"", cmdArgs[0])
		}
		proj := loadProject(configPath, projectSlug)
		runReplace(proj, cmdArgs[0], section, text, force)
	case "retrospective":
		requireArg(cmdArgs, "retrospective", "<slug>")
		text := textFlagValue(cmdArgs[1:], "--body")
		if text == "" {
			text = textFlagValue(cmdArgs[1:], "--text")
		}
		if text == "" {
			var parts []string
			for _, a := range cmdArgs[1:] {
				if !strings.HasPrefix(a, "--") {
					parts = append(parts, a)
				}
			}
			text = strings.Join(parts, " ")
		}
		if text == "" {
			fatal("retrospective requires --body\n\nExample:\n  issue-cli retrospective %s --body \"Base workflow: ...\nSubsystem workflow: ...\nTooling friction: ...\"", cmdArgs[0])
		}
		proj := loadProject(configPath, projectSlug)
		runRetrospective(proj, cmdArgs[0], text)
	case "report-bug":
		if len(cmdArgs) < 1 {
			fatal("report-bug requires a description\n\nExample:\n  issue-cli report-bug \"transition command rejects valid status name with trailing space\"")
		}
		runReportBug(strings.Join(cmdArgs, " "))
	case "data":
		if len(cmdArgs) < 1 {
			fatal("data requires a subcommand\n\nUsage:\n  issue-cli data add <slug> --description \"...\" [--status <s>]\n  issue-cli data list <slug> [--json]\n  issue-cli data set-status <slug> <id> <status>\n  issue-cli data set-comment <slug> <id> --text \"...\"\n  issue-cli data remove <slug> <id>")
		}
		proj := loadProject(configPath, projectSlug)
		runData(proj, cmdArgs[0], cmdArgs[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\nRun: issue-cli help\n", cmd)
		os.Exit(1)
	}
}

// --- Helpers ---

func loadProject(configPath, projectSlug string) *tracker.Project {
	// If local ./issues/ exists, use it directly — no config needed
	if info, err := os.Stat("./issues"); err == nil && info.IsDir() {
		docsDir := "./docs"
		if info, err := os.Stat(docsDir); err != nil || !info.IsDir() {
			docsDir = ""
		}
		// Derive project name from current directory
		cwd, _ := os.Getwd()
		name := filepath.Base(cwd)
		proj := &tracker.Project{
			Name:     name,
			Slug:     tracker.Slugify(name),
			IssueDir: "./issues",
			DocsDir:  docsDir,
		}
		// Load overrides from project.yaml or projects.yaml if present
		for _, f := range []string{"project.yaml", configPath} {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			// Try single project format first (project.yaml)
			var local tracker.Project
			if yaml.Unmarshal(data, &local) == nil {
				if local.Version != "" {
					proj.Version = local.Version
				}
				if local.WorkflowFile != "" {
					proj.WorkflowFile = local.WorkflowFile
				}
				if proj.Version != "" {
					break
				}
			}
			// Try multi-project format (projects.yaml)
			var cfg tracker.ProjectsConfig
			if yaml.Unmarshal(data, &cfg) == nil {
				for _, p := range cfg.Projects {
					if p.Version != "" {
						proj.Version = p.Version
						break
					}
					if p.WorkflowFile != "" {
						proj.WorkflowFile = p.WorkflowFile
					}
				}
			}
			if proj.Version != "" {
				break
			}
		}
		return proj
	}

	// Fall back to config file
	projects, err := tracker.LoadProjects(configPath)
	if err != nil {
		fatal("No ./issues/ directory found and cannot load config: %v\n\nEither run from a project root with an issues/ directory, or use --config <path>", err)
	}
	if projectSlug != "" {
		for i := range projects {
			if projects[i].Slug == projectSlug {
				return &projects[i]
			}
		}
		fatal("Project '%s' not found in config", projectSlug)
	}
	if len(projects) == 0 {
		fatal("No projects configured in %s", configPath)
	}
	return &projects[0]
}

func findIssue(proj *tracker.Project, slug string) (*tracker.Issue, []*tracker.Issue) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		fatal("Cannot load issues: %v", err)
	}

	// Normalize: strip .md extension, strip leading issue dir path, lowercase
	normalized := slug
	normalized = strings.TrimSuffix(normalized, ".md")
	// Strip leading path to issue dir (e.g. "issues/Combat/foo" → "Combat/foo")
	if rel, err := filepath.Rel(proj.IssueDir, normalized); err == nil && !strings.HasPrefix(rel, "..") {
		normalized = rel
	}
	normalizedLower := strings.ToLower(normalized)

	// Exact match (case-insensitive)
	for _, issue := range issues {
		if strings.ToLower(issue.Slug) == normalizedLower {
			return issue, issues
		}
	}
	// Try partial match (case-insensitive)
	for _, issue := range issues {
		slugLower := strings.ToLower(issue.Slug)
		if strings.HasSuffix(slugLower, "/"+normalizedLower) || strings.Contains(slugLower, normalizedLower) {
			return issue, issues
		}
	}
	fatal("Issue not found: %s\n\nRun: issue-cli list", slug)
	return nil, nil
}

func agentNameForSlug(slug string) string {
	if name := os.Getenv("AGENT_NAME"); name != "" {
		return name
	}
	base := filepath.Base(slug)
	return "agent-" + base
}

func projectRoot(proj *tracker.Project) string {
	if proj != nil && proj.WorkDir != "" {
		return proj.WorkDir
	}
	if proj != nil && proj.IssueDir != "" {
		if abs, err := filepath.Abs(proj.IssueDir); err == nil {
			return filepath.Dir(abs)
		}
		return filepath.Dir(proj.IssueDir)
	}
	cwd, _ := os.Getwd()
	return cwd
}

func sanitizePathPart(s string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-", "\t", "-")
	return r.Replace(strings.TrimSpace(s))
}

func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func textFlagValue(args []string, flag string) string {
	return normalizeEscapedText(flagValue(args, flag))
}

func normalizeEscapedText(s string) string {
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		`\r\n`, "\n",
		`\n`, "\n",
		`\r`, "\r",
		`\t`, "\t",
	)
	return replacer.Replace(s)
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// parseFieldFlags collects repeated `--field key=value` flags into a map.
// Used by `transition` to supply declarative `fields[]` answers inline so the
// CLI can satisfy required frontmatter fields without a separate set-meta call.
func parseFieldFlags(args []string) (map[string]string, error) {
	out := map[string]string{}
	for i := 0; i < len(args); i++ {
		if args[i] != "--field" {
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("--field requires a key=value argument")
		}
		kv := args[i+1]
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			return nil, fmt.Errorf("--field expects key=value, got %q", kv)
		}
		key := strings.TrimSpace(kv[:idx])
		val := normalizeEscapedText(kv[idx+1:])
		if key == "" {
			return nil, fmt.Errorf("--field key cannot be empty (got %q)", kv)
		}
		out[key] = val
		i++
	}
	return out, nil
}

func requireArg(args []string, cmd, argName string) {
	if len(args) == 0 {
		fatal("%s requires %s\n\nExample:\n  issue-cli %s <slug>", cmd, argName, cmd)
	}
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	if retries := countRecentRetries(); retries >= 2 {
		fmt.Fprintf(os.Stderr, "\nhint: this same command has failed %d times in a row from this session.\n", retries+1)
		fmt.Fprintf(os.Stderr, "hint: try a different approach — run 'issue-cli process' to review the workflow,\n")
		fmt.Fprintf(os.Stderr, "      or 'issue-cli checklist <slug>' to see what's blocking.\n")
	}
	os.Exit(1)
}

// countRecentRetries checks the action log for consecutive identical commands
// from the same parent process. Returns how many prior identical entries exist.
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

	// Current entry is the last line
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

func outputJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// --- Commands ---

func printHelp() {
	fmt.Print(`== issue-cli — AI-Native Project Viewer CLI ==

Commands:
  process              Learn how this project works (run this first)
  start <slug>         *** USE THIS TO BEGIN WORK *** Picks up an issue from any status — claims, advances handoff states, shows checklist + next steps
  next --version <v>   Find work for a version (default: from project.yaml)
  next --design        Find ideas and in-design issues needing design
  context <slug>       Full context dump for an issue (alias: show)
  create               Create a new issue
  transition <slug>    Move issue to next status (strict ordering)
  claim <slug>         Only set assignee (does NOT start work — use 'start' instead)
  unclaim <slug>       Remove assignee from an issue
  done <slug>          Mark issue as done (validates and auto-unclaims)
  comment <slug>       Add a comment to an issue
  check <slug> <text>  Check off a checkbox item by text match
  checklist <slug>     Show checkbox status for an issue
  list                 List issues with filters (--status open|closed|<name>, --sort score)
  search <query>       Search issues (supports regex, e.g. "foo|bar")
  update <slug>        Replace issue body (--body "content"), preserves frontmatter
  set-meta <slug>      Set/clear a frontmatter field (--key <k> --value "v" | --clear)
  append <slug>        Append content to issue body (--body "...", or --section "X" --body "...")
                       If --body starts with an existing heading, it auto-routes into that section.
  replace <slug>       Replace content of an existing section (--section "X" --body "...")
  retrospective <slug> Save workflow feedback under retros/ in the project
  stats                Project health overview
  report-bug <desc>    Report a bug in issue-cli itself
  data <sub> <slug>    Per-issue structured data store (sub: add|list|set-status|set-comment|remove)

Global flags:
  --config <path>      Path to projects.yaml (default: projects.yaml)
  --project <slug>     Select project (default: first in config)
  --json               Output as JSON

First time? Run these:
  1. issue-cli process
  2. issue-cli next
  3. issue-cli start <slug>   # works from any status — transitions from backlog/human-testing need approval
`)
}

func runProcess(topic string, args []string, configPath, projectSlug string) {
	switch topic {
	case "", "all":
		fmt.Print(`== AI-Native Project Viewer ==

You are working with a markdown-based issue tracker.
Issues are .md files in issues/<System>/ directories.

== Workflow ==
Every issue follows this lifecycle:
  idea → in design → backlog → in progress → testing → human-testing → documentation → shipping → done

== What each status means ==
  idea           Raw concept, just a title and rough description
  in design      Fleshing out requirements, approach, edge cases
  backlog        Designed and ready to implement
  in progress    Actively being worked on
  testing        Implementation done, verifying correctness
  human-testing  Manual verification by humans
  documentation  Updating docs to reflect the change
  shipping       Committing and pushing the changes
  done           Shipped, tested, documented

== Rules ==
  - Always use 'start' to begin work once the issue is approved for in progress in the viewer
  - Do NOT use 'claim' to begin work — it only sets assignee without starting
  - Never skip statuses — follow the order strictly
  - Always update docs before marking done
  - Reference other issues with #<slug> in the body
  - Use checkboxes [x] to track subtasks and acceptance criteria

== IMPORTANT: Command output ==
  - NEVER suppress stderr (no 2>/dev/null) — errors contain critical workflow guidance
  - NEVER use || true to ignore failures — non-zero exit codes mean something went wrong
  - ALWAYS read and act on the full output of every command — it contains next steps
  - If a command fails, fix the issue it describes, do not retry blindly

== Build/test failures from unrelated WIP files ==
  - If 'make test' or the build fails due to pre-existing untracked/WIP files unrelated to
    your issue, document the failure in a comment (issue-cli comment <slug> --text "..."),
    note that the cause is unrelated to your changes, and stop — do NOT modify, stash, or
    delete those files

== When you pick up an issue ==
  1. If the issue is in backlog, confirm it is approved for in-progress in the viewer
  2. issue-cli start <slug>          — starts the approved issue, claims it, and shows next steps
  3. If 'start' says approval is missing, stop and ask the human to approve it in the viewer
  4. Do the work, check off items
  5. Add ## Test Plan section with ### Automated and ### Manual
  6. issue-cli transition <slug> --to "testing"
  7. Log test results: issue-cli comment <slug> --text "tests: ..."
  8. issue-cli transition <slug> --to "documentation"
  9. Update docs: issue-cli comment <slug> --text "docs: ..."
  10. issue-cli transition <slug> --to "shipping"
  11. Commit and push the changes; check off the Shipping section
  12. issue-cli done <slug>

== Quick start ==
  issue-cli next --version 0.1    — find work for version 0.1
  issue-cli start <slug>          — pick up an issue at any status (claims + advances handoff states)
  issue-cli done <slug>           — finish when complete

Run 'issue-cli process <topic>' for details:
  workflow, format, transitions, schema, changes, testing, docs, systems, references
`)
	case "workflow":
		proj := loadProject(configPath, projectSlug)
		wf := proj.LoadWorkflow()
		statusOrder := wf.GetStatusOrder()
		statusDescs := wf.GetStatusDescriptions()
		fmt.Println("== Status Lifecycle ==")
		for i, s := range statusOrder {
			desc := statusDescs[s]
			if i > 0 {
				fmt.Print("  → ")
			} else {
				fmt.Print("  ")
			}
			name := s
			if st := wf.GetStatus(s); st != nil && st.Optional {
				name = s + " (optional)"
			}
			if desc != "" {
				fmt.Printf("%-24s  %s\n", name, desc)
			} else {
				fmt.Printf("%s\n", name)
			}
		}
	case "transitions":
		proj := loadProject(configPath, projectSlug)
		runProcessTransitions(proj, args)
	case "format":
		fmt.Print(`== Issue File Format ==

---
title: "Fix heat calculation overflow"
status: "idea"
system: "Combat"
version: "0.1"
assignee: "my-bot"
priority: "high"
labels:
  - bug
  - combat
---

Description in markdown. Supports checkboxes and #<slug> references.

== Required ==
  title — issue title (used to generate the URL slug)

== Optional ==
  status, system, version, assignee, priority, labels, created
`)
	case "testing":
		fmt.Print(`== Test Plan Convention ==

Before transitioning from testing → documentation, the issue body
must contain a ## Test Plan section with two subsections:

## Test Plan

### Automated
List tests you wrote. Include file paths and what they verify.
- [x] Unit: path/to/test.cs — what it tests
- [x] Integration: path/to/test.go — what it tests
- [ ] E2E: not applicable

### Manual
Steps for a human to perform. You design these but cannot check them off.
- [ ] Load a mech with 5+ heat sinks, fire all weapons
- [ ] Verify heat bar stays in range

Rules:
  - ### Automated must have at least one entry
  - ### Manual must have at least one entry
  - You check off ### Automated as you write tests
  - Only humans check off ### Manual items
  - A test results comment is required:
    issue-cli comment <slug> --text "tests: 3 unit tests added, all passing"

To add a Test Plan section to an issue:
  issue-cli append <slug> --body "## Test Plan

### Automated
- description of test

### Manual
- step for human to verify"
`)
	case "docs":
		fmt.Print(`== Documentation Convention ==

Before marking an issue as done, you must log what docs were updated:

  issue-cli comment <slug> --text "docs: updated combat/heat.md"
  issue-cli comment <slug> --text "docs: not needed, internal refactor"

The "docs:" prefix is required. This is an honor system — the CLI
validates that the comment exists but trusts its content.
`)
	case "systems":
		proj := loadProject(configPath, projectSlug)
		issues, _ := tracker.LoadIssues(proj.IssueDir)
		_, systems, _, _, _ := tracker.CollectFilterValues(issues)
		// Include empty subdirectories as systems too
		subdirSystems := tracker.CollectSubdirSystems(proj.IssueDir)
		seen := map[string]bool{}
		for _, s := range systems {
			seen[s] = true
		}
		for _, s := range subdirSystems {
			if !seen[s] {
				systems = append(systems, s)
				seen[s] = true
			}
		}
		sort.Strings(systems)
		fmt.Println("== Available Systems ==")
		for _, s := range systems {
			fmt.Printf("  %s\n", s)
		}
	case "schema":
		runProcessSchema()
	case "changes", "changelog", "versions":
		runProcessChanges()
	case "references":
		fmt.Print(`== Issue References ==

Use #<slug> in the issue body to link to other issues:
  See #combat/fix-heat-overflow for related work.
  This depends on #321 (looked up by filename).

References are resolved by:
  1. Full slug match (e.g. #combat/fix-heat-overflow)
  2. Title slug match (e.g. #fix-heat-overflow)
  3. Filename match (e.g. #321 if the file is 321.md)
`)
	default:
		fmt.Fprintf(os.Stderr, "Unknown topic: %s\n\nAvailable: workflow, format, transitions, schema, changes, testing, docs, systems, references\n", topic)
		os.Exit(1)
	}
}

func runProcessTransitions(proj *tracker.Project, args []string) {
	wf := proj.LoadWorkflow()

	var (
		system        string
		systemSource  string
		issueRef      string
		hasIssueRef   bool
	)

	systemFlag := flagValue(args, "--system")
	workflowFlag := flagValue(args, "--workflow")
	if systemFlag == "" {
		systemFlag = workflowFlag
	}

	for _, a := range args {
		if a == "--system" || a == "--workflow" {
			break
		}
		if strings.HasPrefix(a, "--") {
			continue
		}
		issueRef = a
		hasIssueRef = true
		break
	}

	var issueSlug string
	switch {
	case hasIssueRef:
		issue, _ := findIssue(proj, issueRef)
		system = issue.System
		issueSlug = issue.Slug
		systemSource = fmt.Sprintf("issue %s", issue.Slug)
	case systemFlag != "":
		system = systemFlag
		systemSource = fmt.Sprintf("--system %s", systemFlag)
	}

	scoped := wf
	if strings.TrimSpace(system) != "" {
		scoped = wf.ForSystem(system)
	}

	header := "== Transition Rules =="
	switch {
	case hasIssueRef && system != "":
		header = fmt.Sprintf("== Transition Rules — system %q (%s) ==", system, systemSource)
	case hasIssueRef:
		header = fmt.Sprintf("== Transition Rules — issue %s (no system overlay; project default) ==", issueSlug)
	case system != "":
		header = fmt.Sprintf("== Transition Rules — system %q ==", system)
	}
	fmt.Println(header)
	fmt.Println()

	statusOrder := scoped.GetStatusOrder()
	if len(statusOrder) > 0 {
		first := statusOrder[0]
		fmt.Printf("  → %-26s  Initial state — title only\n", first)
	}

	rendered := map[string]bool{}
	for _, s := range statusOrder {
		for _, t := range scoped.Transitions {
			if t.To != s {
				continue
			}
			key := t.From + "→" + t.To
			if rendered[key] {
				continue
			}
			rendered[key] = true
			renderTransition(t, scoped)
		}
	}
	// Render any transitions whose From or To isn't in the lifecycle (defensive).
	for _, t := range scoped.Transitions {
		key := t.From + "→" + t.To
		if rendered[key] {
			continue
		}
		rendered[key] = true
		renderTransition(t, scoped)
	}

	fmt.Println()
	fmt.Println("Transitions are strict — you cannot skip required statuses.")

	var optional []string
	var globalStatuses []string
	for _, st := range scoped.Statuses {
		if st.Optional {
			optional = append(optional, st.Name)
		}
		if st.Global {
			globalStatuses = append(globalStatuses, st.Name)
		}
	}
	if len(optional) > 0 {
		fmt.Printf("\nOptional statuses (skippable on forward transitions): %s\n", strings.Join(optional, ", "))
	}
	if len(globalStatuses) > 0 {
		fmt.Printf("Global statuses (transitions from them to any status are allowed): %s\n", strings.Join(globalStatuses, ", "))
	}

	if system == "" && len(scoped.Systems) > 0 {
		var names []string
		for name := range scoped.Systems {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Println()
		fmt.Println("This is the project's default workflow. Per-system overlays are configured for:")
		fmt.Printf("  %s\n", strings.Join(names, ", "))
		fmt.Println("Run 'issue-cli process transitions --system <name>' or 'issue-cli process transitions <issue-slug>' to see the rules for a specific workflow.")
	}
}

func renderTransition(t tracker.WorkflowTransition, wf *tracker.WorkflowConfig) {
	from := t.From
	if from == "" {
		from = "(any)"
	}
	if from == "*" {
		from = "* (any)"
	}
	label := fmt.Sprintf("%s → %s", from, t.To)
	descs := transitionActionDescriptions(t, wf)
	if len(descs) == 0 {
		fmt.Printf("  %-28s  (no rules)\n", label)
		return
	}
	fmt.Printf("  %-28s  %s\n", label, descs[0])
	for _, d := range descs[1:] {
		fmt.Printf("  %-28s    %s\n", "", d)
	}
}

func transitionActionDescriptions(t tracker.WorkflowTransition, wf *tracker.WorkflowConfig) []string {
	var descs []string
	for _, action := range t.Actions {
		desc := tracker.DescribeAction(action, t.To)
		if strings.TrimSpace(desc) == "" {
			continue
		}
		descs = append(descs, desc)
	}
	for _, f := range t.Fields {
		target := strings.TrimSpace(f.Target)
		if target == "" {
			target = "frontmatter"
		}
		qualifier := "Prompts for"
		if f.Required {
			qualifier = "Required"
		}
		switch {
		case target == "frontmatter":
			descs = append(descs, fmt.Sprintf("%s frontmatter field %q (set via `--field %s=…` or `set-meta`)", qualifier, f.Name, f.Name))
		case strings.HasPrefix(target, "section:"):
			section := strings.TrimSpace(strings.TrimPrefix(target, "section:"))
			descs = append(descs, fmt.Sprintf("%s field %q appended to ## %s (set via `--field %s=…`)", qualifier, f.Name, section, f.Name))
		default:
			descs = append(descs, fmt.Sprintf("%s field %q (target: %s)", qualifier, f.Name, target))
		}
	}
	return descs
}

func runProcessSchema() {
	fmt.Println("== workflow.yaml schema ==")
	fmt.Println()
	fmt.Println("Driven off Go struct tags — every field the parser honors appears here.")
	fmt.Println("Edit workflow.yaml at the project root (or demo/workflow.yaml for the demo).")

	for _, section := range tracker.WorkflowSchemaSections() {
		fmt.Println()
		fmt.Printf("== %s  (struct: %s) ==\n", section.Path, section.Title)
		width := maxFieldWidth(section.Fields)
		for _, f := range section.Fields {
			name := f.Name
			if f.Optional {
				name += "?"
			}
			desc := f.Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Printf("  %-*s  %-24s  %s\n", width, name, f.Type, desc)
		}
	}

	fmt.Println()
	fmt.Println("== Action types (transitions[].actions[].type) ==")
	w := maxNamedWidth(tracker.WorkflowActionTypes)
	for _, a := range tracker.WorkflowActionTypes {
		fmt.Printf("  %-*s  %s\n", w, a.Name, a.Description)
	}

	fmt.Println()
	fmt.Println("== Validation rules (actions[].rule when type=validate) ==")
	w = maxNamedWidth(tracker.WorkflowValidationRules)
	for _, r := range tracker.WorkflowValidationRules {
		fmt.Printf("  %-*s  %s\n", w, r.Name, r.Description)
	}

	fmt.Println()
	fmt.Println("Fields marked with ? are optional (yaml omitempty).")
	fmt.Println("Run 'issue-cli process changes' to see when features were added.")
}

func runProcessChanges() {
	releases, err := fetchReleases(releasesRepo)
	if err == nil && len(releases) > 0 {
		printReleases(releases)
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch releases from github (%v); using embedded CHANGELOG.md\n", err)
	}
	printEmbeddedChangelog()
}

func printReleases(releases []githubRelease) {
	fmt.Println("== issue-cli / workflow release history ==")
	fmt.Printf("(from https://github.com/%s/releases)\n\n", releasesRepo)
	printed := 0
	for _, r := range releases {
		if r.Draft {
			continue
		}
		title := r.Name
		if title == "" {
			title = r.TagName
		}
		date := r.PublishedAt
		if t, perr := time.Parse(time.RFC3339, date); perr == nil {
			date = t.Format("2006-01-02")
		}
		fmt.Printf("## %s — %s\n\n", title, date)
		if body := strings.TrimSpace(r.Body); body != "" {
			fmt.Println(body)
			fmt.Println()
		}
		printed++
		if printed >= 20 {
			break
		}
	}
	fmt.Println("Run 'issue-cli process schema' to see the current workflow.yaml schema.")
}

func printEmbeddedChangelog() {
	if strings.TrimSpace(changelogMD) == "" {
		fmt.Println("(no changelog embedded)")
		return
	}
	fmt.Println("== issue-cli / workflow release history (offline) ==")
	fmt.Println()
	trimmed, omitted := trimChangelogToVersions(changelogMD, 20)
	fmt.Print(trimmed)
	if !strings.HasSuffix(trimmed, "\n") {
		fmt.Println()
	}
	if omitted > 0 {
		fmt.Printf("\n(%d older version entries omitted; see cmd/issue-cli/CHANGELOG.md for the full history)\n", omitted)
	}
	fmt.Println()
	fmt.Println("Run 'issue-cli process schema' to see the current workflow.yaml schema.")
}

// trimChangelogToVersions keeps the preamble (everything before the first
// "## v" heading) plus the first `max` version sections. Returns the trimmed
// text and the number of version sections that were dropped.
func trimChangelogToVersions(md string, max int) (string, int) {
	lines := strings.Split(md, "\n")
	var preamble, kept []string
	total, seen := 0, 0
	seenFirst := false
	dropping := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## v") {
			total++
			if !seenFirst {
				seenFirst = true
			}
			if seen < max {
				seen++
				dropping = false
			} else {
				dropping = true
				continue
			}
		} else if dropping {
			continue
		}
		if !seenFirst {
			preamble = append(preamble, line)
		} else {
			kept = append(kept, line)
		}
	}
	omitted := total - seen
	if omitted < 0 {
		omitted = 0
	}
	return strings.Join(append(preamble, kept...), "\n"), omitted
}

func maxFieldWidth(fields []tracker.SchemaFieldDoc) int {
	w := 0
	for _, f := range fields {
		n := len(f.Name)
		if f.Optional {
			n++
		}
		if n > w {
			w = n
		}
	}
	return w
}

func maxNamedWidth(items []tracker.SchemaNamedDoc) int {
	w := 0
	for _, i := range items {
		if len(i.Name) > w {
			w = len(i.Name)
		}
	}
	return w
}

func runNext(proj *tracker.Project, design bool, version string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		fatal("Cannot load issues: %v", err)
	}

	// Filter by version
	if version != "" {
		var filtered []*tracker.Issue
		for _, issue := range issues {
			if issue.Version == version {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}

	priorityRank := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "": 4}

	if design {
		var matches []*tracker.Issue
		for _, issue := range issues {
			if issue.Status == "idea" || issue.Status == "in design" {
				matches = append(matches, issue)
			}
		}
		sortByPriority(matches, priorityRank)

		if jsonOutput {
			outputJSON(matches)
			return
		}

		if len(matches) == 0 {
			fmt.Printf("No issues needing design work for version %s.\n", version)
			return
		}

		fmt.Printf("== Issues needing design work (v%s) ==\n", version)
		for _, issue := range matches {
			p := issue.Priority
			if p == "" {
				p = "none"
			}
			fmt.Printf("  [%-8s] %-45s %s\n", p, issue.Slug, issue.System)
		}
		fmt.Println("\nPick one: issue-cli claim <slug>")
		return
	}

	// Collect backlog, in-progress, and testing issues
	var backlog, inProgress, testing []*tracker.Issue
	for _, issue := range issues {
		switch issue.Status {
		case "backlog":
			if issue.Assignee == "" {
				backlog = append(backlog, issue)
			}
		case "in progress":
			inProgress = append(inProgress, issue)
		case "testing":
			testing = append(testing, issue)
		}
	}

	sortByPriority(backlog, priorityRank)
	sortByPriority(inProgress, priorityRank)
	sortByPriority(testing, priorityRank)

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"backlog":     backlog,
			"in_progress": inProgress,
			"testing":     testing,
		})
		return
	}

	if len(backlog) == 0 && len(inProgress) == 0 && len(testing) == 0 {
		fmt.Printf("No work available for version %s. Try: issue-cli next --design --version %s\n", version, version)
		return
	}

	fmt.Printf("== Work for v%s ==\n", version)

	if len(inProgress) > 0 {
		fmt.Printf("\nIn Progress (%d):\n", len(inProgress))
		for _, issue := range inProgress {
			a := ""
			if issue.Assignee != "" {
				a = " @" + issue.Assignee
			}
			fmt.Printf("  [%-8s] %-45s %s%s\n", issue.Priority, issue.Slug, issue.System, a)
		}
	}

	if len(testing) > 0 {
		fmt.Printf("\nTesting (%d):\n", len(testing))
		for _, issue := range testing {
			a := ""
			if issue.Assignee != "" {
				a = " @" + issue.Assignee
			}
			fmt.Printf("  [%-8s] %-45s %s%s\n", issue.Priority, issue.Slug, issue.System, a)
		}
	}

	if len(backlog) > 0 {
		fmt.Printf("\nBacklog — unclaimed (%d):\n", len(backlog))
		for _, issue := range backlog {
			p := issue.Priority
			if p == "" {
				p = "none"
			}
			fmt.Printf("  [%-8s] %-45s %s\n", p, issue.Slug, issue.System)
		}
	}

	fmt.Println("\nPick one: issue-cli start <slug>")
}

func runStart(proj *tracker.Project, slug, assignee string) {
	issue, _ := findIssue(proj, slug)
	wf := proj.LoadWorkflowForIssue(issue)

	if assignee == "" {
		assignee = agentNameForSlug(slug)
	}

	started, err := wf.StartIssueOnce(issue.FilePath, slug, assignee)
	if err != nil {
		if strings.Contains(err.Error(), "human approval for") && strings.Contains(err.Error(), "is missing") {
			fatal("%s\n\nNext step: approve the required status in the issue viewer, then rerun:\n  issue-cli start %s", err.Error(), slug)
		}
		fatal("%v", err)
	}
	issue = started.Issue

	fmt.Printf("== Starting work on: %s ==\n", issue.Title)
	fmt.Printf("Status: %s\n", statusLabel(wf, started.FromStatus))

	if started.Claimed {
		fmt.Printf("✓ Claimed (assignee: %s)\n", assignee)
	} else if issue.Assignee != "" {
		fmt.Printf("Already claimed by: %s\n", issue.Assignee)
	}

	if started.Transitioned && started.FromStatus != started.ToStatus {
		fmt.Printf("✓ Status → %s\n", started.ToStatus)
	} else {
		fmt.Printf("Status unchanged (%s is a work status — ready to pick up)\n", started.ToStatus)
	}
	if started.Result.BodyAppended {
		fmt.Println("✓ Workflow content appended to issue body")
	}
	if started.Result.ClearedApproval {
		fmt.Println("✓ Approval consumed")
	}

	fmt.Printf("file: %s\n", issue.FilePath)
	fmt.Println()

	printWorkflowNextSteps(wf, issue)
	printStartWorkflowReminder(wf)
}

func printStartWorkflowReminder(wf *tracker.WorkflowConfig) {
	order := wf.GetStatusOrder()
	if len(order) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("== Workflow lifecycle ==")
	fmt.Printf("  %s\n", strings.Join(order, " → "))
	fmt.Println("Run 'issue-cli process workflow' or 'issue-cli process transitions' for details.")
}

func printWorkflowNextSteps(wf *tracker.WorkflowConfig, issue *tracker.Issue) {
	total, checked := tracker.CountCheckboxes(issue.BodyRaw)
	if total > 0 {
		fmt.Printf("== Checklist (%d/%d) ==\n", checked, total)
		printCheckboxes(issue.BodyRaw)
		fmt.Println()
	}

	if prompt := wf.StatusPrompt(issue.Status); prompt != "" {
		fmt.Println("== Current Status Guidance ==")
		fmt.Printf("- %s\n", prompt)
		fmt.Println()
	}

	// Show the template for current status only if not already in the body
	tmpl := wf.TemplateForStatus(issue.Status)
	if tmpl != "" {
		firstLine := strings.SplitN(tmpl, "\n", 2)[0]
		if !strings.Contains(issue.BodyRaw, firstLine) {
			fmt.Println("== Current status template ==")
			fmt.Println(tmpl)
			fmt.Println()
		}
	}

	// Show next transition, preferring the first non-optional target so agents
	// don't walk into an optional side-path by default.
	required, optionals := wf.DefaultNextStatus(issue.Status)
	next := required
	allOptional := false
	if next == "" && len(optionals) > 0 {
		next = optionals[0]
		optionals = optionals[1:]
		allOptional = true
	}
	if next != "" {
		fmt.Println("== Next ==")
		suffix := ""
		if allOptional {
			suffix = "   (optional — every remaining status is optional)"
		}
		fmt.Printf("  issue-cli transition %s --to \"%s\"%s\n", issue.Slug, next, suffix)
		if len(optionals) > 0 {
			fmt.Println()
			fmt.Println("Optional side-paths:")
			for _, opt := range optionals {
				fmt.Printf("  issue-cli transition %s --to \"%s\"\n", issue.Slug, opt)
			}
		}
		prompts := wf.EntryPrompts(issue.Status, next)
		if len(prompts) > 0 {
			fmt.Println()
			fmt.Println("== Entry Guidance ==")
			for _, prompt := range prompts {
				fmt.Printf("- %s\n", prompt)
			}
		}
	}
}

func runContext(proj *tracker.Project, slug string) {
	issue, _ := findIssue(proj, slug)

	if jsonOutput {
		comments, _ := tracker.LoadComments(issue.FilePath)
		outputJSON(map[string]interface{}{
			"issue":    issue,
			"comments": comments,
		})
		return
	}

	wf := proj.LoadWorkflowForIssue(issue)
	fmt.Printf("== %s ==\n", issue.Title)
	fmt.Printf("Status: %s | System: %s | Priority: %s | Assignee: %s\n",
		statusLabel(wf, issue.Status), issue.System, issue.Priority, issue.Assignee)
	fmt.Printf("File: %s\n\n", issue.FilePath)

	fmt.Println("== Body ==")
	fmt.Println(issue.BodyRaw)
	fmt.Println()

	total, checked := tracker.CountCheckboxes(issue.BodyRaw)
	if total > 0 {
		fmt.Printf("== Checklist (%d/%d) ==\n", checked, total)
		printCheckboxes(issue.BodyRaw)
		fmt.Println()
	}

	hasAuto, hasManual := tracker.HasTestPlan(issue.BodyRaw)
	if hasAuto || hasManual {
		fmt.Print("== Test Plan ==\n")
		if hasAuto {
			fmt.Println("  ✓ ### Automated section present")
		}
		if hasManual {
			fmt.Println("  ✓ ### Manual section present")
		}
		fmt.Println()
	}

	comments, _ := tracker.LoadComments(issue.FilePath)
	if len(comments) > 0 {
		fmt.Printf("== Comments (%d) ==\n", len(comments))
		for _, c := range comments {
			status := ""
			if c.Status == "done" {
				status = " [done]"
			}
			fmt.Printf("  [%s]%s %s\n", c.Date, status, c.Text)
		}
	}
}

func runCreate(proj *tracker.Project, args []string) {
	title := flagValue(args, "--title")
	system := flagValue(args, "--system")
	status := flagValue(args, "--status")
	priority := flagValue(args, "--priority")

	if title == "" {
		fatal("--title is required\n\nExample:\n  issue-cli create --title \"Fix heat overflow\" --system Combat --status idea")
	}
	wf := proj.LoadWorkflow()
	statusOrder := wf.GetStatusOrder()

	if status == "" {
		// Default to first non-"none" status
		status = "idea"
		for _, s := range statusOrder {
			if s != "none" {
				status = s
				break
			}
		}
	}

	// Only allow creating issues in early statuses (before backlog)
	idx := wf.GetStatusIndex(status)
	backlogIdx := wf.GetStatusIndex("backlog")
	if backlogIdx == -1 {
		backlogIdx = 3
	}
	if idx == -1 || idx >= backlogIdx {
		var allowed []string
		for _, s := range statusOrder {
			if s == "none" {
				continue
			}
			if wf.GetStatusIndex(s) < backlogIdx {
				allowed = append(allowed, "\""+s+"\"")
			}
		}
		fatal("Cannot create issue with status \"%s\" — allowed: %s", status, strings.Join(allowed, ", "))
	}

	// Determine directory
	dir := proj.IssueDir
	if system != "" {
		dir = filepath.Join(dir, system)
		os.MkdirAll(dir, 0755)
	}

	slug := tracker.Slugify(title)
	filename := filepath.Join(dir, slug+".md")

	if _, err := os.Stat(filename); err == nil {
		fatal("Issue already exists: %s\nUse 'update' to modify existing issues.", filename)
	}

	var content strings.Builder
	content.WriteString("---\n")
	content.WriteString(fmt.Sprintf("title: \"%s\"\n", strings.ReplaceAll(title, "\"", "\\\"")))
	content.WriteString(fmt.Sprintf("status: \"%s\"\n", status))
	if system != "" {
		content.WriteString(fmt.Sprintf("system: \"%s\"\n", system))
	}
	if priority != "" {
		content.WriteString(fmt.Sprintf("priority: \"%s\"\n", priority))
	}
	content.WriteString("---\n")

	// Append template for the initial status
	tmpl := wf.TemplateForStatus(status)
	if tmpl != "" {
		content.WriteString("\n")
		content.WriteString(tmpl)
		content.WriteString("\n")
	} else {
		content.WriteString("\n")
	}

	if err := os.WriteFile(filename, []byte(content.String()), 0644); err != nil {
		fatal("Failed to create issue: %v", err)
	}

	if system != "" {
		slug = strings.ToLower(system) + "/" + slug
	}

	fmt.Printf("✓ Created: %s\n", filename)
	fmt.Printf("file: %s\n", filename)
	fmt.Printf("  Slug: %s\n", slug)
	if tmpl != "" {
		fmt.Println("✓ Template checkboxes added to issue body")
	}
	fmt.Println("\nThank you!")
}

func runTransition(proj *tracker.Project, slug, to string, fields map[string]string) {
	issue, _ := findIssue(proj, slug)
	wf := proj.LoadWorkflowForIssue(issue)
	to = strings.ToLower(to)

	from, result, err := wf.ApplyTransitionToFileWithFields(issue.FilePath, to, fields)
	if err != nil {
		fatal("Failed to transition: %v", err)
	}

	issue, _ = findIssue(proj, slug)
	output := buildTransitionOutput(wf, issue, from, to, result)
	printTransitionResult(output)
}

func buildTransitionOutput(wf *tracker.WorkflowConfig, issue *tracker.Issue, from, to string, result tracker.TransitionResult) transitionOutput {
	required, optionals := wf.DefaultNextStatus(issue.Status)
	next := required
	nextOptional := false
	if next == "" && len(optionals) > 0 {
		next = optionals[0]
		nextOptional = true
		optionals = optionals[1:]
	}

	guidance := []string{}
	if prompt := strings.TrimSpace(wf.StatusPrompt(issue.Status)); prompt != "" {
		guidance = append(guidance, prompt)
	}
	guidance = append(guidance, result.InjectedPrompts...)
	guidance = append(guidance, wf.EntryPrompts(issue.Status, next)...)

	statusOptional := false
	if s := wf.GetStatus(issue.Status); s != nil {
		statusOptional = s.Optional
	}
	return transitionOutput{
		From:                 from,
		To:                   to,
		Status:               issue.Status,
		StatusOptional:       statusOptional,
		Slug:                 issue.Slug,
		File:                 issue.FilePath,
		SideEffects:          transitionSideEffects(result),
		Checklist:            collectChecklist(issue.BodyRaw),
		BodyChanged:          result.BodyChanged,
		CommentsChanged:      false,
		NextStatus:           next,
		NextStatusOptional:   nextOptional,
		OptionalNextStatuses: optionals,
		Guidance:             guidance,
	}
}

func transitionSideEffects(result tracker.TransitionResult) []string {
	var effects []string
	if result.Update.Assignee != nil {
		if *result.Update.Assignee == "" {
			effects = append(effects, "assignee cleared")
		} else {
			effects = append(effects, fmt.Sprintf("assignee set to %q", *result.Update.Assignee))
		}
	}
	if result.ClearedApproval {
		effects = append(effects, "approval consumed")
	}
	if result.BodyAppended {
		effects = append(effects, "workflow content appended to issue body")
	} else if result.BodyChanged {
		effects = append(effects, "issue body updated")
	}
	if len(result.InjectedPrompts) > 0 {
		effects = append(effects, fmt.Sprintf("%d entry guidance prompt(s) injected", len(result.InjectedPrompts)))
	}
	return effects
}

func collectChecklist(body string) []transitionChecklistItem {
	re := regexp.MustCompile(`^\s*-\s*\[([ xX])\]\s*(.*)$`)
	var items []transitionChecklistItem
	for _, line := range strings.Split(body, "\n") {
		m := re.FindStringSubmatch(line)
		if len(m) == 3 {
			items = append(items, transitionChecklistItem{
				Text:    strings.TrimSpace(m[2]),
				Checked: strings.EqualFold(m[1], "x"),
			})
		}
	}
	return items
}

func printTransitionResult(output transitionOutput) {
	if jsonOutput {
		outputJSON(output)
		return
	}

	fmt.Printf("✓ %s → %s\n", output.From, output.To)
	fmt.Printf("file: %s\n", output.File)
	statusDisp := output.Status
	if output.StatusOptional {
		statusDisp += " (optional)"
	}
	fmt.Printf("Status: %s\n", statusDisp)
	for _, effect := range output.SideEffects {
		fmt.Printf("✓ %s\n", capitalize(effect))
	}
	fmt.Println()

	printWorkflowNextStepsFromData(output.Checklist, output.Guidance, output.NextStatus, output.NextStatusOptional, output.OptionalNextStatuses, output.Slug)
}

func printWorkflowNextStepsFromData(checklist []transitionChecklistItem, guidance []string, nextStatus string, nextStatusOptional bool, optionalSidePaths []string, slug string) {
	if len(checklist) > 0 {
		checked := 0
		for _, item := range checklist {
			if item.Checked {
				checked++
			}
		}
		fmt.Printf("== Checklist (%d/%d) ==\n", checked, len(checklist))
		for _, item := range checklist {
			mark := " "
			if item.Checked {
				mark = "x"
			}
			fmt.Printf("- [%s] %s\n", mark, item.Text)
		}
		fmt.Println()
	}

	if len(guidance) > 0 {
		fmt.Println("== Guidance ==")
		for _, prompt := range guidance {
			fmt.Printf("- %s\n", prompt)
		}
		fmt.Println()
	}

	if nextStatus != "" {
		fmt.Println("== Next ==")
		suffix := ""
		if nextStatusOptional {
			suffix = "   (optional — every remaining status is optional)"
		}
		fmt.Printf("  issue-cli transition %s --to \"%s\"%s\n", slug, nextStatus, suffix)
		if len(optionalSidePaths) > 0 {
			fmt.Println()
			fmt.Println("Optional side-paths:")
			for _, opt := range optionalSidePaths {
				fmt.Printf("  issue-cli transition %s --to \"%s\"\n", slug, opt)
			}
		}
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// statusLabel returns the status name, appending " (optional)" when the workflow marks it skippable.
func statusLabel(wf *tracker.WorkflowConfig, status string) string {
	if wf == nil || status == "" {
		return status
	}
	if s := wf.GetStatus(status); s != nil && s.Optional {
		return status + " (optional)"
	}
	return status
}

func runClaim(proj *tracker.Project, slug, assignee string) {
	issue, _ := findIssue(proj, slug)

	if issue.Assignee != "" && issue.Assignee != assignee {
		// Check for --force
		for _, a := range os.Args {
			if a == "--force" {
				goto doClaim
			}
		}
		fatal("Already claimed by \"%s\"\n\nUse --force to reassign:\n  issue-cli claim %s --assignee %s --force",
			issue.Assignee, slug, assignee)
	}

doClaim:
	if err := tracker.UpdateIssueFrontmatter(issue.FilePath, tracker.IssueUpdate{Assignee: &assignee}); err != nil {
		fatal("Failed to claim: %v", err)
	}
	fmt.Printf("✓ Claimed: %s (assignee: %s)\n", issue.Slug, assignee)
	fmt.Printf("file: %s\n", issue.FilePath)
}

func runUnclaim(proj *tracker.Project, slug string) {
	issue, _ := findIssue(proj, slug)
	empty := ""
	if err := tracker.UpdateIssueFrontmatter(issue.FilePath, tracker.IssueUpdate{Assignee: &empty}); err != nil {
		fatal("Failed to unclaim: %v", err)
	}
	fmt.Printf("✓ Unclaimed: %s\n", issue.Slug)
	fmt.Printf("file: %s\n", issue.FilePath)
}

func runUpdate(proj *tracker.Project, slug string, args []string) {
	issue, _ := findIssue(proj, slug)
	update := tracker.IssueUpdate{}
	changed := false

	t := flagValue(args, "--title")
	if t != "" {
		update.Title = &t
		changed = true
	}

	b := flagValue(args, "--body")
	b = normalizeEscapedText(b)
	if b != "" {
		update.Body = &b
		changed = true
	}

	if !changed {
		fatal("update requires --title and/or --body\n\nExample:\n  issue-cli update %s --title \"new title\" --body \"new body content\"", slug)
	}

	if err := tracker.UpdateIssueFrontmatter(issue.FilePath, update); err != nil {
		fatal("Failed to update: %v", err)
	}

	fmt.Printf("✓ Updated: %s\n", issue.Slug)
	fmt.Printf("file: %s\n", issue.FilePath)
}

func runSetMeta(proj *tracker.Project, slug, key, value string, clear bool) {
	issue, _ := findIssue(proj, slug)
	if err := tracker.SetFrontmatterField(issue.FilePath, key, value, clear); err != nil {
		fatal("Failed to set frontmatter: %v", err)
	}
	if clear {
		fmt.Printf("✓ Cleared %s on %s\n", key, issue.Slug)
	} else {
		fmt.Printf("✓ Set %s = %q on %s\n", key, value, issue.Slug)
	}
	fmt.Printf("file: %s\n", issue.FilePath)
}

func runDone(proj *tracker.Project, slug string) {
	issue, _ := findIssue(proj, slug)
	wf := proj.LoadWorkflowForIssue(issue)
	issue, err := wf.MarkIssueDoneOnce(issue.FilePath, slug)
	if err != nil {
		fatal("%v", err)
	}

	fmt.Println("== Validation ==")
	fmt.Println("✓ done: all checks passed")

	fmt.Printf("\n✓ Status → %s\n", statusLabel(wf, issue.Status))
	fmt.Println("✓ Assignee cleared")
	fmt.Printf("file: %s\n", issue.FilePath)
}

func runComment(proj *tracker.Project, slug, text string) {
	issue, _ := findIssue(proj, slug)

	if err := tracker.AddComment(issue.FilePath, 0, text, "cli"); err != nil {
		fatal("Failed to add comment: %v", err)
	}
	fmt.Printf("✓ Comment added to %s\n", issue.Slug)
	fmt.Printf("file: %s\n", issue.FilePath)
}

func runChecklist(proj *tracker.Project, slug string) {
	issue, _ := findIssue(proj, slug)
	total, checked := tracker.CountCheckboxes(issue.BodyRaw)

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"total": total, "checked": checked,
		})
		return
	}

	fmt.Printf("== Checklist (%d/%d) ==\n", checked, total)
	printCheckboxes(issue.BodyRaw)
}

func runCheck(proj *tracker.Project, slug, query string) {
	issue, _ := findIssue(proj, slug)

	newBody, found, err := tracker.UpdateIssueBody(issue.FilePath, func(body string) (string, bool, error) {
		updated, ok := tracker.CheckCheckbox(body, query)
		return updated, ok, nil
	})
	if err != nil {
		fatal("Failed to update: %v", err)
	}
	if !found {
		fmt.Printf("No unchecked item matching \"%s\"\n\n", query)
		fmt.Println("Unchecked items:")
		printCheckboxes(newBody)
		os.Exit(1)
	}

	total, checked := tracker.CountCheckboxes(newBody)
	fmt.Printf("✓ Checked: \"%s\"\n", query)
	fmt.Printf("  Progress: %d/%d\n", checked, total)
	fmt.Printf("file: %s\n", issue.FilePath)
}

// listJSONIssue wraps tracker.Issue with scoring fields for `list --json`.
// Score and ScoreBreakdown are populated only when scoring is enabled in the
// workflow config; otherwise they marshal as null so consumers can treat
// missing scoring uniformly.
type listJSONIssue struct {
	*tracker.Issue
	Score          *float64                `json:"Score"`
	ScoreBreakdown *tracker.ScoreBreakdown `json:"ScoreBreakdown"`
}

func runList(proj *tracker.Project, args []string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		fatal("Cannot load issues: %v", err)
	}

	status := flagValue(args, "--status")
	system := flagValue(args, "--system")
	if system == "" {
		system = flagValue(args, "--category")
	}
	assignee := flagValue(args, "--assignee")
	version := flagValue(args, "--version")
	sortBy := flagValue(args, "--sort")

	var filtered []*tracker.Issue
	for _, issue := range issues {
		if status != "" {
			switch strings.ToLower(status) {
			case "open":
				if issue.Status == "done" {
					continue
				}
			case "closed":
				if issue.Status != "done" {
					continue
				}
			default:
				if !strings.EqualFold(issue.Status, status) {
					continue
				}
			}
		}
		if version != "" && issue.Version != version {
			continue
		}
		if system != "" && !strings.EqualFold(issue.System, system) {
			continue
		}
		if assignee != "" && issue.Assignee != assignee {
			continue
		}
		filtered = append(filtered, issue)
	}

	wf := proj.LoadWorkflow()
	scoringOn := wf != nil && wf.Scoring.Enabled

	if scoringOn {
		applySort := strings.ToLower(strings.TrimSpace(sortBy))
		if applySort == "" && strings.EqualFold(wf.Scoring.DefaultSort, "score_desc") {
			applySort = "score"
		}
		if applySort == "score" || applySort == "score_desc" {
			now := time.Now()
			scores := make(map[*tracker.Issue]float64, len(filtered))
			for _, iss := range filtered {
				if bd := tracker.ComputeScore(iss, &wf.Scoring, now); bd != nil {
					scores[iss] = bd.Total
				}
			}
			sort.SliceStable(filtered, func(i, j int) bool {
				return scores[filtered[i]] > scores[filtered[j]]
			})
		}
	}

	if jsonOutput {
		entries := make([]listJSONIssue, 0, len(filtered))
		now := time.Now()
		for _, issue := range filtered {
			entry := listJSONIssue{Issue: issue}
			if scoringOn {
				if bd := tracker.ComputeScore(issue, &wf.Scoring, now); bd != nil {
					total := bd.Total
					entry.Score = &total
					entry.ScoreBreakdown = bd
				}
			}
			entries = append(entries, entry)
		}
		outputJSON(entries)
		return
	}

	for _, issue := range filtered {
		a := ""
		if issue.Assignee != "" {
			a = " claimed by " + issue.Assignee
		}
		fmt.Printf("  [%-13s] %-45s %-10s%s\n", issue.Status, issue.Slug, issue.System, a)
	}
	fmt.Printf("\n%d issues\n", len(filtered))
}

func runSearch(proj *tracker.Project, query string) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		fatal("Cannot load issues: %v", err)
	}

	// Normalize shell-escaped regex operators (bots often pass \| instead of |)
	normalized := strings.ReplaceAll(query, `\|`, "|")
	re, err := regexp.Compile("(?i)" + normalized)
	if err != nil {
		// Fall back to literal case-insensitive search
		re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(query))
	}
	var matches []*tracker.Issue
	for _, issue := range issues {
		if re.MatchString(issue.Title) ||
			re.MatchString(issue.BodyRaw) ||
			re.MatchString(issue.Status) {
			matches = append(matches, issue)
		}
	}

	if jsonOutput {
		outputJSON(matches)
		return
	}

	if len(matches) == 0 {
		fmt.Printf("No issues matching \"%s\"\n", query)
		return
	}

	fmt.Printf("== Search: \"%s\" (%d results) ==\n", query, len(matches))
	for _, issue := range matches {
		fmt.Printf("  [%-13s] %-45s %s\n", issue.Status, issue.Slug, issue.System)
	}
}

func runAppend(proj *tracker.Project, slug, section, text string, force bool) {
	issue, _ := findIssue(proj, slug)

	_, _, err := tracker.UpdateIssueBody(issue.FilePath, func(body string) (string, bool, error) {
		if strings.TrimSpace(section) != "" {
			return tracker.AppendIssueBodyToSection(body, section, text, force)
		}
		return tracker.AppendIssueBody(body, text)
	})
	if err != nil {
		fatal("Failed to append: %v", err)
	}

	fmt.Printf("✓ Appended to %s\n", issue.Slug)
}

func runReplace(proj *tracker.Project, slug, section, text string, force bool) {
	issue, _ := findIssue(proj, slug)

	_, _, err := tracker.UpdateIssueBody(issue.FilePath, func(body string) (string, bool, error) {
		return tracker.ReplaceIssueBodySection(body, section, text, force)
	})
	if err != nil {
		fatal("Failed to replace: %v", err)
	}

	fmt.Printf("✓ Replaced section %q in %s\n", section, issue.Slug)
}

func runRetrospective(proj *tracker.Project, slug, text string) {
	issue, _ := findIssue(proj, slug)

	retroDir := filepath.Join(projectRoot(proj), "retros")
	if err := os.MkdirAll(retroDir, 0755); err != nil {
		fatal("Failed to create retrospective directory: %v", err)
	}

	body := strings.TrimSpace(text)
	report := fmt.Sprintf(`# Workflow Retrospective

Issue: %s
Title: %s
Status: %s
System: %s
Date: %s

%s
`, issue.Slug, issue.Title, issue.Status, issue.System, time.Now().Format(time.RFC3339), body)

	name := fmt.Sprintf("%s-%s.md", time.Now().UTC().Format("20060102-150405"), sanitizePathPart(issue.Slug))
	path := filepath.Join(retroDir, name)
	if err := os.WriteFile(path, []byte(report), 0644); err != nil {
		fatal("Failed to save retrospective: %v", err)
	}
	fmt.Printf("✓ Retrospective saved for %s\n", issue.Slug)
	fmt.Printf("file: %s\n", path)
}

func runReportBug(description string) {
	entry := map[string]interface{}{
		"ts":          time.Now().UTC().Format(time.RFC3339),
		"description": description,
		"tool":        "issue-cli",
	}
	if slug := os.Getenv("ISSUE_VIEWER_ISSUE_SLUG"); slug != "" {
		entry["issue_slug"] = slug
	}

	line, err := json.Marshal(entry)
	if err != nil {
		fatal("failed to marshal: %v", err)
	}
	line = append(line, '\n')

	serverRoot := os.Getenv("ISSUE_VIEWER_SERVER_PWD")
	if serverRoot == "" {
		serverRoot, _ = os.Getwd()
	}
	logDir := filepath.Join(serverRoot, "bugs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fatal("failed to create bug directory: %v", err)
	}
	logFile := filepath.Join(logDir, fmt.Sprintf("%s-issue-cli-bug.json", time.Now().UTC().Format("20060102-150405")))

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fatal("failed to write bug report: %v", err)
	}
	defer f.Close()
	f.Write(line)

	fmt.Printf("Bug reported to %s\n", logFile)
}

func runStats(proj *tracker.Project) {
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		fatal("Cannot load issues: %v", err)
	}

	byStatus := map[string]int{}
	bySystem := map[string]int{}
	byAssignee := map[string]int{}
	for _, issue := range issues {
		byStatus[issue.Status]++
		if issue.System != "" {
			bySystem[issue.System]++
		}
		if issue.Assignee != "" {
			byAssignee[issue.Assignee]++
		}
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"total":       len(issues),
			"by_status":   byStatus,
			"by_system":   bySystem,
			"by_assignee": byAssignee,
		})
		return
	}

	fmt.Printf("== Project Stats (%d issues) ==\n\n", len(issues))

	wf := proj.LoadWorkflow()
	fmt.Println("By status:")
	for _, s := range wf.GetStatusOrder() {
		if n, ok := byStatus[s]; ok {
			label := s
			if st := wf.GetStatus(s); st != nil && st.Optional {
				label = s + " (optional)"
			}
			fmt.Printf("  %-24s %d\n", label, n)
		}
	}

	fmt.Println("\nBy system:")
	for sys, n := range bySystem {
		fmt.Printf("  %-15s %d\n", sys, n)
	}

	if len(byAssignee) > 0 {
		fmt.Println("\nBy assignee:")
		for a, n := range byAssignee {
			fmt.Printf("  %-15s %d\n", a, n)
		}
	}
}

// --- data subcommand ---

func runData(proj *tracker.Project, sub string, args []string) {
	switch sub {
	case "add":
		runDataAdd(proj, args)
	case "list":
		runDataList(proj, args)
	case "set-status":
		runDataSetStatus(proj, args)
	case "set-comment":
		runDataSetComment(proj, args)
	case "remove", "rm":
		runDataRemove(proj, args)
	default:
		fatal("Unknown data subcommand: %s\n\nValid: add, list, set-status, set-comment, remove", sub)
	}
}

func parseDataID(s string) int {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || id <= 0 {
		fatal("Invalid id %q: must be a positive integer", s)
	}
	return id
}

func runDataAdd(proj *tracker.Project, args []string) {
	if len(args) < 1 {
		fatal("data add requires <slug>\n\nExample:\n  issue-cli data add my-issue --description \"finding\" --status \"open\"")
	}
	slug := args[0]
	rest := args[1:]
	desc := textFlagValue(rest, "--description")
	if desc == "" {
		fatal("data add requires --description\n\nExample:\n  issue-cli data add %s --description \"finding\" --status \"open\"", slug)
	}
	status := textFlagValue(rest, "--status")

	issue, _ := findIssue(proj, slug)
	id, err := tracker.AddEntry(issue.FilePath, desc, status)
	if err != nil {
		fatal("Failed to add entry: %v", err)
	}
	if jsonOutput {
		outputJSON(map[string]interface{}{"id": id, "slug": issue.Slug})
		return
	}
	fmt.Println(id)
	fmt.Fprintf(os.Stderr, "✓ Added entry #%d to %s\n", id, issue.Slug)
}

func runDataList(proj *tracker.Project, args []string) {
	if len(args) < 1 {
		fatal("data list requires <slug>")
	}
	slug := args[0]
	issue, _ := findIssue(proj, slug)
	store, err := tracker.LoadData(issue.FilePath)
	if err != nil {
		fatal("Failed to load data: %v", err)
	}

	if jsonOutput {
		outputJSON(store.Entries)
		return
	}

	if len(store.Entries) == 0 {
		fmt.Printf("== %s — data ==\n(no entries)\n", issue.Slug)
		return
	}
	fmt.Printf("== %s — data (%d) ==\n", issue.Slug, len(store.Entries))
	for _, e := range store.Entries {
		fmt.Printf("  #%d  [%s]  %s\n", e.ID, e.Status, e.Description)
		if e.Comment != "" {
			fmt.Printf("        comment: %s\n", e.Comment)
		}
	}
}

func runDataSetStatus(proj *tracker.Project, args []string) {
	if len(args) < 3 {
		fatal("data set-status requires <slug> <id> <status>\n\nExample:\n  issue-cli data set-status my-issue 1 resolved")
	}
	slug := args[0]
	id := parseDataID(args[1])
	status := args[2]

	issue, _ := findIssue(proj, slug)
	if err := tracker.SetEntryStatus(issue.FilePath, id, status); err != nil {
		fatal("Failed to set status: %v", err)
	}
	fmt.Printf("✓ %s entry #%d status → %s\n", issue.Slug, id, status)
}

func runDataSetComment(proj *tracker.Project, args []string) {
	if len(args) < 2 {
		fatal("data set-comment requires <slug> <id> --text \"...\"")
	}
	slug := args[0]
	id := parseDataID(args[1])
	text := textFlagValue(args[2:], "--text")
	if text == "" {
		text = textFlagValue(args[2:], "--body")
	}
	// allow trailing positional text
	if text == "" {
		var parts []string
		skip := false
		for _, a := range args[2:] {
			if skip {
				skip = false
				continue
			}
			if strings.HasPrefix(a, "--") {
				skip = true
				continue
			}
			parts = append(parts, a)
		}
		text = strings.Join(parts, " ")
	}

	issue, _ := findIssue(proj, slug)
	if err := tracker.SetEntryComment(issue.FilePath, id, text); err != nil {
		fatal("Failed to set comment: %v", err)
	}
	fmt.Printf("✓ %s entry #%d comment updated\n", issue.Slug, id)
}

func runDataRemove(proj *tracker.Project, args []string) {
	if len(args) < 2 {
		fatal("data remove requires <slug> <id>")
	}
	slug := args[0]
	id := parseDataID(args[1])
	issue, _ := findIssue(proj, slug)
	if err := tracker.RemoveEntry(issue.FilePath, id); err != nil {
		fatal("Failed to remove entry: %v", err)
	}
	fmt.Printf("✓ %s entry #%d removed\n", issue.Slug, id)
}

// --- Utility ---

func printCheckboxes(body string) {
	re := regexp.MustCompile(`^(\s*-\s*\[[ xX]\].*)$`)
	for _, line := range strings.Split(body, "\n") {
		if re.MatchString(line) {
			fmt.Println(strings.TrimSpace(line))
		}
	}
}

func sortByPriority(issues []*tracker.Issue, ranks map[string]int) {
	for i := 0; i < len(issues); i++ {
		for j := i + 1; j < len(issues); j++ {
			ri := ranks[issues[i].Priority]
			rj := ranks[issues[j].Priority]
			if rj < ri {
				issues[i], issues[j] = issues[j], issues[i]
			}
		}
	}
}
