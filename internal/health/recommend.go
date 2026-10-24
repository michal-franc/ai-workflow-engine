package health

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// ErrNeedsOwner is what filing or dismissing a recommendation answers without
// the owner's words: only the owner turns a proposal into work.
var ErrNeedsOwner = errors.New("needs the owner's approval: ask them, then pass --quote \"<their words>\"")

// Decision is who approved a filing or a dismissal, and how.
type Decision struct {
	How   string // "lead chat", "viewer", ...
	Quote string // the owner's words
}

// FileRecommendation turns a proposed recommendation of the newest opinion
// into an idea issue, records the owner's decision on it and marks the
// recommendation filed. It returns the new issue's slug.
func FileRecommendation(proj *tracker.Project, wf *tracker.WorkflowConfig, root, id string, d Decision, now time.Time) (string, error) {
	if strings.TrimSpace(d.Quote) == "" {
		return "", ErrNeedsOwner
	}
	o, path := LatestOpinion(root, time.Time{})
	if o == nil {
		return "", errors.New("no health review yet: issue-cli health review")
	}
	r := o.Recommendation(id)
	if r == nil {
		return "", fmt.Errorf("the review of %s has no %s (it has %s)", o.Date, id, recIDs(o))
	}
	if r.State == StateFiled {
		return "", fmt.Errorf("%s is already filed as %s", r.ID, r.Issue)
	}
	if r.State == StateDismissed {
		return "", fmt.Errorf("%s was dismissed (%s); a new review can propose it again", r.ID, valueOr(r.Reason, "no reason given"))
	}
	typ := ""
	if wf.HasTypes() {
		want := "tweak"
		if r.Kind == "add_e2e" {
			want = "feature"
		}
		if _, ok := wf.Types[want]; ok {
			typ = want
		}
		typ, _ = wf.ResolveType(typ)
	}
	status := "idea"
	if order := wf.ForType(typ).GetStatusOrder(); len(order) > 0 && order[0] != "none" {
		status = order[0]
	}
	system := recSystem(r)
	if system != "" {
		if _, err := os.Stat(filepath.Join(proj.IssueDir, system)); err != nil {
			system = "" // a system label the tracker has no folder for
		}
	}
	body := recBody(o, r)
	if tmpl := wf.ForType(typ).TemplateForStatus(status); tmpl != "" {
		body += "\n\n" + tmpl
	}
	file, slug, err := tracker.CreateIssueFileOpts(proj.IssueDir, tracker.CreateIssueOpts{
		Title: r.Title, Status: status, System: system, Type: typ, Body: body, Labels: []string{"health"},
	})
	if err != nil {
		return "", err
	}
	if _, err := tracker.AddDecision(file, slug, tracker.NewDecision{
		Text:  fmt.Sprintf("File the health review's %s (%s, %s) as an issue", r.ID, o.Date, r.Kind),
		How:   valueOr(d.How, "owner"),
		Quote: d.Quote,
	}, now); err != nil {
		return slug, fmt.Errorf("created %s but could not record the decision: %w", slug, err)
	}
	r.State, r.Issue, r.Decided = StateFiled, slug, now.Local().Format(dateLayout)
	return slug, SaveOpinion(path, o)
}

// DismissRecommendation marks a proposed recommendation dismissed.
func DismissRecommendation(root, id, reason string, d Decision, now time.Time) (*Recommendation, error) {
	if strings.TrimSpace(d.Quote) == "" {
		return nil, ErrNeedsOwner
	}
	o, path := LatestOpinion(root, time.Time{})
	if o == nil {
		return nil, errors.New("no health review yet: issue-cli health review")
	}
	r := o.Recommendation(id)
	if r == nil {
		return nil, fmt.Errorf("the review of %s has no %s (it has %s)", o.Date, id, recIDs(o))
	}
	if r.State == StateFiled {
		return nil, fmt.Errorf("%s is already filed as %s; close that issue instead", r.ID, r.Issue)
	}
	r.State, r.Reason, r.Decided = StateDismissed, strings.TrimSpace(reason), now.Local().Format(dateLayout)
	return r, SaveOpinion(path, o)
}

func recIDs(o *Opinion) string {
	var ids []string
	for _, r := range o.Recommendations {
		ids = append(ids, r.ID)
	}
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

// recSystem is the system the evidence names (a system= label), if any.
func recSystem(r *Recommendation) string {
	for _, e := range r.Evidence {
		if _, labels := rowNameLabels(e.Metric); labels["system"] != "" && labels["system"] != "all" && labels["system"] != "none" {
			return labels["system"]
		}
	}
	return ""
}

func recBody(o *Opinion, r *Recommendation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Why\n\nFrom the health review of %s (opinion %d, derived %s), %s, %s:\n\n> %s\n\n### Evidence\n\n", o.Date, o.Score, derivedText(o.Derived), r.ID, r.Kind, r.Title)
	for _, e := range r.Evidence {
		ref := e.Path
		if e.Metric != "" {
			ref = "`" + e.Metric + "`"
		} else if e.Lines != "" {
			ref += ":" + e.Lines
		}
		fmt.Fprintf(&b, "- %s: %s\n", ref, e.Shows)
	}
	fmt.Fprintf(&b, "\n**Effort:** %s · **Expected:** %s\n\nFiled from the health review (the Health view, or issue-cli health review file).", r.Effort, r.ExpectedEffect)
	return b.String()
}

func valueOr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
