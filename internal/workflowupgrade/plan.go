package workflowupgrade

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// CustomStamp marks a hand-built workflow.yaml: no hints, no proposals.
const CustomStamp = "custom"

// DefaultTemplate is the template an unstamped workflow.yaml is
// compared with when --template is not given.
const DefaultTemplate = "development"

// Upgrade plan statuses.
const (
	StatusUpToDate        = "up_to_date"
	StatusBehind          = "behind"
	StatusUnstamped       = "unstamped"
	StatusCustom          = "custom"
	StatusUnknownTemplate = "unknown_template"
	// StatusUndecided: the stamp is current but the record leaves proposals
	// undecided; the owner picks or declines them.
	StatusUndecided = "undecided"
	// StatusStaleRecord: the stamp is current but the file has features its
	// record doesn't list; `issue-cli workflow scan` records them.
	StatusStaleRecord = "stale_record"
)

// ReadStamp returns the raw `template:` value of a workflow file.
func ReadStamp(raw []byte) string {
	var doc struct {
		Template string `yaml:"template"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Template)
}

// ParseStamp splits "development@v0.36.0" into name and version.
func ParseStamp(stamp string) (name, version string, ok bool) {
	name, version, found := strings.Cut(stamp, "@")
	name, version = strings.TrimSpace(name), strings.TrimSpace(version)
	if !found || name == "" || ParseVersion(version) == nil {
		return "", "", false
	}
	return name, version, true
}

// BundledVersion is the version in the bundled template's own stamp.
func BundledVersion(name string) (string, error) {
	data, err := ReadTemplate(name)
	if err != nil {
		return "", err
	}
	stampName, version, ok := ParseStamp(ReadStamp(data))
	if !ok || stampName != name {
		return "", fmt.Errorf("bundled template %q has no valid `template: %s@vX.Y.Z` stamp — this is a build issue", name, name)
	}
	return version, nil
}

// ParseVersion parses vX.Y.Z (the leading v is optional) into its numbers.
func ParseVersion(v string) []int {
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

// CompareVersions returns -1, 0 or 1. Unparseable versions sort first.
func CompareVersions(a, b string) int {
	va, vb := ParseVersion(a), ParseVersion(b)
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

type Proposal struct {
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
	// Setup is the entry's steps beyond the snippet (owner or agent).
	Setup []SetupStep `json:"setup,omitempty"`
	// Needs lists required entries the project lacks; only --all lists
	// such a proposal.
	Needs []string `json:"needs,omitempty"`
}

type Plan struct {
	Status       string `json:"status"`
	WorkflowFile string `json:"workflow_file"`
	Template     string `json:"template"`
	Current      string `json:"current"`
	Latest       string `json:"latest"`
	// RecordSource is "record" when the file has a features: record (it
	// decides what is installed) or "detection" when the catalog probes do.
	RecordSource string     `json:"record_source"`
	Record       *RecordOut `json:"record,omitempty"`
	All          bool       `json:"all"`
	Proposals    []Proposal `json:"proposals"`
	Present      []string   `json:"already_present"`
	Declined     []string   `json:"declined"`
	Unrecorded   []string   `json:"detected_unrecorded"`
	// Undecided lists the proposals made without --all: the features the
	// record leaves for the owner to pick or decline.
	Undecided  []string `json:"undecided"`
	BackupPath string   `json:"backup_path,omitempty"`
	Steps      []string `json:"steps"`
	Message    string   `json:"message"`
}

type RecordOut struct {
	Installed []string `json:"installed"`
	Declined  []string `json:"declined"`
}

const (
	ProposalNew    = "new"
	ProposalUpdate = "update"
)

// BuildPlan compares a workflow file with the bundled templates and
// the feature catalog and lists the features to propose. It reads only;
// templateFlag picks the template for an unstamped file, and all lists every
// catalog feature the file lacks (ignoring the stamp, propose: and
// templates:), declined ones included.
//
// What counts as installed comes from the features: record when the file has
// one, otherwise from the catalog's detection probes.
func BuildPlan(path string, raw []byte, templateFlag string, all bool) (*Plan, error) {
	plan := &Plan{WorkflowFile: path, All: all, Proposals: []Proposal{}, Present: []string{}, Declined: []string{}, Unrecorded: []string{}, Undecided: []string{}, Steps: []string{}}
	stamp := ReadStamp(raw)
	templateFlag = strings.TrimSpace(templateFlag)

	if stamp == CustomStamp {
		plan.Status = StatusCustom
		plan.Template = CustomStamp
		plan.Message = "workflow.yaml is marked `template: custom`; there is nothing to upgrade. Remove the line or set `template: <name>@<version>` to get proposals."
		return plan, nil
	}

	if stamp == "" {
		plan.Status = StatusUnstamped
		plan.Template = templateFlag
		if plan.Template == "" {
			plan.Template = DefaultTemplate
		}
	} else {
		name, version, ok := ParseStamp(stamp)
		if !ok {
			plan.Status = StatusUnknownTemplate
			plan.Template = stamp
			plan.Message = fmt.Sprintf("`template: %s` is not <name>@vX.Y.Z (e.g. %s@%s) or custom. Fix the stamp, or remove it and pass --template.", stamp, DefaultTemplate, "v0.36.0")
			return plan, nil
		}
		if templateFlag != "" && templateFlag != name {
			return nil, fmt.Errorf("workflow.yaml is stamped %s; --template only applies to an unstamped workflow.yaml", stamp)
		}
		plan.Template, plan.Current = name, version
	}

	latest, err := BundledVersion(plan.Template)
	if err != nil {
		if !IsBundled(plan.Template) {
			plan.Status = StatusUnknownTemplate
			plan.Message = fmt.Sprintf("no bundled template named %q (bundled: %s).", plan.Template, strings.Join(Templates(), ", "))
			return plan, nil
		}
		return nil, err
	}
	plan.Latest = latest
	upToDate := plan.Status == "" && CompareVersions(plan.Current, latest) >= 0
	if plan.Status == "" {
		plan.Status = StatusBehind
		if upToDate {
			plan.Status = StatusUpToDate
		}
	}

	entries, err := Catalog()
	if err != nil {
		return nil, err
	}
	inTemplate, err := templateFeatures(plan.Template)
	if err != nil {
		return nil, err
	}
	rec, hasRecord := ReadRecord(raw)
	installed, declined := RecordIndex(rec)
	plan.RecordSource = "detection"
	if hasRecord {
		plan.RecordSource = "record"
		plan.Record = &RecordOut{Installed: append([]string{}, rec.Installed...), Declined: append([]string{}, rec.Declined...)}
	}
	doc := ParseDoc(raw)

	idx := catalogIndex(entries)
	has := func(id string) bool {
		if _, ok := installed[id]; ok {
			return true
		}
		return Detect(idx[id], doc).Matches > 0
	}
	for _, e := range entries {
		// newer: the stamp predates the entry, so the project hasn't been
		// offered this version yet. Only a file without a features: record
		// is gated by it. With a record, the record decides: an entry that
		// is neither installed nor declined is proposed whatever the stamp
		// says, so bumping the stamp can't hide an undecided feature.
		newer := hasRecord || plan.Current == "" || CompareVersions(e.Version, plan.Current) > 0
		var needs []string
		for _, id := range e.Requires {
			if !has(id) {
				needs = append(needs, id)
			}
		}
		pending := newer && e.Propose && e.appliesTo(plan.Template) && len(needs) == 0
		offer := all || pending
		p := Proposal{
			ID:         e.ID,
			Kind:       ProposalNew,
			Version:    e.Version,
			Category:   e.Category,
			InTemplate: inTemplate[e.ID],
			Title:      e.Title,
			What:       strings.TrimSpace(e.What),
			Why:        strings.TrimSpace(e.Why),
			Where:      e.Where,
			Snippet:    strings.ReplaceAll(strings.TrimRight(e.Snippet, "\n"), "{{today}}", time.Now().Format("2006-01-02")),
			Question:   e.Question,
			Setup:      e.Setup,
			Needs:      needs,
		}
		if hasRecord {
			if v, ok := installed[e.ID]; ok {
				plan.Present = append(plan.Present, e.ID)
				if (all || newer) && v != "" && CompareVersions(e.Version, v) > 0 {
					p.Kind, p.Installed = ProposalUpdate, v
					plan.Proposals = append(plan.Proposals, p)
					if newer {
						plan.Undecided = append(plan.Undecided, e.ID)
					}
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
			if Detect(e, doc).Matches > 0 {
				// In the file but not in the record: scan, don't propose.
				plan.Unrecorded = append(plan.Unrecorded, e.ID)
				continue
			}
		} else if Detect(e, doc).Matches > 0 {
			plan.Present = append(plan.Present, e.ID)
			continue
		}
		if offer {
			plan.Proposals = append(plan.Proposals, p)
		}
		if pending {
			plan.Undecided = append(plan.Undecided, e.ID)
		}
	}
	for i := range plan.Proposals {
		plan.Proposals[i].Number = i + 1
	}

	// A current stamp is up to date only when the record decides
	// everything: an unrecorded feature or an undecided proposal is not.
	if upToDate && len(plan.Unrecorded) > 0 {
		plan.Status, upToDate = StatusStaleRecord, false
	} else if upToDate && len(plan.Undecided) > 0 {
		plan.Status, upToDate = StatusUndecided, false
	}

	stampLine := fmt.Sprintf("template: %s@%s", plan.Template, latest)
	scanStep := "Record the result: `issue-cli workflow scan --installed <picked ids> --declined <declined ids>` (comma-separated). It writes the features: record, and upgrade stops proposing declined items; the human can re-open one later with `issue-cli workflow scan --forget <id>`."
	if len(plan.Proposals) == 0 {
		switch {
		case plan.Status == StatusStaleRecord:
			plan.Message = fmt.Sprintf("workflow.yaml's stamp matches the bundled %s template (%s), but its %s.", plan.Template, latest, plan.StaleRecord())
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

	plan.BackupPath = NextBackupPath(path)
	note := ""
	switch plan.Status {
	case StatusStaleRecord:
		note = fmt.Sprintf("The stamp matches the bundled %s template (%s), but the %s. ", plan.Template, latest, plan.StaleRecord())
	case StatusUndecided:
		note = fmt.Sprintf("The stamp matches the bundled %s template (%s), but the features: record leaves %d undecided; `issue-cli workflow scan --installed/--declined` records each pick. ", plan.Template, latest, len(plan.Undecided))
	}
	plan.Message = note + fmt.Sprintf("%d feature(s) to propose. Only act on them if the operator asked you to upgrade workflow.yaml; otherwise tell them an upgrade is available and stop.", len(plan.Proposals))
	plan.Steps = []string{
		"Continue only if the operator explicitly asked for a workflow upgrade. Otherwise tell them it is available (`issue-cli workflow upgrade` lists it) and stop.",
		"Propose the items one at a time: say what it is and why it helps, ask its question, and note whether the human picks or declines it. Don't apply anything they didn't pick.",
		fmt.Sprintf("Back up the current file: cp %s %s", path, plan.BackupPath),
		fmt.Sprintf("Write the new %s: the old content plus each picked snippet, adapted to the project's own status names and sections. Keep every existing customisation; ask the human when a snippet conflicts with one.", filepath.Base(path)),
		fmt.Sprintf("Only once every proposal is installed or declined, set the stamp at the top of the file: `%s`. While any is undecided, leave the stamp as it is.", stampLine),
		scanStep,
		"Validate: run `issue-cli process workflow` and fix anything under == Warnings (workflow.yaml) ==.",
		"Existing issues keep the checklist sections they already have; new sections appear as issues pass the changed transitions.",
	}
	return plan, nil
}

// StaleRecord is the warning for a features: record that misses features
// the file has, whatever the stamp says; "" otherwise. Undecided proposals
// are the owner's call, not staleness.
func (p *Plan) StaleRecord() string {
	if p == nil || len(p.Unrecorded) == 0 {
		return ""
	}
	return fmt.Sprintf("features: record is stale: %s detected in the file but not recorded; `issue-cli workflow scan` records it", strings.Join(p.Unrecorded, ", "))
}

// templateFeatures is the set of feature ids in a bundled template's
// own features: record.
func templateFeatures(name string) (map[string]bool, error) {
	data, err := ReadTemplate(name)
	if err != nil {
		return nil, err
	}
	rec, _ := ReadRecord(data)
	installed, _ := RecordIndex(rec)
	set := make(map[string]bool, len(installed))
	for id := range installed {
		set[id] = true
	}
	return set, nil
}

func IsBundled(name string) bool {
	for _, t := range Templates() {
		if t == name {
			return true
		}
	}
	return false
}

// NextBackupPath returns the first workflow.oldN.yaml next to path
// that doesn't exist yet.
func NextBackupPath(path string) string {
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for n := 1; ; n++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s.old%d.yaml", base, n))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
