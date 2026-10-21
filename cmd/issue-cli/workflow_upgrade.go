package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

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

// parseTemplateStamp splits "development@v0.36.0" into name and version.
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
	Kind       string `json:"kind"` // "new", or "update" for a newer version of an installed feature
	Version    string `json:"version"`
	Installed  string `json:"installed_version,omitempty"`
	Category   string `json:"category"`
	InTemplate bool   `json:"in_template"`
	Declined   bool   `json:"declined,omitempty"`
	Title      string `json:"title"`
	What       string `json:"what"`
	Why        string `json:"why"`
	Where      string `json:"where"`
	Snippet    string `json:"snippet"`
	Question   string `json:"question"`
}

type upgradePlan struct {
	Status       string `json:"status"`
	WorkflowFile string `json:"workflow_file"`
	Template     string `json:"template"`
	Current      string `json:"current"`
	Latest       string `json:"latest"`
	// RecordSource is "record" when the file has a features: record (it
	// decides what is installed) or "detection" when the catalog probes do.
	RecordSource string            `json:"record_source"`
	Record       *featureRecordOut `json:"record,omitempty"`
	All          bool              `json:"all"`
	Proposals    []upgradeProposal `json:"proposals"`
	Present      []string          `json:"already_present"`
	Declined     []string          `json:"declined"`
	Unrecorded   []string          `json:"detected_unrecorded"`
	BackupPath   string            `json:"backup_path,omitempty"`
	Steps        []string          `json:"steps"`
	Message      string            `json:"message"`
}

type featureRecordOut struct {
	Installed []string `json:"installed"`
	Declined  []string `json:"declined"`
}

const (
	proposalNew    = "new"
	proposalUpdate = "update"
)

