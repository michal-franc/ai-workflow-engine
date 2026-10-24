package tracker

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// CriteriaSection is the section whose checkboxes are an issue's acceptance
// criteria: the boxes proof links point at (AC1, AC2, …).
const CriteriaSection = "Acceptance Criteria"

// Proof link kinds.
const (
	ProofTest   = "test"
	ProofStep   = "step"
	ProofCommit = "commit"
	ProofValue  = "value"
	ProofShot   = "shot"
)

// ProofLink is one link from an acceptance criterion to its proof, written
// by `issue-cli prove` into the data sidecar under "proofs". Links declared
// next to the proof (commit trailers, playtest steps) are never stored.
type ProofLink struct {
	Kind string    `json:"kind"`
	Ref  string    `json:"ref"`
	At   time.Time `json:"at"`
	// Criterion is the criterion's text when the link was made, so a
	// renumbered or rewritten criterion shows up as changed.
	Criterion string `json:"criterion"`
}

// Criteria returns the issue's acceptance criteria boxes, in order.
func Criteria(body string) []CheckboxItem {
	var out []CheckboxItem
	for _, it := range ListCheckboxes(body) {
		if strings.EqualFold(it.Section, CriteriaSection) {
			out = append(out, it)
		}
	}
	return out
}

// IsVisualCriterion reports whether a criterion is marked visual: its text
// ends in "(visual)".
func IsVisualCriterion(text string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(text)), "(visual)")
}

// proofChecker runs the criteria_proven check. It needs git and playtest
// runs, which tracker can't reach, so internal/proof registers it.
var proofChecker func(w *WorkflowConfig, issue *Issue, comments []Comment) error

// SetProofChecker registers the criteria_proven check (internal/proof does
// this in init, so any binary that imports it can run the rule).
func SetProofChecker(fn func(w *WorkflowConfig, issue *Issue, comments []Comment) error) {
	proofChecker = fn
}

const criteriaProvenRule = "criteria_proven"

// ProofGateSince returns the date of the workflow's criteria_proven rule
// ("criteria_proven: since 2026-10-24"), and whether the workflow has one.
func (w *WorkflowConfig) ProofGateSince() (string, bool) {
	if w == nil {
		return "", false
	}
	for _, t := range w.Transitions {
		for _, a := range t.Actions {
			if a.Type != "validate" {
				continue
			}
			name, arg, _ := strings.Cut(a.Rule, ":")
			if strings.TrimSpace(name) == criteriaProvenRule {
				since, _ := parseProofSince(arg)
				return since, true
			}
		}
	}
	return "", false
}

// ProofRequired reports whether the proof gate covers issue: the workflow
// has a criteria_proven rule, the issue was created on or after its date,
// and it has acceptance criteria checkboxes.
func (w *WorkflowConfig) ProofRequired(issue *Issue) bool {
	since, ok := w.ProofGateSince()
	if !ok || issue == nil {
		return false
	}
	return proofCovers(since, issue)
}

func proofCovers(since string, issue *Issue) bool {
	if created := strings.TrimSpace(issue.Created); created != "" && since != "" {
		if len(created) > 10 {
			created = created[:10]
		}
		if created < since {
			return false
		}
	}
	return len(Criteria(issue.BodyRaw)) > 0
}

var proofSinceRe = regexp.MustCompile(`^since\s+(\d{4}-\d{2}-\d{2})$`)

// parseProofSince reads the criteria_proven argument: "since YYYY-MM-DD", or
// empty for every issue.
func parseProofSince(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", nil
	}
	m := proofSinceRe.FindStringSubmatch(arg)
	if m == nil {
		return "", fmt.Errorf("criteria_proven: want \"since YYYY-MM-DD\", got %q", arg)
	}
	if _, err := time.Parse("2006-01-02", m[1]); err != nil {
		return "", fmt.Errorf("criteria_proven: %q is not a date", m[1])
	}
	return m[1], nil
}

func (w *WorkflowConfig) checkCriteriaProven(arg string, issue *Issue, comments []Comment) error {
	since, err := parseProofSince(arg)
	if err != nil {
		return err
	}
	if !proofCovers(since, issue) {
		return nil
	}
	if proofChecker == nil {
		return fmt.Errorf("criteria_proven needs the proof checks, which this program doesn't include — run the transition with issue-cli")
	}
	return proofChecker(w, issue, comments)
}

