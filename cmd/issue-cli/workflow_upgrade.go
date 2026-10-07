package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// workflowChangelogYAML lists the template features `workflow upgrade`
// proposes. It sits outside templates/workflow/ so the *.yaml template glob
// doesn't pick it up as a template.
//
//go:embed templates/workflow-changelog.yaml
var workflowChangelogYAML []byte

// customTemplateStamp marks a hand-built workflow.yaml: no hints, no proposals.
const customTemplateStamp = "custom"

// defaultUpgradeTemplate is the template an unstamped workflow.yaml is
// compared with when --template is not given.
const defaultUpgradeTemplate = "development"

// Upgrade plan statuses.
const (
	upgradeUpToDate        = "up_to_date"
	upgradeBehind          = "behind"
	upgradeUnstamped       = "unstamped"
	upgradeCustom          = "custom"
	upgradeUnknownTemplate = "unknown_template"
)

type changelogEntry struct {
	ID         string          `yaml:"id"`
	Version    string          `yaml:"version"`
	Templates  []string        `yaml:"templates"`
	InTemplate bool            `yaml:"in_template"`
	Title      string          `yaml:"title"`
	What       string          `yaml:"what"`
	Why        string          `yaml:"why"`
	Where      string          `yaml:"where"`
	Snippet    string          `yaml:"snippet"`
	Question   string          `yaml:"question"`
	Detect     changelogDetect `yaml:"detect"`
}

// changelogDetect is a data-only probe: the feature counts as present when any
// listed top-level key exists or any listed string appears in the raw file.
type changelogDetect struct {
	HasKey   []string `yaml:"has_key"`
	Contains []string `yaml:"contains"`
}

func (e changelogEntry) appliesTo(template string) bool {
	for _, t := range e.Templates {
		if t == template {
			return true
		}
	}
	return false
}

// presentIn reports whether the workflow file already has the feature.
func (e changelogEntry) presentIn(raw []byte, topKeys map[string]bool) bool {
	for _, k := range e.Detect.HasKey {
		if topKeys[k] {
			return true
		}
	}
	for _, s := range e.Detect.Contains {
		if strings.Contains(string(raw), s) {
			return true
		}
	}
	return false
}

func loadWorkflowChangelog() ([]changelogEntry, error) {
	var doc struct {
		Entries []changelogEntry `yaml:"entries"`
	}
	if err := yaml.Unmarshal(workflowChangelogYAML, &doc); err != nil {
		return nil, fmt.Errorf("parsing bundled workflow changelog: %w", err)
	}
	return doc.Entries, nil
}

// topLevelKeys returns the keys of the workflow.yaml mapping. A file that
// doesn't parse as a mapping yields an empty set rather than an error, so the
// hint never breaks a command over a broken workflow.yaml.
func topLevelKeys(raw []byte) map[string]bool {
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return map[string]bool{}
	}
	keys := make(map[string]bool, len(m))
	for k := range m {
		keys[k] = true
	}
	return keys
}