// buildUpgradePlan compares a workflow file with the bundled templates and
// the feature catalog and lists the features to propose. It reads only;
// templateFlag picks the template for an unstamped file, and all lists every
// catalog feature the file lacks (ignoring the stamp, propose: and
// templates:), declined ones included.
//
// What counts as installed comes from the features: record when the file has
// one, otherwise from the catalog's detection probes.
func buildUpgradePlan(path string, raw []byte, templateFlag string, all bool) (*upgradePlan, error) {
	plan := &upgradePlan{WorkflowFile: path, All: all, Proposals: []upgradeProposal{}, Present: []string{}, Declined: []string{}, Unrecorded: []string{}, Steps: []string{}}
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
			plan.Message = fmt.Sprintf("`template: %s` is not <name>@vX.Y.Z (e.g. %s@%s) or custom. Fix the stamp, or remove it and pass --template.", stamp, defaultUpgradeTemplate, "v0.36.0")
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
	upToDate := plan.Status == "" && compareVersions(plan.Current, latest) >= 0
	if plan.Status == "" {
		plan.Status = upgradeBehind
		if upToDate {
			plan.Status = upgradeUpToDate
		}
	}

	entries, err := loadFeatureCatalog()
	if err != nil {
		return nil, err
	}
	inTemplate, err := bundledTemplateFeatures(plan.Template)
	if err != nil {
		return nil, err
	}
	rec, hasRecord := readFeatureRecord(raw)
	installed, declined := recordIndex(rec)
	plan.RecordSource = "detection"
	if hasRecord {
		plan.RecordSource = "record"
		plan.Record = &featureRecordOut{Installed: append([]string{}, rec.Installed...), Declined: append([]string{}, rec.Declined...)}
	}
	doc := parseWorkflowDoc(raw)

	for _, e := range entries {
		// newer: the stamp predates the entry, so the project hasn't been
		// offered this version yet.
		newer := plan.Current == "" || compareVersions(e.Version, plan.Current) > 0
		offer := all || (newer && e.Propose && e.appliesTo(plan.Template))
		p := upgradeProposal{
			ID:         e.ID,
			Kind:       proposalNew,
			Version:    e.Version,
			Category:   e.Category,
			InTemplate: inTemplate[e.ID],
			Title:      e.Title,
			What:       strings.TrimSpace(e.What),
			Why:        strings.TrimSpace(e.Why),
			Where:      e.Where,
			Snippet:    strings.TrimRight(e.Snippet, "\n"),
			Question:   e.Question,
		}
		if hasRecord {
			if v, ok := installed[e.ID]; ok {
				plan.Present = append(plan.Present, e.ID)
				if (all || newer) && v != "" && compareVersions(e.Version, v) > 0 {
					p.Kind, p.Installed = proposalUpdate, v
					plan.Proposals = append(plan.Proposals, p)
				}
				continue
			}
			if declined[e.ID] {
				plan.Declined = append(plan.Declined, e.ID)
				if all {
					p.Declined = true
					plan.Proposals = append(plan.Proposals, p)
				}
				continue
			}
			if detectFeature(e, doc).Matches > 0 {
				// In the file but not in the record: scan, don't propose.
				plan.Unrecorded = append(plan.Unrecorded, e.ID)
				continue
			}
		} else if detectFeature(e, doc).Matches > 0 {
			plan.Present = append(plan.Present, e.ID)
			continue
		}
		if offer {
			plan.Proposals = append(plan.Proposals, p)
		}
	}
	for i := range plan.Proposals {
		plan.Proposals[i].Number = i + 1
	}

	stampLine := fmt.Sprintf("template: %s@%s", plan.Template, latest)
	scanStep := "Record the result: `issue-cli workflow scan --installed <picked ids> --declined <declined ids>` (comma-separated). It writes the features: record, and upgrade stops proposing declined items; the human can re-open one later with `issue-cli workflow scan --forget <id>`."
	if len(plan.Proposals) == 0 {
		switch {
		case upToDate && hasRecord:
			plan.Message = fmt.Sprintf("workflow.yaml is up to date with the bundled %s template (%s).", plan.Template, latest)
		case upToDate:
			plan.Message = fmt.Sprintf("workflow.yaml is up to date with the bundled %s template (%s). It has no features: record yet; `issue-cli workflow scan` writes one.", plan.Template, latest)
			plan.Steps = append(plan.Steps, "Record the features the file has: `issue-cli workflow scan` (preview with --dry-run).")
		default:
			plan.Message = fmt.Sprintf("No features to propose: workflow.yaml already has everything the bundled %s template (%s) offers. Add `%s` at the top of workflow.yaml to record that.", plan.Template, latest, stampLine)
			plan.Steps = append(plan.Steps, fmt.Sprintf("Set the stamp: add or update `%s` at the top of %s.", stampLine, path))
			if !hasRecord {
				plan.Steps = append(plan.Steps, "Record the features the file has: `issue-cli workflow scan` (preview with --dry-run).")
			}
		}
		if len(plan.Unrecorded) > 0 {
			plan.Steps = append(plan.Steps, "Some features are in the file but not in its features: record; run `issue-cli workflow scan` to record them.")
		}
		return plan, nil
	}

	plan.BackupPath = nextWorkflowBackupPath(path)
	plan.Message = fmt.Sprintf("%d feature(s) to propose. Only act on them if the operator asked you to upgrade workflow.yaml; otherwise tell them an upgrade is available and stop.", len(plan.Proposals))
	plan.Steps = []string{
		"Continue only if the operator explicitly asked for a workflow upgrade. Otherwise tell them it is available (`issue-cli workflow upgrade` lists it) and stop.",
		"Propose the items one at a time: say what it is and why it helps, ask its question, and note whether the human picks or declines it. Don't apply anything they didn't pick.",
		fmt.Sprintf("Back up the current file: cp %s %s", path, plan.BackupPath),
		fmt.Sprintf("Write the new %s: the old content plus each picked snippet, adapted to the project's own status names and sections. Keep every existing customisation; ask the human when a snippet conflicts with one.", filepath.Base(path)),
		fmt.Sprintf("Set the stamp at the top of the file: `%s`.", stampLine),
		scanStep,
		"Validate: run `issue-cli process workflow` and fix anything under == Warnings (workflow.yaml) ==.",
		"Existing issues keep the checklist sections they already have; new sections appear as issues pass the changed transitions.",
	}
	return plan, nil
}

// bundledTemplateFeatures is the set of feature ids in a bundled template's
// own features: record.
func bundledTemplateFeatures(name string) (map[string]bool, error) {
	data, err := readWorkflowTemplate(name)
	if err != nil {
		return nil, err
	}
	rec, _ := readFeatureRecord(data)
	installed, _ := recordIndex(rec)
	set := make(map[string]bool, len(installed))
	for id := range installed {
		set[id] = true
	}
	return set, nil
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
	plan, err := buildUpgradePlan(path, raw, "", false)
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
