package lint

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var decisionDayRe = regexp.MustCompile(`(?m)^- \*\*D-\d+\*\* · (\d{4}-\d{2}-\d{2})`)

// checkUndated flags issues without created:. Fix suggests the earliest
// date recorded for the issue: its stats sidecar, comments, decisions, else
// the file's mtime.
func checkUndated(env *Env) []Finding {
	var out []Finding
	for _, issue := range env.Issues {
		if strings.TrimSpace(issue.Created) != "" {
			continue
		}
		out = append(out, Finding{
			Where:   issue.Slug,
			Message: "no created: date; the criteria_proven gate counts an undated issue as new",
			Fix:     fmt.Sprintf("issue-cli set-meta %s --key created --value %s", issue.Slug, earliestDay(env, issue)),
		})
	}
	return out
}

func earliestDay(env *Env, issue *tracker.Issue) string {
	day := ""
	offer := func(d string) {
		if len(d) >= 10 && (day == "" || d[:10] < day) {
			day = d[:10]
		}
	}
	if stats, err := tracker.LoadStats(issue.FilePath); err == nil {
		for _, t := range stats.Transitions {
			if !t.TS.IsZero() {
				offer(t.TS.Local().Format("2006-01-02"))
			}
		}
		for _, d := range stats.Dispatches {
			if !d.At.IsZero() {
				offer(d.At.Local().Format("2006-01-02"))
			}
		}
	}
	if comments, err := tracker.LoadComments(issue.FilePath); err == nil {
		for _, c := range comments {
			offer(tracker.CommentDay(c.Date))
		}
	}
	for _, m := range decisionDayRe.FindAllStringSubmatch(issue.BodyRaw, -1) {
		offer(m[1])
	}
	if day == "" {
		if info, err := os.Stat(issue.FilePath); err == nil {
			offer(info.ModTime().Format("2006-01-02"))
		} else {
			offer(env.Now.Format("2006-01-02"))
		}
	}
	return day
}

// checkWorkflow reports WorkflowConfig.Lint: the types:, workbench:, ci:
// and health: warnings process workflow prints.
func checkWorkflow(env *Env) []Finding {
	var out []Finding
	for _, msg := range env.Workflow.Lint() {
		out = append(out, Finding{Where: env.where(0), Message: msg})
	}
	return out
}
