package tracker

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// CIConfig is the optional `ci:` block in workflow.yaml: the local CI that
// re-checks the integration branch after every move (docs/workflow.md, "Local
// CI"). Without the block nothing runs and nothing shows. It is project-wide:
// system and type overlays cannot override it.
type CIConfig struct {
	Branch  string      `yaml:"branch,omitempty" desc:"Integration branch to watch (default: the branch checked out in the main checkout)"`
	Run     []CICommand `yaml:"run" desc:"Commands run in order in .worktrees/.ci; the first failure stops the run. A plain string, or {cmd, tier, failures}"`
	Timeout string      `yaml:"timeout,omitempty" desc:"Limit for the whole run, a Go duration (default 10m)"`
	Nice    *int        `yaml:"nice,omitempty" desc:"nice level the commands run at, so CI doesn't slow agents (default 10; 0 = off)"`
	OnRed   string      `yaml:"on_red,omitempty" desc:"What a red run does besides markers, ci: comments and the lead nudge: mark (default) or file_issue (one bugfix issue per red streak)"`
}

// CICommand is one `ci.run` entry.
type CICommand struct {
	Cmd      string `yaml:"cmd" desc:"Shell command (sh -c), run in the CI worktree"`
	Tier     string `yaml:"tier,omitempty" desc:"Empty or default: runs on every move of the branch. Any other name (slow): only with issue-cli ci run --tier <name>"`
	Failures string `yaml:"failures,omitempty" desc:"Regex whose first group names a failing test in the command's output (shown in ci status and comments)"`
}

// CIDefaultTier is the tier that runs on every move of the branch.
const CIDefaultTier = "default"

const (
	CIOnRedMark      = "mark"
	CIOnRedFileIssue = "file_issue"

	ciDefaultTimeout = 10 * time.Minute
	ciDefaultNice    = 10
)

// UnmarshalYAML accepts a plain string as {cmd: <string>}.
func (c *CICommand) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		c.Cmd = node.Value
		return nil
	}
	type plain CICommand
	return node.Decode((*plain)(c))
}

// TierName is the command's tier, CIDefaultTier when unset.
func (c CICommand) TierName() string {
	if t := strings.TrimSpace(c.Tier); t != "" {
		return t
	}
	return CIDefaultTier
}

// Enabled reports whether the project opted in: a ci: block with at least one
// command.
func (c *CIConfig) Enabled() bool {
	return c != nil && len(c.Run) > 0
}

// TimeoutDuration is the run's limit; an unset or bad value is the default.
func (c *CIConfig) TimeoutDuration() time.Duration {
	if c != nil {
		if d, err := time.ParseDuration(strings.TrimSpace(c.Timeout)); err == nil && d > 0 {
			return d
		}
	}
	return ciDefaultTimeout
}

// NiceLevel is the nice level the commands run at; 0 means no nice.
func (c *CIConfig) NiceLevel() int {
	if c == nil || c.Nice == nil {
		return ciDefaultNice
	}
	return *c.Nice
}

// OnRedMode is mark or file_issue.
func (c *CIConfig) OnRedMode() string {
	if c != nil && strings.TrimSpace(c.OnRed) == CIOnRedFileIssue {
		return CIOnRedFileIssue
	}
	return CIOnRedMark
}

// Commands lists what a run of tier runs: the default commands, plus tier's
// own when tier is not the default.
func (c *CIConfig) Commands(tier string) []CICommand {
	if c == nil {
		return nil
	}
	if tier == "" {
		tier = CIDefaultTier
	}
	var out []CICommand
	for _, cmd := range c.Run {
		if t := cmd.TierName(); t == CIDefaultTier || t == tier {
			out = append(out, cmd)
		}
	}
	return out
}

// Tiers lists the tier names besides the default one, in config order.
func (c *CIConfig) Tiers() []string {
	if c == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, cmd := range c.Run {
		if t := cmd.TierName(); t != CIDefaultTier && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// Validate returns one problem per invalid entry. A missing block is valid.
func (c *CIConfig) Validate() []string {
	if c == nil {
		return nil
	}
	var problems []string
	if len(c.Run) == 0 {
		problems = append(problems, "ci.run is empty: CI stays off until it lists a command")
	}
	if t := strings.TrimSpace(c.Timeout); t != "" {
		if d, err := time.ParseDuration(t); err != nil || d <= 0 {
			problems = append(problems, fmt.Sprintf("ci.timeout %q is not a positive duration like 20m (default 10m is used)", c.Timeout))
		}
	}
	if c.Nice != nil && (*c.Nice < 0 || *c.Nice > 19) {
		problems = append(problems, fmt.Sprintf("ci.nice %d is outside 0..19", *c.Nice))
	}
	if m := strings.TrimSpace(c.OnRed); m != "" && m != CIOnRedMark && m != CIOnRedFileIssue {
		problems = append(problems, fmt.Sprintf("ci.on_red %q is not mark or file_issue (mark is used)", c.OnRed))
	}
	defaults := 0
	for i, cmd := range c.Run {
		if strings.TrimSpace(cmd.Cmd) == "" {
			problems = append(problems, fmt.Sprintf("ci.run[%d]: cmd is empty", i))
		}
		if cmd.TierName() == CIDefaultTier {
			defaults++
		}
		if cmd.Failures == "" {
			continue
		}
		re, err := regexp.Compile(cmd.Failures)
		switch {
		case err != nil:
			problems = append(problems, fmt.Sprintf("ci.run[%d].failures: %v", i, err))
		case re.NumSubexp() < 1:
			problems = append(problems, fmt.Sprintf("ci.run[%d].failures: needs a capture group for the test name, e.g. 'FAIL: (\\S+)'", i))
		}
	}
	if len(c.Run) > 0 && defaults == 0 {
		problems = append(problems, "ci.run has no default-tier command: nothing runs when the branch moves")
	}
	return problems
}

// FailureNames pulls failing test names out of a command's output with its
// failures regex: unique, in order, at most max.
func (c CICommand) FailureNames(output string, max int) []string {
	if c.Failures == "" {
		return nil
	}
	re, err := regexp.Compile(c.Failures)
	if err != nil || re.NumSubexp() < 1 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(output, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) == max {
			break
		}
	}
	return out
}

func cloneCI(c *CIConfig) *CIConfig {
	if c == nil {
		return nil
	}
	out := *c
	out.Run = append([]CICommand(nil), c.Run...)
	if c.Nice != nil {
		n := *c.Nice
		out.Nice = &n
	}
	return &out
}

// CIStatusLine returns the "Local CI: …" line agents see in their dispatch
// prompt and `start`, or "" when the project has no ci: block. internal/ci
// sets it (it reads .ci/, and imports this package); nil means no line.
var CIStatusLine func(workDir string, wf *WorkflowConfig) string
