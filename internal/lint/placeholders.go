package lint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"github.com/michal-franc/issue-viewer/internal/tracker/validations"
)

var placeholderRe = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// field is one templated (or untemplated) workflow.yaml text.
type field struct {
	what string // "issue_actions[defer].prompt"
	text string
	// names the field fills; nil means it is not templated at all.
	names []string
	// exact: filled by plain replacement, so {{ slug }} with spaces is
	// not filled (validator hints use text/template, which allows them).
	exact bool
}

// checkPlaceholders flags a {{name}} its field never fills: the agent (or
// the person reading a hint) sees it as written.
func checkPlaceholders(env *Env) []Finding {
	var out []Finding
	for _, f := range placeholderFields(env.Workflow) {
		for _, m := range placeholderRe.FindAllStringSubmatch(f.text, -1) {
			raw, name := m[0], m[1]
			var msg, fix string
			switch {
			case f.names == nil:
				msg = fmt.Sprintf("%s is not templated, so %s reaches the agent as written", f.what, raw)
				fix = "write the value out, or move the text to a field that fills it"
			case !contains(f.names, name):
				msg = fmt.Sprintf("%s: %s is never filled (it fills %s)", f.what, raw, braces(f.names))
				if near := nearestName(name, f.names); near != "" {
					fix = fmt.Sprintf("did you mean {{%s}}?", near)
				}
			case f.exact && raw != "{{"+name+"}}":
				msg = fmt.Sprintf("%s: %s has spaces, so it is never filled", f.what, raw)
				fix = fmt.Sprintf("write {{%s}}", name)
			default:
				continue
			}
			out = append(out, Finding{Where: env.where(env.lineOf(raw)), Message: msg, Fix: fix})
		}
	}
	return out
}

func placeholderFields(wf *tracker.WorkflowConfig) []field {
	issueNames := tracker.PlaceholderNames(tracker.IssueActionValues(&tracker.Issue{}))
	projectNames := tracker.PlaceholderNames(tracker.ProjectActionValues(&tracker.Project{}))
	leadNames := tracker.PlaceholderNames(tracker.LeadValues("", ""))
	hintNames := validations.TemplateNames()

	var out []field
	for _, a := range wf.IssueActionList() {
		out = append(out, field{fmt.Sprintf("issue_actions[%s].prompt", a.ID), a.Prompt, issueNames, true})
	}
	for _, a := range wf.ProjectActions {
		out = append(out, field{fmt.Sprintf("project_actions[%s].prompt", a.ID), a.Prompt, projectNames, true})
	}
	out = append(out, field{"leads.project", wf.Leads.Project, leadNames, true})

	scope := func(prefix string, statuses []tracker.WorkflowStatus, transitions []tracker.WorkflowTransition) {
		for _, s := range statuses {
			out = append(out,
				field{fmt.Sprintf("%sstatuses[%s].prompt", prefix, s.Name), s.Prompt, nil, false},
				field{fmt.Sprintf("%sstatuses[%s].template", prefix, s.Name), s.Template, nil, false})
		}
		for _, t := range transitions {
			edge := fmt.Sprintf("%s%s → %s", prefix, t.From, t.To)
			for _, a := range t.Actions {
				switch a.Type {
				case "validate":
					out = append(out,
						field{edge + " " + a.Rule + " hint", a.Hint, hintNames, false},
						field{edge + " " + a.Rule + " command", a.Command, hintNames, false})
				case "inject_prompt":
					out = append(out, field{edge + " inject_prompt", a.Prompt, nil, false})
				case "append_section":
					out = append(out, field{edge + " append_section " + a.Title, a.Body, nil, false})
				}
			}
			for _, f := range t.Fields {
				out = append(out, field{edge + " fields[" + f.Name + "].prompt", f.Prompt, nil, false})
			}
		}
	}
	scope("", wf.Statuses, wf.Transitions)
	for _, name := range wf.TypeNames() {
		t := wf.Types[name]
		scope("types."+name+": ", t.Statuses, t.Transitions)
	}
	for _, name := range sortedKeys(wf.Systems) {
		s := wf.Systems[name]
		scope("systems."+name+": ", s.Statuses, s.Transitions)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func braces(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "{{" + n + "}}"
	}
	return strings.Join(out, " ")
}

func nearestName(name string, names []string) string {
	best, bestD := "", 3
	for _, n := range names {
		if d := editDistance(name, n); d < bestD {
			best, bestD = n, d
		}
	}
	return best
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
