package tracker

import (
	"fmt"
	"os"

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
	Repo         string `yaml:"repo"`
	// SupportsGitHub enables GitHub integration for this project: the /github
	// sync tab and auto-closing the remote issue when an issue is marked done.
	// Defaults to false — the GitHub tab is hidden and auto-close is skipped.
	SupportsGitHub bool `yaml:"supports_github"`
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
	}

	return cfg.Projects, nil
}
