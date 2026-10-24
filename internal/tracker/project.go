package tracker

import (
	"fmt"
	"net/url"
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
	// LeadTerminal decides what opens when the project lead starts or is
	// opened; workers keep Terminal. Unset or "none" is headless,
	// "tmux-window" adds a tab to LeadWindowSession, anything else is a
	// shell command like Terminal.
	LeadTerminal string `yaml:"lead_terminal"`
	// LeadWindowSession is the tmux session (usually the human's own) that
	// lead_terminal: tmux-window adds the lead's tab to.
	LeadWindowSession string `yaml:"lead_window_session"`
	// LeadWindow pins that tab to a window index; nil takes the next free one.
	LeadWindow *int   `yaml:"lead_window"`
	Repo       string `yaml:"repo"`
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

	// TrackerGitDir is the git folder of the project's private tracker repo
	// (privacy.work_files: tracker). Empty means the default,
	// <checkout's parent>/.trackers/<slug>.git.
	TrackerGitDir string `yaml:"tracker_git_dir"`

	// ViewerURL is projects.yaml's top-level viewer_url, copied onto every
	// project; ConfigPath is the absolute path of the projects.yaml it came
	// from. Neither is a per-project key. See viewerstate.BaseURL.
	ViewerURL  string `yaml:"-"`
	ConfigPath string `yaml:"-"`
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
	wf.RepoRoot = p.TelemetryRoot()
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
	if wf.ChildrenOf == nil && p.IssueDir != "" {
		dir := p.IssueDir
		wf.ChildrenOf = func(slug string) []*Issue {
			issues, err := LoadIssues(dir)
			if err != nil {
				return nil
			}
			return EpicChildren(issues, slug)
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
	return p.LoadWorkflow().ForIssue(issue)
}

// ProjectsConfig is projects.yaml. The top-level keys are machine-wide
// defaults: each fills in the project setting of the same kind wherever a
// project leaves it unset.
type ProjectsConfig struct {
	// Terminal is the terminal: every project without its own gets.
	Terminal string `yaml:"terminal"`
	// LeadTerminal and LeadWindowSession are the lead_terminal: and
	// lead_window_session: every project without its own gets.
	LeadTerminal      string `yaml:"lead_terminal"`
	LeadWindowSession string `yaml:"lead_window_session"`
	// TmuxSessionPerProject gives every project without its own tmux_session
	// one named after its slug, so each project's lead and workers are
	// windows of one session.
	TmuxSessionPerProject bool `yaml:"tmux_session_per_project"`
	// EngineProject is the slug of the project issue-cli bug reports go to
	// (report-bug). Unset, the project with the engine's repo is used.
	EngineProject string `yaml:"engine_project"`
	// ViewerURL is the address the viewer is reached at, used for every
	// link the viewer and issue-cli print (approval hints, Timeline, ...)
	// and for the calls issue-cli makes to it. Unset, issue-cli uses the
	// running viewer's own address (viewer.json), then http://localhost:8080.
	ViewerURL string `yaml:"viewer_url"`
	// AllowedHosts are the Host names the viewer answers to besides
	// loopback (localhost, 127.0.0.1, [::1], *.localhost): a LAN or VPN
	// name it is reached by. Any other Host gets 421 (DNS rebinding).
	AllowedHosts []string  `yaml:"allowed_hosts"`
	Projects     []Project `yaml:"projects"`
}

func LoadProjects(configPath string) ([]Project, error) {
	cfg, err := LoadProjectsConfig(configPath)
	if err != nil {
		return nil, err
	}
	return cfg.Projects, nil
}

// LoadProjectsConfig reads projects.yaml with its machine-wide keys.
func LoadProjectsConfig(configPath string) (*ProjectsConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", configPath, err)
	}

	var cfg ProjectsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", configPath, err)
	}

	viewerURL, err := normalizeViewerURL(cfg.ViewerURL)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", configPath, err)
	}
	cfg.ViewerURL = viewerURL
	absConfig := configPath
	if abs, err := filepath.Abs(configPath); err == nil {
		absConfig = abs
	}

	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		p.ViewerURL, p.ConfigPath = cfg.ViewerURL, absConfig
		if p.Slug == "" {
			p.Slug = Slugify(p.Name)
		}
		if strings.TrimSpace(p.Terminal) == "" {
			p.Terminal = cfg.Terminal
		}
		if strings.TrimSpace(p.LeadTerminal) == "" {
			p.LeadTerminal = cfg.LeadTerminal
		}
		if strings.TrimSpace(p.LeadWindowSession) == "" {
			p.LeadWindowSession = cfg.LeadWindowSession
		}
		if cfg.TmuxSessionPerProject && strings.TrimSpace(p.TmuxSession) == "" {
			p.TmuxSession = sessionSafe(p.Slug)
		}
		switch p.AgentModelSource {
		case "", "global", "project":
		default:
			return nil, fmt.Errorf("project %q: agent_model_source must be \"project\" or \"global\", got %q", p.Name, p.AgentModelSource)
		}
	}

	return &cfg, nil
}

// normalizeViewerURL checks viewer_url is an http(s) URL with a host and
// trims its trailing slash; a path (a reverse-proxy prefix) is kept.
func normalizeViewerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("viewer_url must be an http:// or https:// URL with a host, got %q", raw)
	}
	return strings.TrimRight(raw, "/"), nil
}
