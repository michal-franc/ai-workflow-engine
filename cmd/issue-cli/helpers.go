package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"gopkg.in/yaml.v3"
)

// errAmbiguousProject is returned by loadProjectOrErr when the configured
// projects.yaml has more than one project and the caller did not pass
// --project. The dispatcher in main.go checks for this with errors.Is so it
// can keep `help` and `process` working in multi-project setups while every
// other command fails loud with a list of the configured slugs.
var errAmbiguousProject = errors.New("ambiguous project — pass --project")

// loadProjectOrErr resolves the active project and returns the full
// configured project list alongside it.
//
// Resolution order:
//  1. Explicit --project always wins. We consult projects.yaml even when the
//     cwd has its own ./issues/ — a bot inside one project workdir must still
//     be able to query a sibling project by passing --project <slug>.
//  2. No --project + a local ./issues/ → bootstrap mode synthesizes a single
//     project from cwd. Single-project semantics; --project is unused.
//  3. No --project + no local ./issues/ → consult projects.yaml. >1 project
//     in that file returns errAmbiguousProject so the bot fails loud instead
//     of silently using projects[0].
func loadProjectOrErr(configPath, projectSlug string) (*tracker.Project, []tracker.Project, error) {
	if projectSlug != "" {
		return resolveFromConfig(configPath, projectSlug)
	}
	if info, err := os.Stat("./issues"); err == nil && info.IsDir() {
		return resolveBootstrap(configPath)
	}
	return resolveFromConfig(configPath, "")
}

// resolveBootstrap synthesizes a single-project config from cwd when ./issues
// exists. configPath is consulted only for version / workflow metadata.
func resolveBootstrap(configPath string) (*tracker.Project, []tracker.Project, error) {
	docsDir := "./docs"
	if info, err := os.Stat(docsDir); err != nil || !info.IsDir() {
		docsDir = ""
	}
	cwd, _ := os.Getwd()
	name := filepath.Base(cwd)
	proj := &tracker.Project{
		Name:     name,
		Slug:     tracker.Slugify(name),
		IssueDir: "./issues",
		DocsDir:  docsDir,
	}
	for _, f := range []string{"project.yaml", configPath} {
		data, readErr := os.ReadFile(f)
		if readErr != nil {
			continue
		}
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
	return proj, []tracker.Project{*proj}, nil
}

// resolveFromConfig reads projects.yaml and resolves projectSlug against it.
// An empty slug triggers the fail-loud / silent-fallback decision based on
// project count.
func resolveFromConfig(configPath, projectSlug string) (*tracker.Project, []tracker.Project, error) {
	projects, err := tracker.LoadProjects(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot load config %s: %w\n\nEither run from a project root with an issues/ directory, or use --config <path>", configPath, err)
	}
	if projectSlug != "" {
		for i := range projects {
			if projects[i].Slug == projectSlug {
				return &projects[i], projects, nil
			}
		}
		return nil, projects, fmt.Errorf("project %q not found in config\n%s", projectSlug, formatProjectList(projects, ""))
	}
	if len(projects) == 0 {
		return nil, projects, fmt.Errorf("no projects configured in %s", configPath)
	}
	if len(projects) > 1 {
		// Fail loud: silent fallback to projects[0] is the bug this code
		// solves. Return projects[0] as the conventional default in the
		// listing so the bot can re-run with the right --project.
		return nil, projects, fmt.Errorf("%w\n%s", errAmbiguousProject, formatProjectList(projects, projects[0].Slug))
	}
	return &projects[0], projects, nil
}

// formatProjectList renders a fixed-format enumeration of configured project
// slugs for the agent-facing error messages. defaultSlug, when non-empty, is
// marked "(default)" so the bot knows which project the silent fallback would
// have chosen historically. The leading "Available projects:" line and
// trailing hint are part of the contract — tests and bots match on them.
func formatProjectList(projects []tracker.Project, defaultSlug string) string {
	if len(projects) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available projects:\n")
	for _, p := range projects {
		if p.Slug == defaultSlug {
			fmt.Fprintf(&b, "  %s (default)\n", p.Slug)
		} else {
			fmt.Fprintf(&b, "  %s\n", p.Slug)
		}
	}
	b.WriteString("\nRetry with: issue-cli --project <slug> ...")
	return b.String()
}

// agentNameForSlug returns the assignee name used by `start` and `claim` when
// the user does not pass --assignee. The AGENT_NAME env var, if set by a
// dispatched session, takes precedence.
func agentNameForSlug(slug string) string {
	if name := os.Getenv("AGENT_NAME"); name != "" {
		return name
	}
	base := filepath.Base(slug)
	return "agent-" + base
}

// projectRoot returns the directory used as the project root for sidecar
// artifacts (retros/, bugs/). Falls back to cwd when neither WorkDir nor
// IssueDir is configured.
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

// sanitizePathPart strips characters unsuitable for a filename component.
func sanitizePathPart(s string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-", "\t", "-")
	return r.Replace(strings.TrimSpace(s))
}

// normalizeEscapedText converts escape sequences embedded in flag values
// (`\n`, `\r\n`, `\r`, `\t`) into real control characters. Used by every
// subcommand that accepts free-form body/text input.
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

// parseFieldFlags collects repeated `--field key=value` flags into a map.
// Used by `transition` to supply declarative fields[] answers inline.
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

// writeJSON emits v as indented JSON to w. Replaces the package-level
// outputJSON helper that wrote to os.Stdout unconditionally.
func writeJSON(w io.Writer, v interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// printCheckboxes writes every "- [ ]" or "- [x]" line found in body to w,
// trimmed of leading whitespace.
func printCheckboxes(w io.Writer, body string) {
	re := regexp.MustCompile(`^(\s*-\s*\[[ xX]\].*)$`)
	for _, line := range strings.Split(body, "\n") {
		if re.MatchString(line) {
			fmt.Fprintln(w, strings.TrimSpace(line))
		}
	}
}

// sortByPriority sorts issues in place using the provided rank map. Stable
// across equal priorities.
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

// statusLabel returns the status name, appending " (optional)" when the
// workflow marks it skippable.
func statusLabel(wf *tracker.WorkflowConfig, status string) string {
	if wf == nil || status == "" {
		return status
	}
	if s := wf.GetStatus(status); s != nil && s.Optional {
		return status + " (optional)"
	}
	return status
}

// capitalize returns s with its first byte uppercased. Empty string returns
// empty.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
