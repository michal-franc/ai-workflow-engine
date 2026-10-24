package lint

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var checkboxRe = regexp.MustCompile(`(?m)^\s*- \[[ xX]\]`)

// checkboxGates are the rules that need a section with checkboxes.
var checkboxGates = []string{"section_has_checkboxes:", "section_checkboxes_checked:", "criteria_checkboxes:"}

// scope is the workflow one kind of issue walks: a type (or the base) in a
// system (or none).
type scope struct {
	label string
	wf    *tracker.WorkflowConfig
}

func scopes(wf *tracker.WorkflowConfig) []scope {
	types := []string{""}
	if wf.HasTypes() {
		types = wf.TypeNames()
	}
	systems := append([]string{""}, sortedKeys(wf.Systems)...)
	var out []scope
	for _, t := range types {
		for _, s := range systems {
			var parts []string
			if t != "" {
				parts = append(parts, "type "+t)
			}
			if s != "" {
				parts = append(parts, "system "+s)
			}
			out = append(out, scope{strings.Join(parts, ", "), wf.ForIssue(&tracker.Issue{Type: t, System: s})})
		}
	}
	return out
}

// written maps each section title (lowercased) an append_section or a
// legacy status template writes to the bodies written under it.
func written(wf *tracker.WorkflowConfig) map[string][]string {
	out := map[string][]string{}
	for _, t := range wf.Transitions {
		for _, a := range t.Actions {
			if a.Type == "append_section" && strings.TrimSpace(a.Title) != "" {
				k := strings.ToLower(strings.TrimSpace(a.Title))
				out[k] = append(out[k], a.Body)
			}
		}
	}
	for _, s := range wf.Statuses {
		title, body := "", []string{}
		flush := func() {
			if title != "" {
				out[title] = append(out[title], strings.Join(body, "\n"))
			}
		}
		for _, line := range strings.Split(s.Template, "\n") {
			if strings.HasPrefix(line, "## ") {
				flush()
				title, body = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## "))), nil
				continue
			}
			body = append(body, line)
		}
		flush()
	}
	return out
}

type gate struct {
	edge, rule, section string
}

// gates lists the scope's section gates: validate actions on its edges and
// legacy statuses[].validation rules.
func gates(wf *tracker.WorkflowConfig) []gate {
	var out []gate
	add := func(edge, rule string) {
		rule = strings.TrimSpace(rule)
		if rule == "has_test_plan" {
			out = append(out, gate{edge, rule, "test plan"})
			return
		}
		for _, p := range checkboxGates {
			if strings.HasPrefix(rule, p) {
				out = append(out, gate{edge, rule, strings.ToLower(strings.TrimSpace(strings.TrimPrefix(rule, p)))})
			}
		}
	}
	for _, t := range wf.Transitions {
		for _, a := range t.Actions {
			if a.Type == "validate" {
				add(t.From+" → "+t.To, a.Rule)
			}
		}
	}
	for _, s := range wf.Statuses {
		for _, r := range s.Validation {
			add("into "+s.Name, r)
		}
	}
	return out
}

// checkSections flags gates on sections the workflow's templates don't
// write the way the gate needs: a checkbox gate on a section nothing
// writes or that is written without a checkbox, a has_test_plan gate whose
// Test Plan template lacks the exact ### Automated / ### Manual headings.
// A type that drops the edge appending a section is the workflow check's.
func checkSections(env *Env) []Finding {
	all := scopes(env.Workflow)
	anywhere := map[string]bool{}
	for _, sc := range all {
		for k := range written(sc.wf) {
			anywhere[k] = true
		}
	}
	seen := map[string]bool{}
	var out []Finding
	for _, sc := range all {
		w := written(sc.wf)
		for _, g := range gates(sc.wf) {
			var msg, fix string
			bodies := w[g.section]
			switch {
			case g.rule == "has_test_plan":
				if len(bodies) == 0 || anyBody(bodies, func(b string) bool { a, m := tracker.HasTestPlan("## Test Plan\n" + b); return a && m }) {
					continue
				}
				msg = fmt.Sprintf("%s has_test_plan: the Test Plan template lacks the exact ### Automated and ### Manual headings, so the gate fails on the template as written", g.edge)
				fix = "give the ## Test Plan template ### Automated and ### Manual subsections"
			case !anywhere[g.section]:
				msg = fmt.Sprintf("%s %s: no append_section or status template writes ## %s, so the gate fails until someone adds it by hand", g.edge, g.rule, title(g.rule))
				fix = fmt.Sprintf("add an append_section titled %q with its checkboxes to an earlier edge", title(g.rule))
			case len(bodies) > 0 && !anyBody(bodies, checkboxRe.MatchString):
				msg = fmt.Sprintf("%s %s: ## %s is written without a checkbox, so the gate fails on the template as written", g.edge, g.rule, title(g.rule))
				fix = fmt.Sprintf("put the boxes the gate checks into the ## %s template", title(g.rule))
			default:
				continue
			}
			if seen[msg] {
				continue
			}
			seen[msg] = true
			if sc.label != "" {
				msg += " (" + sc.label + ")"
			}
			out = append(out, Finding{Where: env.where(env.lineOf(g.rule)), Message: msg, Fix: fix})
		}
	}
	return out
}

func anyBody(bodies []string, ok func(string) bool) bool {
	for _, b := range bodies {
		if ok(b) {
			return true
		}
	}
	return false
}

// title is the section a rule names, as written.
func title(rule string) string {
	_, t, _ := strings.Cut(rule, ":")
	return strings.TrimSpace(t)
}
