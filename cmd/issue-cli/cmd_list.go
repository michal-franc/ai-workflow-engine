package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var listCommand = &Command{
	Name:      "list",
	ShortHelp: "List issues with filters",
	LongHelp: `List issues. Filters: --status (open|closed|<name>), --system, --assignee,
--version, --sort score.`,
	Run: runList,
}

func init() {
	registerCommand(listCommand)
}

// listJSONIssue wraps tracker.Issue with scoring fields for `list --json`.
// Score and ScoreBreakdown are populated only when scoring is enabled in the
// workflow config; otherwise they marshal as null so consumers can treat
// missing scoring uniformly.
type listJSONIssue struct {
	*tracker.Issue
	Score          *float64                `json:"Score"`
	ScoreBreakdown *tracker.ScoreBreakdown `json:"ScoreBreakdown"`
}

func runList(ctx *Context, args []string) error {
	fs := newFlagSet("list", ctx)
	statusFlag := fs.String("status", "", "filter by status (open|closed|<name>)")
	systemFlag := fs.String("system", "", "filter by system")
	categoryFlag := fs.String("category", "", "alias for --system")
	assigneeFlag := fs.String("assignee", "", "filter by assignee")
	versionFlag := fs.String("version", "", "filter by version (defaults to project version)")
	sortFlag := fs.String("sort", "", "sort key (score)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	proj := ctx.Project
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return fmt.Errorf("cannot load issues: %w", err)
	}

	status := *statusFlag
	system := *systemFlag
	if system == "" {
		system = *categoryFlag
	}
	assignee := *assigneeFlag
	version := *versionFlag
	if version == "" && proj.Version != "" {
		version = proj.Version
	}
	sortBy := *sortFlag

	var filtered []*tracker.Issue
	for _, issue := range issues {
		if status != "" {
			switch strings.ToLower(status) {
			case "open":
				if issue.Status == "done" {
					continue
				}
			case "closed":
				if issue.Status != "done" {
					continue
				}
			default:
				if !strings.EqualFold(issue.Status, status) {
					continue
				}
			}
		}
		if version != "" && issue.Version != version {
			continue
		}
		if system != "" && !strings.EqualFold(issue.System, system) {
			continue
		}
		if assignee != "" && issue.Assignee != assignee {
			continue
		}
		filtered = append(filtered, issue)
	}

	wf := proj.LoadWorkflow()
	scoringOn := wf != nil && wf.Scoring.Enabled

	if scoringOn {
		applySort := strings.ToLower(strings.TrimSpace(sortBy))
		if applySort == "" && strings.EqualFold(wf.Scoring.DefaultSort, "score_desc") {
			applySort = "score"
		}
		if applySort == "score" || applySort == "score_desc" {
			now := ctx.Now()
			scores := make(map[*tracker.Issue]float64, len(filtered))
			for _, iss := range filtered {
				if bd := tracker.ComputeScore(iss, &wf.Scoring, now); bd != nil {
					scores[iss] = bd.Total
				}
			}
			sort.SliceStable(filtered, func(i, j int) bool {
				return scores[filtered[i]] > scores[filtered[j]]
			})
		}
	}

	if ctx.JSONOutput {
		entries := make([]listJSONIssue, 0, len(filtered))
		now := ctx.Now()
		for _, issue := range filtered {
			entry := listJSONIssue{Issue: issue}
			if scoringOn {
				if bd := tracker.ComputeScore(issue, &wf.Scoring, now); bd != nil {
					total := bd.Total
					entry.Score = &total
					entry.ScoreBreakdown = bd
				}
			}
			entries = append(entries, entry)
		}
		return writeJSON(ctx.Stdout, entries)
	}

	for _, issue := range filtered {
		a := ""
		if issue.Assignee != "" {
			a = " claimed by " + issue.Assignee
		}
		fmt.Fprintf(ctx.Stdout, "  [%-13s] %-45s %-10s%s\n", issue.Status, issue.Slug, issue.System, a)
	}
	fmt.Fprintf(ctx.Stdout, "\n%d issues\n", len(filtered))
	return nil
}
