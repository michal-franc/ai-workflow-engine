package tracker

import (
	"fmt"
	"path"
	"strings"
)

// PrivacyConfig is the optional `privacy:` block in workflow.yaml: where the
// project's work files live (a private tracker repo, or the code repo) and
// whether development is private with public release snapshots. It records
// the owner's choice; without it the project has not decided and the pages
// say "Not set up". It is project-wide: overlays cannot override it.
type PrivacyConfig struct {
	WorkFiles    string   `yaml:"work_files,omitempty" desc:"Where the work files live: tracker (a private tracker repo whose git folder sits outside the checkout) or in_repo (the code repo, by choice)"`
	WorkPaths    []string `yaml:"work_paths,omitempty" desc:"Work paths relative to the project root (default issues, sessions, retros, bugs, .health/snapshots)"`
	Code         string   `yaml:"code,omitempty" desc:"snapshots (develop in a private dev repo, publish one commit per release) or public (develop in the public repo)"`
	PublicRemote string   `yaml:"public_remote,omitempty" desc:"Remote that receives release snapshots (default public); code: snapshots only"`
	PublicBranch string   `yaml:"public_branch,omitempty" desc:"Branch on the public remote that receives release snapshots (default main)"`
	Decided      string   `yaml:"decided,omitempty" desc:"When and in whose words the owner made this choice, e.g. 2026-10-24, owner: \"…\""`
}

// Privacy choices.
const (
	WorkFilesTracker = "tracker"
	WorkFilesInRepo  = "in_repo"
	CodeSnapshots    = "snapshots"
	CodePublic       = "public"
)

// DefaultWorkPaths are the work paths when privacy.work_paths is not set.
var DefaultWorkPaths = []string{"issues", "sessions", "retros", "bugs", ".health/snapshots"}

// Paths is the work paths, cleaned, or the defaults.
func (p *PrivacyConfig) Paths() []string {
	if p == nil || len(p.WorkPaths) == 0 {
		return append([]string(nil), DefaultWorkPaths...)
	}
	var out []string
	for _, wp := range p.WorkPaths {
		if c := cleanWorkPath(wp); c != "" && !strings.HasPrefix(c, "..") {
			out = append(out, c)
		}
	}
	return out
}

func cleanWorkPath(wp string) string {
	wp = strings.TrimSpace(wp)
	if wp == "" {
		return ""
	}
	c := path.Clean(strings.TrimSuffix(wp, "/"))
	if c == "." || c == "/" {
		return ""
	}
	return strings.TrimPrefix(c, "/")
}

// Tracker reports whether the work files live in a private tracker repo.
func (p *PrivacyConfig) Tracker() bool { return p != nil && p.WorkFiles == WorkFilesTracker }

// Snapshots reports whether the project develops privately and publishes
// one commit per release.
func (p *PrivacyConfig) Snapshots() bool { return p != nil && p.Code == CodeSnapshots }

// Remote is the public remote's name (default public).
func (p *PrivacyConfig) Remote() string {
	if p == nil || strings.TrimSpace(p.PublicRemote) == "" {
		return "public"
	}
	return strings.TrimSpace(p.PublicRemote)
}

// Branch is the public branch (default main).
func (p *PrivacyConfig) Branch() string {
	if p == nil || strings.TrimSpace(p.PublicBranch) == "" {
		return "main"
	}
	return strings.TrimSpace(p.PublicBranch)
}

// Validate reports problems in the privacy: block for process workflow.
func (p *PrivacyConfig) Validate() []string {
	if p == nil {
		return nil
	}
	var problems []string
	if w := p.WorkFiles; w != "" && w != WorkFilesTracker && w != WorkFilesInRepo {
		problems = append(problems, fmt.Sprintf("privacy.work_files %q is not tracker or in_repo (treated as not decided)", w))
	}
	if c := p.Code; c != "" && c != CodeSnapshots && c != CodePublic {
		problems = append(problems, fmt.Sprintf("privacy.code %q is not snapshots or public (treated as not decided)", c))
	}
	for i, wp := range p.WorkPaths {
		if cleanWorkPath(wp) == "" {
			problems = append(problems, fmt.Sprintf("privacy.work_paths[%d] %q is empty or the project root (ignored)", i, wp))
		} else if strings.HasPrefix(path.Clean(wp), "..") {
			problems = append(problems, fmt.Sprintf("privacy.work_paths[%d] %q is outside the project", i, wp))
		}
	}
	if !p.Snapshots() && (p.PublicRemote != "" || p.PublicBranch != "") {
		problems = append(problems, "privacy.public_remote and public_branch only apply with code: snapshots (ignored)")
	}
	if (p.WorkFiles != "" || p.Code != "") && strings.TrimSpace(p.Decided) == "" {
		problems = append(problems, "privacy.decided is empty: record when and in whose words the owner chose (issue-cli privacy check)")
	}
	return problems
}
