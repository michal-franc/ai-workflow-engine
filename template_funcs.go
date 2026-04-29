package main

import (
	"fmt"
	"html/template"
	"regexp"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func canonicalStatusKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func orderedStatusesForIssue(wf *tracker.WorkflowConfig, current string) []string {
	statuses := append([]string{}, wf.GetStatusOrder()...)
	current = strings.TrimSpace(current)
	if current == "" {
		return statuses
	}
	for _, status := range statuses {
		if status == current {
			return statuses
		}
	}
	return append([]string{current}, statuses...)
}

var funcMap = template.FuncMap{
	"statusColor": func(s string) string {
		colors := map[string]string{
			"idea":          "#8b5cf6",
			"in design":     "#3b82f6",
			"backlog":       "#64748b",
			"in progress":   "#eab308",
			"testing":       "#f97316",
			"human-testing": "#ec4899",
			"documentation": "#14b8a6",
			"shipping":      "#0ea5e9",
			"done":          "#22c55e",
		}
		if c, ok := colors[canonicalStatusKey(s)]; ok {
			return c
		}
		return "#6b7280"
	},
	"statusTextColor": func(s string) string {
		dark := map[string]bool{"in progress": true, "testing": true, "human-testing": true}
		if dark[canonicalStatusKey(s)] {
			return "#000000"
		}
		return "#ffffff"
	},
	"priorityColor": func(p string) string {
		colors := map[string]string{
			"low":      "#6b7280",
			"medium":   "#3b82f6",
			"high":     "#f97316",
			"critical": "#ef4444",
		}
		if c, ok := colors[p]; ok {
			return c
		}
		return "#6b7280"
	},
	"joinLabels": func(labels []string) string {
		return strings.Join(labels, ", ")
	},
	"addCounts": func(counts map[string]int, keys ...string) int {
		total := 0
		for _, key := range keys {
			total += counts[key]
		}
		return total
	},
	"safeHTML": func(s string) template.HTML {
		return template.HTML(s)
	},
	"urlEncodeColor": func(s string) string {
		return strings.ReplaceAll(s, "#", "%23")
	},
	"assigneeColor": func(name string) string {
		if name == "" {
			return ""
		}
		colors := []string{
			"#f97316", "#3b82f6", "#22c55e", "#a855f7",
			"#ef4444", "#eab308", "#14b8a6", "#ec4899",
			"#6366f1", "#84cc16",
		}
		h := 0
		for _, c := range name {
			h = h*31 + int(c)
		}
		if h < 0 {
			h = -h
		}
		return colors[h%len(colors)]
	},
	"boardCardFields": func(fields []string, issue *IssueView) []BoardCardField {
		var result []BoardCardField
		for _, f := range fields {
			switch f {
			case "system":
				if issue.System != "" {
					result = append(result, BoardCardField{Name: f, Value: issue.System})
				}
			case "labels":
				if len(issue.Labels) > 0 {
					result = append(result, BoardCardField{Name: f, Values: issue.Labels, IsList: true})
				}
			case "priority":
				if issue.Priority != "" {
					result = append(result, BoardCardField{Name: f, Value: issue.Priority})
				}
			case "assignee":
				if issue.Assignee != "" {
					result = append(result, BoardCardField{Name: f, Value: issue.Assignee})
				}
			case "version":
				if issue.Version != "" {
					result = append(result, BoardCardField{Name: f, Value: issue.Version})
				}
			case "number":
				if issue.Number > 0 {
					result = append(result, BoardCardField{Name: f, Value: fmt.Sprintf("#%d", issue.Number)})
				}
			default:
				for _, ef := range issue.ExtraFields {
					if ef.Key != f {
						continue
					}
					if ef.IsList {
						if len(ef.Values) > 0 {
							result = append(result, BoardCardField{Name: f, Values: ef.Values, IsList: true})
						}
					} else if ef.Value != "" {
						result = append(result, BoardCardField{Name: f, Value: ef.Value})
					}
					break
				}
			}
		}
		return result
	},
	"formatScore": func(score *tracker.ScoreBreakdown) string {
		if score == nil {
			return ""
		}
		t := score.Total
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%.1f", t)
	},
	"scoreColor": func(score *tracker.ScoreBreakdown) string {
		if score == nil {
			return "#6b7280"
		}
		switch {
		case score.Total >= 60:
			return "#ef4444"
		case score.Total >= 30:
			return "#f97316"
		case score.Total >= 10:
			return "#eab308"
		default:
			return "#22c55e"
		}
	},
	"linkIssueRefs": func(html string, prefix string, slugMap map[string]string) template.HTML {
		return template.HTML(linkIssueRefs(html, prefix, slugMap))
	},
}

var issueRefRe = regexp.MustCompile(`#([a-zA-Z0-9][\w/.-]*)`)

func linkIssueRefs(html, prefix string, slugMap map[string]string) string {
	return issueRefRe.ReplaceAllStringFunc(html, func(match string) string {
		ref := match[1:]
		if slug, ok := slugMap[ref]; ok {
			return fmt.Sprintf(`<a href="%s/issue/%s" class="issue-ref">%s</a>`, prefix, slug, match)
		}
		if slug, ok := slugMap[strings.ToLower(ref)]; ok {
			return fmt.Sprintf(`<a href="%s/issue/%s" class="issue-ref">%s</a>`, prefix, slug, match)
		}
		return match
	})
}