// readTemplateStamp returns the raw `template:` value of a workflow file.
func readTemplateStamp(raw []byte) string {
	var doc struct {
		Template string `yaml:"template"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Template)
}

// parseTemplateStamp splits "development@v0.34.0" into name and version.
func parseTemplateStamp(stamp string) (name, version string, ok bool) {
	name, version, found := strings.Cut(stamp, "@")
	name, version = strings.TrimSpace(name), strings.TrimSpace(version)
	if !found || name == "" || parseVersion(version) == nil {
		return "", "", false
	}
	return name, version, true
}

// bundledTemplateVersion is the version in the bundled template's own stamp.
func bundledTemplateVersion(name string) (string, error) {
	data, err := readWorkflowTemplate(name)
	if err != nil {
		return "", err
	}
	stampName, version, ok := parseTemplateStamp(readTemplateStamp(data))
	if !ok || stampName != name {
		return "", fmt.Errorf("bundled template %q has no valid `template: %s@vX.Y.Z` stamp — this is a build issue", name, name)
	}
	return version, nil
}

// parseVersion parses vX.Y.Z (the leading v is optional) into its numbers.
func parseVersion(v string) []int {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		nums[i] = n
	}
	return nums
}

// compareVersions returns -1, 0 or 1. Unparseable versions sort first.
func compareVersions(a, b string) int {
	va, vb := parseVersion(a), parseVersion(b)
	switch {
	case va == nil && vb == nil:
		return 0
	case va == nil:
		return -1
	case vb == nil:
		return 1
	}
	for i := 0; i < 3; i++ {
		if va[i] != vb[i] {
			if va[i] < vb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

type upgradeProposal struct {
	Number     int    `json:"number"`
	ID         string `json:"id"`
	Version    string `json:"version"`
	InTemplate bool   `json:"in_template"`
	Title      string `json:"title"`
	What       string `json:"what"`
	Why        string `json:"why"`
	Where      string `json:"where"`
	Snippet    string `json:"snippet"`
	Question   string `json:"question"`
}

type upgradePlan struct {
	Status       string            `json:"status"`
	WorkflowFile string            `json:"workflow_file"`
	Template     string            `json:"template"`
	Current      string            `json:"current"`
	Latest       string            `json:"latest"`
	Proposals    []upgradeProposal `json:"proposals"`
	Present      []string          `json:"already_present"`
	BackupPath   string            `json:"backup_path,omitempty"`
	Steps        []string          `json:"steps"`
	Message      string            `json:"message"`
}

// buildUpgradePlan compares a workflow file with the bundled templates and
// lists the features to propose. It reads only; templateFlag picks the
// template for an unstamped file.
func buildUpgradePlan(path string, raw []byte, templateFlag string) (*upgradePlan, error) {
	plan := &upgradePlan{WorkflowFile: path, Proposals: []upgradeProposal{}, Present: []string{}, Steps: []string{}}
	stamp := readTemplateStamp(raw)
	templateFlag = strings.TrimSpace(templateFlag)

	if stamp == customTemplateStamp {
		plan.Status = upgradeCustom
		plan.Template = customTemplateStamp
		plan.Message = "workflow.yaml is marked `template: custom`; there is nothing to upgrade. Remove the line or set `template: <name>@<version>` to get proposals."
		return plan, nil
	}

	if stamp == "" {
		plan.Status = upgradeUnstamped
		plan.Template = templateFlag
		if plan.Template == "" {
			plan.Template = defaultUpgradeTemplate
		}
	} else {
		name, version, ok := parseTemplateStamp(stamp)
		if !ok {
			plan.Status = upgradeUnknownTemplate
			plan.Template = stamp
			plan.Message = fmt.Sprintf("`template: %s` is not <name>@vX.Y.Z (e.g. %s@%s) or custom. Fix the stamp, or remove it and pass --template.", stamp, defaultUpgradeTemplate, "v0.34.0")
			return plan, nil
		}
		if templateFlag != "" && templateFlag != name {
			return nil, fmt.Errorf("workflow.yaml is stamped %s; --template only applies to an unstamped workflow.yaml", stamp)
		}
		plan.Template, plan.Current = name, version
	}

	latest, err := bundledTemplateVersion(plan.Template)
	if err != nil {
		if !isBundledTemplate(plan.Template) {
			plan.Status = upgradeUnknownTemplate
			plan.Message = fmt.Sprintf("no bundled template named %q (bundled: %s).", plan.Template, strings.Join(availableWorkflowTemplates(), ", "))
			return plan, nil
		}
		return nil, err
	}
	plan.Latest = latest

	if plan.Status == "" {
		if compareVersions(plan.Current, latest) >= 0 {
			plan.Status = upgradeUpToDate
			plan.Message = fmt.Sprintf("workflow.yaml is up to date with the bundled %s template (%s).", plan.Template, latest)
			return plan, nil
		}
		plan.Status = upgradeBehind
	}

	entries, err := loadWorkflowChangelog()
	if err != nil {
		return nil, err
	}
	keys := topLevelKeys(raw)
	for _, e := range entries {
		if !e.appliesTo(plan.Template) {
			continue
		}
		if plan.Current != "" && compareVersions(e.Version, plan.Current) <= 0 {
			continue
		}
		if e.presentIn(raw, keys) {
			plan.Present = append(plan.Present, e.ID)
			continue
		}
		plan.Proposals = append(plan.Proposals, upgradeProposal{
			Number:     len(plan.Proposals) + 1,
			ID:         e.ID,
			Version:    e.Version,
			InTemplate: e.InTemplate,
			Title:      e.Title,
			What:       strings.TrimSpace(e.What),
			Why:        strings.TrimSpace(e.Why),
			Where:      e.Where,
			Snippet:    strings.TrimRight(e.Snippet, "\n"),
			Question:   e.Question,
		})
	}

	stampLine := fmt.Sprintf("template: %s@%s", plan.Template, latest)
	if len(plan.Proposals) == 0 {
		plan.Message = fmt.Sprintf("No features to propose: workflow.yaml already has everything the bundled %s template (%s) offers. Add `%s` at the top of workflow.yaml to record that.", plan.Template, latest, stampLine)
		plan.Steps = append(plan.Steps, fmt.Sprintf("Set the stamp: add or update `%s` at the top of %s.", stampLine, path))
		return plan, nil
	}

	plan.BackupPath = nextWorkflowBackupPath(path)
	plan.Message = fmt.Sprintf("%d feature(s) to propose. Only act on them if the operator asked you to upgrade workflow.yaml; otherwise tell them an upgrade is available and stop.", len(plan.Proposals))
	plan.Steps = []string{
		"Continue only if the operator explicitly asked for a workflow upgrade. Otherwise tell them it is available (`issue-cli workflow upgrade` lists it) and stop.",
		"Propose the items one at a time: say what it is and why it helps, ask its question, and note whether the human picks or declines it. Don't apply anything they didn't pick.",
		fmt.Sprintf("Back up the current file: cp %s %s", path, plan.BackupPath),
		fmt.Sprintf("Write the new %s: the old content plus each picked snippet, adapted to the project's own status names and sections. Keep every existing customisation; ask the human when a snippet conflicts with one.", filepath.Base(path)),
		fmt.Sprintf("Set the stamp at the top of the file: `%s`. Declined items are then no longer proposed.", stampLine),
		"Validate: run `issue-cli process workflow` and fix anything under == Warnings (workflow.yaml) ==.",
		"Existing issues keep the checklist sections they already have; new sections appear as issues pass the changed transitions.",
	}
	return plan, nil
}

func isBundledTemplate(name string) bool {
	for _, t := range availableWorkflowTemplates() {
		if t == name {
			return true
		}
	}
	return false
}

// nextWorkflowBackupPath returns the first workflow.oldN.yaml next to path
// that doesn't exist yet.
func nextWorkflowBackupPath(path string) string {
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for n := 1; ; n++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s.old%d.yaml", base, n))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

// projectWorkflowFile mirrors Project.LoadWorkflow's file choice: the
// project's configured file, then ./workflow.yaml. "" means the project runs
// on the built-in default workflow.
func projectWorkflowFile(p *tracker.Project) string {
	if p != nil && p.WorkflowFile != "" && isRegularFile(p.WorkflowFile) {
		return p.WorkflowFile
	}
	if isRegularFile("workflow.yaml") {
		return "workflow.yaml"
	}
	return ""
}

// workflowUpgradeHint is the one-line notice `start` and `process workflow`
// print when the project's workflow.yaml has features to propose. It returns
// "" when there is nothing to say or anything goes wrong — a hint must never
// break the command that prints it.
func workflowUpgradeHint(p *tracker.Project) string {
	path := projectWorkflowFile(p)
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	plan, err := buildUpgradePlan(path, raw, "")
	if err != nil || len(plan.Proposals) == 0 {
		return ""
	}
	var where string
	if plan.Status == upgradeUnstamped {
		where = fmt.Sprintf("workflow.yaml has no template: stamp (compared with the bundled %s@%s)", plan.Template, plan.Latest)
	} else {
		where = fmt.Sprintf("workflow.yaml is %s@%s (bundled: %s@%s)", plan.Template, plan.Current, plan.Template, plan.Latest)
	}
	return fmt.Sprintf("ℹ %s: %d upgrade proposal(s). Tell the operator; run `issue-cli workflow upgrade` only if they ask you to upgrade.", where, len(plan.Proposals))
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// printWorkflowUpgradeHint prints the upgrade hint as its own paragraph, or
// nothing.
func printWorkflowUpgradeHint(ctx *Context) {
	if hint := workflowUpgradeHint(ctx.Project); hint != "" {
		fmt.Fprintln(ctx.Stdout)
		fmt.Fprintln(ctx.Stdout, hint)
	}
}
