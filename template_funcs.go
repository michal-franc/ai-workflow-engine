package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/milestone"
	"github.com/michal-franc/issue-viewer/internal/testids"
	"github.com/michal-franc/issue-viewer/internal/timeline"
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

// StatusTabs is what the status-tabs partial renders.
type StatusTabs struct {
	Prefix  string
	Builtin bool // the Built-in tab is the selected one
}

var funcMap = template.FuncMap{
	"releaseBranch": milestone.ReleaseBranch,
	// commentDay is the day of a comment date stored as RFC3339 or a day.
	"commentDay": tracker.CommentDay,
	// sidebar is replaced per server (Server.sidebarNav): the Nav the shared
	// sidebar partial renders for a project page.
	"sidebar": func(prefix, active string) Nav { return Nav{Prefix: prefix} },
	// statusTabs is the data of the Status page's Project status / Built-in
	// tabs (_status_tabs.html), shown when workbench.status_page is set.
	"statusTabs": func(prefix string, builtin bool) StatusTabs { return StatusTabs{Prefix: prefix, Builtin: builtin} },
	// bodyHash is the fingerprint the detail page sends back with a body
	// save, so a save from a stale page is refused (409).
	"bodyHash": tracker.BodyHash,
	"statusColor": func(s string) string {
		// Keys are canonicalStatusKey form: lower case, spaces as hyphens.
		colors := map[string]string{
			"idea":          "#8b5cf6",
			"in-design":     "#3b82f6",
			"backlog":       "#64748b",
			"in-progress":   "#eab308",
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
		dark := map[string]bool{"in-progress": true, "testing": true, "human-testing": true}
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
	// statusFragment normalises a status name into the form used by the
	// `id="approve-<status>"` anchors on the detail page so deep links emitted
	// by the CLI (`#approve-in-progress`) line up with the DOM.
	"isURL": func(s string) bool {
		return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
	},
	// tid turns a status or slug into one data-testid segment (see
	// internal/testids).
	"tid":    testids.Segment,
	"plural": plural,
	// statsCard pairs a chart card with its dashboard for the statsChart
	// template; statsRangeLabel names the range ("30 days").
	"statsCard":       func(d *Dashboard, c ChartCard) statsCardData { return statsCardData{Dash: d, Card: c} },
	"statsRangeLabel": statsRangeLabel,
	// head and rest split a list at n for "+K more" folds: range head 5 .X
	// shows the first five, with rest 5 .X the others go in the fold.
	// inlineMarkdown renders a changelog line's bold and code.
	"inlineMarkdown": inlineMarkdown,
	"head":           func(n int, list any) any { return sliceAt(list, n, true) },
	"rest":           func(n int, list any) any { return sliceAt(list, n, false) },
	// agentTone is the chip class for a Status page agent state.
	"agentTone": func(state string) string {
		switch state {
		case "working":
			return "chip-green"
		case "at gate":
			return "chip-amber"
		case "question":
			return "chip-blue"
		}
		return ""
	},
	"statusFragment": func(s string) string {
		return strings.ReplaceAll(strings.TrimSpace(s), " ", "-")
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
			case "type":
				if issue.WorkType != "" {
					result = append(result, BoardCardField{Name: f, Value: issue.WorkType})
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
	"toJSON": func(v any) template.JS {
		b, err := json.Marshal(v)
		if err != nil {
			return template.JS("null")
		}
		return template.JS(b)
	},
	// The Timeline tab's formats (timeline.go): durations, clock times and
	// bar widths.
	"tlDur":   fmtDur,
	"tlSpan":  fmtSpan,
	"tlClock": func(t time.Time) string { return t.Local().Format("15:04") },
	"tlDay":   func(t time.Time) string { return t.Local().Format("Mon 2 Jan") },
	"tlDate":  func(t time.Time) string { return t.Local().Format("2 Jan 2006") },
	"tlPct":   func(p float64) template.CSS { return template.CSS(fmt.Sprintf("%.3f%%", p)) },
	"tlShare": func(d, total time.Duration) string { return fmt.Sprintf("%.0f%%", durPct(d, total)) },
	"tlSub":   func(a, b time.Time) time.Duration { return a.Sub(b) },
	// "p50 / p90" on /stats.
	"tlP":    func(p timeline.Pct) string { return fmtDur(p.P50) + " / " + fmtDur(p.P90) },
	"tlFrac": func(f float64) string { return fmt.Sprintf("%.0f%%", f*100) },
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

// sliceAt is list[:n] (head) or list[n:] (rest), clamped; nil for a list
// that is not a slice or an empty rest, so {{with rest …}} skips it.
func sliceAt(list any, n int, head bool) any {
	v := reflect.ValueOf(list)
	if v.Kind() != reflect.Slice {
		return nil
	}
	if n > v.Len() {
		n = v.Len()
	}
	if head {
		return v.Slice(0, n).Interface()
	}
	if n == v.Len() {
		return nil
	}
	return v.Slice(n, v.Len()).Interface()
}
