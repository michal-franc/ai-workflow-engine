package tracker

import (
	"fmt"
	"sort"
	"strings"
)

// The {{name}} placeholders each templated workflow.yaml field fills. The
// value maps are what the dispatch and lead code substitute, so issue-cli
// lint's placeholders check reads the allowed names from the same place
// (PlaceholderNames).

// IssueActionValues fills an issue action's prompt (issue_actions, actions).
func IssueActionValues(issue *Issue) map[string]string {
	return map[string]string{
		"slug":     issue.Slug,
		"title":    issue.Title,
		"status":   issue.Status,
		"system":   issue.System,
		"priority": issue.Priority,
		"number":   fmt.Sprintf("%d", issue.Number),
	}
}

// ProjectActionValues fills a project action's prompt (project_actions).
func ProjectActionValues(proj *Project) map[string]string {
	return map[string]string{"project": proj.Name}
}

// LeadValues fills the lead brief (leads.project).
func LeadValues(projectName, lead string) map[string]string {
	return map[string]string{"project": projectName, "lead": lead, "project_lead": lead}
}

// FillPlaceholders replaces each {{name}} in s with values[name]. An unknown
// {{...}} is left as written, so a prompt never fails to render.
func FillPlaceholders(s string, values map[string]string) string {
	pairs := make([]string, 0, 2*len(values))
	for name, v := range values {
		pairs = append(pairs, "{{"+name+"}}", v)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

// PlaceholderNames lists the names a value map fills, sorted.
func PlaceholderNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
