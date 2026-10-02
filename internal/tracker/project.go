package tracker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Project struct {
	Name         string `yaml:"name"`
	Slug         string `yaml:"slug"`
	IssueDir     string `yaml:"issues"`
	DocsDir      string `yaml:"docs"`
	WorkflowFile string `yaml:"workflow"`
	Version      string `yaml:"version"`
	WorkDir      string `yaml:"workdir"`
	I3Workspace  string `yaml:"i3_workspace"`
	Terminal     string `yaml:"terminal"`
	// TmuxSession, when set, runs dispatched agents (and nvim edits) as
	// windows inside this one shared tmux session instead of one session per
	// agent. A terminal is only opened when nobody is attached to it yet.
	TmuxSession string `yaml:"tmux_session"`
	Repo        string `yaml:"repo"`
	// SupportsGitHub enables GitHub integration for this project: the /github
	// sync tab and auto-closing the remote issue when an issue is marked done.
	// Defaults to false — the GitHub tab is hidden and auto-close is skipped.
	SupportsGitHub bool `yaml:"supports_github"`
	// ImportStatus is the status assigned to issues imported from GitHub.
	// Empty falls back to the workflow's first status (e.g. "idea").
	ImportStatus string `yaml:"import_status"`
	// AgentModels maps an agent type ("claude", "codex") to the model passed
	// via --model when dispatching. Only used when AgentModelSource is "project".
	AgentModels map[string]string `yaml:"agent_models"`
	// AgentModelSource decides where the model comes from: "project" enforces
	// AgentModels; "global" (the default) passes no --model so the agent uses
	// its own global settings.
	AgentModelSource string `yaml:"agent_model_source"`
	// Telemetry opts this project out of issue-cli usage telemetry when set
	// to false. Nil (unset) means enabled.
	Telemetry *bool `yaml:"telemetry"`
}

// TelemetryEnabled reports whether issue-cli usage telemetry is on for this
// project (the ISSUE_CLI_TELEMETRY env opt-out is checked separately).
func (p *Project) TelemetryEnabled() bool {
	return p == nil || p.Telemetry == nil || *p.Telemetry
}

// TelemetryRoot is the directory whose .agent-logs/ holds this project's
// telemetry file: WorkDir when configured, otherwise the parent of the issues
// directory (the project root in the usual layout). Returns "" when neither
// can be determined.
func (p *Project) TelemetryRoot() string {
	if p == nil {
		return ""
	}
	if p.WorkDir != "" {
		return p.WorkDir
	}
	if p.IssueDir == "" {
		return ""
	}
	abs, err := filepath.Abs(p.IssueDir)
	if err != nil {
		return ""
	}
	return filepath.Dir(abs)
}

// AgentModel returns the model to enforce for agentType, or "" when the agent
// should fall back to its own global settings.
func (p *Project) AgentModel(agentType string) string {
	if p == nil || p.AgentModelSource != "project" {
		return ""
	}
	return strings.TrimSpace(p.AgentModels[agentType])
}

// LoadWorkflow loads the project's workflow config.
// An explicit project workflow file is the source of truth. If no project workflow
// file exists, it falls back to a local workflow.yaml, and finally to the built-in
// default workflow.
func (p *Project) LoadWorkflow() *WorkflowConfig {
	wf := p.loadWorkflowRaw()
	p.attachRuntime(wf)
	return wf
}

func (p *Project) loadWorkflowRaw() *WorkflowConfig {
	if p.WorkflowFile != "" {
		if custom, err := LoadWorkflow(p.WorkflowFile); err == nil && custom != nil {
			return custom
		}
	}
	if custom, err := LoadWorkflow("workflow.yaml"); err == nil && custom != nil {
		return custom
	}
	return DefaultWorkflow()
}

// attachRuntime populates IssuesRoot and a LookupIssue resolver so the
// linked_issue_in_status and command_succeeds validators have what they need
// without every caller wiring it themselves.
func (p *Project) attachRuntime(wf *WorkflowConfig) {
	if wf == nil || p == nil {
		return
	}
	wf.IssuesRoot = p.IssueDir
	if wf.LookupIssue == nil && p.IssueDir != "" {
		dir := p.IssueDir
		wf.LookupIssue = func(slug string) *Issue {
			issues, err := LoadIssues(dir)
			if err != nil {
				return nil
			}
			for _, issue := range issues {
				if issue.Slug == slug {
					return issue
				}
			}
			return nil
		}
	}
}

func (p *Project) LoadWorkflowForSystem(system string) *WorkflowConfig {
	return p.LoadWorkflow().ForSystem(system)
}

func (p *Project) LoadWorkflowForIssue(issue *Issue) *WorkflowConfig {
	if issue == nil {
		return p.LoadWorkflow()
	}
	return p.LoadWorkflowForSystem(issue.System)
}

type ProjectsConfig struct {
	Projects []Project `yaml:"projects"`
}

func LoadProjects(configPath string) ([]Project, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", configPath, err)
	}

	var cfg ProjectsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", configPath, err)
	}

	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		if p.Slug == "" {
			p.Slug = Slugify(p.Name)
		}
		switch p.AgentModelSource {
		case "", "global", "project":
		default:
			return nil, fmt.Errorf("project %q: agent_model_source must be \"project\" or \"global\", got %q", p.Name, p.AgentModelSource)
		}
	}

	return cfg.Projects, nil
}
