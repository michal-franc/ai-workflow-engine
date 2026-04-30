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
)

//go:embed CHANGELOG.md
var changelogMD string

// releasesRepo is the GitHub repo `process changes` pulls release history
// from. CHANGELOG.md is the offline fallback when the API is unreachable.
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
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		if retries := countRecentRetries(); retries >= 2 {
			fmt.Fprintf(os.Stderr, "\nhint: this same command has failed %d times in a row from this session.\n", retries+1)
			fmt.Fprintln(os.Stderr, "hint: try a different approach — run 'issue-cli process' to review the workflow,")
			fmt.Fprintln(os.Stderr, "      or 'issue-cli checklist <slug>' to see what's blocking.")
		}
		os.Exit(1)
	}
}

// run is the testable entry point. It separates global-flag parsing from
// per-command flag parsing so the registry can dispatch a Context-bound Run.
func run(args []string, in io.Reader, out, errw io.Writer) error {
	global := flag.NewFlagSet("issue-cli", flag.ContinueOnError)
	global.SetOutput(errw)
	configPath := global.String("config", "projects.yaml", "path to projects.yaml")
	projectSlug := global.String("project", "", "select project (default: first in config)")
	jsonOut := global.Bool("json", false, "output as JSON")

	rest, err := splitGlobalAndCommand(args)
	if err != nil {
		return err
	}
	if err := global.Parse(rest.global); err != nil {
		return err
	}
	if rest.command == "" {
		// Help must work even when --project is missing in a multi-project
		// setup, so we deliberately swallow the resolution error and surface
		// the project list (loaded best-effort) to the bot reading --help.
		_, allProjects, _ := loadProjectOrErr(*configPath, *projectSlug)
		return printHelp(out, allProjects, *projectSlug)
	}

	cmd := lookupCommand(rest.command)
	if cmd == nil {
		return fmt.Errorf("unknown command: %s\n\nRun: issue-cli help", rest.command)
	}

	proj, allProjects, projErr := loadProjectOrErr(*configPath, *projectSlug)
	// Help, process, and projects are project-agnostic surfaces (top-level
	// help, releases, workflow reference, project listing). Every other
	// command must surface project-resolution errors so a nil project doesn't
	// propagate into a downstream nil-pointer panic — covers ambiguous
	// multi-project, missing config file, and unknown --project slug.
	if projErr != nil && rest.command != "help" && rest.command != "process" && rest.command != "projects" {
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
		ProjectSlug: *projectSlug,
		Now:         time.Now,
	}

	err = cmd.Run(ctx, rest.args)
	if errors.Is(err, flag.ErrHelp) {
		// FlagSet already wrote usage to ctx.Stderr; suppress the duplicate
		// error message in main()'s error path.
		return nil
	}
	return err
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
		case "--json":
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