// checkCriteriaCheckboxes is the criteria_checkboxes rule: the section has
// checkboxes, every list item in it is one, and none still carries the text
// the workflow's template put there.
func (w *WorkflowConfig) checkCriteriaCheckboxes(section string, issue *Issue) error {
	if section == "" {
		return fmt.Errorf("criteria_checkboxes rule requires a section name argument")
	}
	text, ok := SectionText(issue.BodyRaw, section)
	total, _ := CountCheckboxesInSection(issue.BodyRaw, section)
	if !ok || total == 0 {
		return fmt.Errorf("no checkboxes found in section %q — write each criterion as a checkbox:\n\n  - [ ] <what someone can observe when it works>", section)
	}
	var plain []string
	for _, line := range strings.Split(text, "\n") {
		item, isItem := listItemText(line)
		if isItem && !checkboxPrefixRe.MatchString(strings.TrimSpace(line)) {
			plain = append(plain, item)
		}
	}
	placeholders := w.sectionPlaceholders(section)
	var unchanged []CheckboxItem
	for _, it := range ListCheckboxes(issue.BodyRaw) {
		if strings.EqualFold(it.Section, section) && placeholders[strings.TrimSpace(it.Text)] {
			unchanged = append(unchanged, it)
		}
	}
	if len(plain) == 0 && len(unchanged) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "section %q has criteria that can't be proven:", section)
	for _, p := range plain {
		fmt.Fprintf(&b, "\n  - plain bullet, not a checkbox: %s", p)
	}
	for _, it := range unchanged {
		fmt.Fprintf(&b, "\n  - %s still has the template text: %s", it.ID, it.Text)
	}
	fmt.Fprintf(&b, "\n\nRewrite the section with one checkbox per criterion:\n  issue-cli replace %s --section %q --body \"- [ ] ...\"", issue.Slug, section)
	return fmt.Errorf("%s", b.String())
}

var (
	checkboxPrefixRe = regexp.MustCompile(`^[-*+]\s+\[[ xX]\]`)
	listItemRe       = regexp.MustCompile(`^\s{0,3}(?:[-*+]|\d+[.)])\s+(.*)$`)
)

// listItemText returns a top-level list item's text.
func listItemText(line string) (string, bool) {
	m := listItemRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// sectionPlaceholders collects the checkbox texts the workflow's
// append_section actions write into section: a box still holding one was
// never filled in.
func (w *WorkflowConfig) sectionPlaceholders(section string) map[string]bool {
	out := map[string]bool{}
	for _, t := range w.Transitions {
		for _, a := range t.Actions {
			if a.Type != "append_section" || !strings.EqualFold(strings.TrimSpace(a.Title), section) {
				continue
			}
			for _, it := range ListCheckboxes(a.Body) {
				out[strings.TrimSpace(it.Text)] = true
			}
		}
	}
	return out
}

// TransitionChecksProof reports whether from → to runs criteria_proven.
func (w *WorkflowConfig) TransitionChecksProof(from, to string) bool {
	for _, a := range w.transitionActions(from, to) {
		name, _, _ := strings.Cut(a.Rule, ":")
		if a.Type == "validate" && strings.TrimSpace(name) == criteriaProvenRule {
			return true
		}
	}
	return false
}

// DeclinesFeature reports whether the workflow's feature record lists id as
// declined.
func (w *WorkflowConfig) DeclinesFeature(id string) bool {
	if w == nil {
		return false
	}
	for _, f := range w.Features.Declined {
		if name, _, _ := strings.Cut(f, "@"); strings.TrimSpace(name) == id {
			return true
		}
	}
	return false
}

// HasFeature reports whether the workflow's feature record has id installed
// (any version).
func (w *WorkflowConfig) HasFeature(id string) bool {
	if w == nil {
		return false
	}
	for _, f := range w.Features.Installed {
		if name, _, _ := strings.Cut(f, "@"); strings.TrimSpace(name) == id {
			return true
		}
	}
	return false
}
