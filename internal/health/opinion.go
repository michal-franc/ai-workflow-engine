package health

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The agent opinion: an agent reviews the project on demand (issue-cli
// health review), and its score, assessment and recommendations are stored
// in .health/opinions/<ts>.json. The opinion family puts the newest one in
// every snapshot, so the score trends (as a step between reviews). See
// docs/workbench-files.md, "The agent opinion".
func init() {
	Register("opinion", opinionFamily)
	Describe("opinion", "the review: issue-cli health review")
}

// Recommendation kinds and states.
var (
	RecommendationKinds = []string{"remove_tests", "merge_tests", "speed_up_tests", "add_e2e", "split_file", "simplify", "fix_flaky", "config", "other"}
	Efforts             = []string{"S", "M", "L"}
)

const (
	StateProposed  = "proposed"
	StateFiled     = "filed"
	StateDismissed = "dismissed"
)

// ReviewStaleDays is when an opinion is old enough to nudge for a new review.
const ReviewStaleDays = 7

// Opinion is one review.
type Opinion struct {
	Date            string           `json:"date"` // local, 2006-01-02T15:04
	Score           int              `json:"score"`
	Assessment      string           `json:"assessment"`
	Recommendations []Recommendation `json:"recommendations"`
	Reviewer        string           `json:"reviewer,omitempty"`
	Model           string           `json:"model,omitempty"`
	// Snapshot is the date of the snapshot the review read, Derived its
	// derived score then.
	Snapshot string   `json:"snapshot,omitempty"`
	Derived  *float64 `json:"derived,omitempty"`
}

// Recommendation is one proposed change, with its evidence.
type Recommendation struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	Title          string     `json:"title"`
	Evidence       []Evidence `json:"evidence"`
	Effort         string     `json:"effort"`
	ExpectedEffect string     `json:"expected_effect"`
	State          string     `json:"state,omitempty"`
	Issue          string     `json:"issue,omitempty"`  // the slug, once filed
	Reason         string     `json:"reason,omitempty"` // why dismissed
	Decided        string     `json:"decided,omitempty"`
}

// Evidence backs a recommendation: a metric id from the snapshot or a repo
// path (with an optional line range), and what it shows.
type Evidence struct {
	Metric string `json:"metric,omitempty"`
	Path   string `json:"path,omitempty"`
	Lines  string `json:"lines,omitempty"` // 120-180
	Shows  string `json:"shows"`
}

// OpinionsDir holds the opinion files.
func OpinionsDir(root string) string { return filepath.Join(Dir(root), "opinions") }

// Proposed counts the recommendations still waiting for the owner.
func (o *Opinion) Proposed() int {
	n := 0
	for _, r := range o.Recommendations {
		if r.State == "" || r.State == StateProposed {
			n++
		}
	}
	return n
}

// Time is the opinion's date.
func (o *Opinion) Time() time.Time {
	t, _ := time.ParseInLocation(dateLayout, o.Date, time.Local)
	return t
}

// AgeDays is the opinion's age at now, in days (one decimal).
func (o *Opinion) AgeDays(now time.Time) float64 {
	return math.Round(now.Sub(o.Time()).Hours()/24*10) / 10
}

// Recommendation finds a recommendation by id (R2, r2).
func (o *Opinion) Recommendation(id string) *Recommendation {
	for i := range o.Recommendations {
		if strings.EqualFold(o.Recommendations[i].ID, id) {
			return &o.Recommendations[i]
		}
	}
	return nil
}

// LatestOpinion is the newest opinion at or before t (zero: any) and its
// path; nil when there is none.
func LatestOpinion(root string, t time.Time) (*Opinion, string) {
	paths, _ := filepath.Glob(filepath.Join(OpinionsDir(root), "*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	for _, p := range paths {
		o, err := ReadOpinion(p)
		if err != nil {
			continue
		}
		if !t.IsZero() && o.Time().After(t) {
			continue
		}
		return o, p
	}
	return nil, ""
}

// ReadOpinion parses one opinion file.
func ReadOpinion(path string) (*Opinion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var o Opinion
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &o, nil
}

// WriteOpinion stores o as .health/opinions/<date>.json and returns the path.
func WriteOpinion(root string, o *Opinion) (string, error) {
	if err := ensureDir(root); err != nil {
		return "", err
	}
	if err := os.MkdirAll(OpinionsDir(root), 0o755); err != nil {
		return "", err
	}
	t := o.Time()
	if t.IsZero() {
		return "", fmt.Errorf("opinion date %q is not 2006-01-02T15:04", o.Date)
	}
	path := filepath.Join(OpinionsDir(root), t.Format(fileLayout)+".json")
	return path, SaveOpinion(path, o)
}

// SaveOpinion rewrites an opinion file (a recommendation filed or dismissed).
func SaveOpinion(path string, o *Opinion) error {
	return writeJSONFile(path, o)
}

var (
	recIDRe = regexp.MustCompile(`^R[1-9][0-9]?$`)
	linesRe = regexp.MustCompile(`^[1-9][0-9]*(-[1-9][0-9]*)?$`)
	wordRe  = regexp.MustCompile(`\S+`)
)

// Review limits.
const (
	maxTitle = 80
	minWords = 60
	maxWords = 200
	maxRecs  = 6
)

// Validate checks a review before it is stored: the shape, and that every
// piece of evidence points at something real (a metric id in snap, a path
// that exists at the main head). Each problem names its field and the fix.
func (o *Opinion) Validate(snap *Snapshot, pathExists func(string) bool) []string {
	var p []string
	add := func(f string, a ...any) { p = append(p, fmt.Sprintf(f, a...)) }
	if o.Score < 0 || o.Score > 100 {
		add("score: %d is outside 0–100", o.Score)
	}
	if n := len(wordRe.FindAllString(o.Assessment, -1)); n < minWords || n > maxWords {
		add("assessment: %d words; write %d–%d: what is getting better, what is getting worse, the one thing to fix first", n, minWords, maxWords)
	}
	if len(o.Recommendations) == 0 {
		add("recommendations: give at least one precise change with evidence")
	}
	if len(o.Recommendations) > maxRecs {
		add("recommendations: %d; keep the %d with the most value", len(o.Recommendations), maxRecs)
	}
	ids, titles := map[string]bool{}, map[string]bool{}
	for i, r := range o.Recommendations {
		at := fmt.Sprintf("recommendations[%d]", i)
		if !recIDRe.MatchString(r.ID) {
			add("%s.id %q: use R1, R2, ... in order", at, r.ID)
		} else if ids[r.ID] {
			add("%s.id %q is used twice", at, r.ID)
		}
		ids[r.ID] = true
		if !containsStr(RecommendationKinds, r.Kind) {
			add("%s.kind %q: one of %s", at, r.Kind, strings.Join(RecommendationKinds, ", "))
		}
		t := strings.TrimSpace(r.Title)
		switch {
		case t == "":
			add("%s.title is empty", at)
		case len([]rune(t)) > maxTitle:
			add("%s.title is %d characters; keep it under %d", at, len([]rune(t)), maxTitle)
		case titles[strings.ToLower(t)]:
			add("%s.title %q repeats another recommendation", at, t)
		}
		titles[strings.ToLower(t)] = true
		if !containsStr(Efforts, r.Effort) {
			add("%s.effort %q: S (≤ ½ day), M (≤ 2 days) or L", at, r.Effort)
		}
		if strings.TrimSpace(r.ExpectedEffect) == "" {
			add("%s.expected_effect is empty: which metric moves, and by roughly how much", at)
		}
		if len(r.Evidence) == 0 {
			add("%s.evidence is empty: cite a metric id from the snapshot or a repo path", at)
		}
		for j, e := range r.Evidence {
			eat := fmt.Sprintf("%s.evidence[%d]", at, j)
			switch {
			case e.Metric == "" && e.Path == "":
				add("%s: give a metric id (tests.wall_seconds{kind=unit}) or a path", eat)
			case e.Metric != "" && snap != nil && !snap.HasRow(e.Metric):
				add("%s.metric %q is not in the snapshot the review read; copy an id from the evidence pack", eat, e.Metric)
			case e.Path != "" && pathExists != nil && !pathExists(e.Path):
				add("%s.path %q does not exist at the main head", eat, e.Path)
			}
			if e.Lines != "" && !linesRe.MatchString(e.Lines) {
				add("%s.lines %q: a line or range such as 120-180", eat, e.Lines)
			}
			if strings.TrimSpace(e.Shows) == "" {
				add("%s.shows is empty: say what this evidence shows", eat)
			}
		}
		if r.State != "" && r.State != StateProposed {
			add("%s.state: leave it out; the owner files or dismisses", at)
		}
	}
	return p
}

// PathAt reports whether path exists in commit's tree (a file or a folder).
func PathAt(root, commit, path string) bool {
	path = strings.TrimPrefix(strings.TrimSpace(path), "./")
	if path == "" || commit == "" {
		return false
	}
	_, err := gitOut(context.Background(), root, "cat-file", "-e", commit+":"+strings.TrimSuffix(path, "/"))
	return err == nil
}

// HasRow reports whether the snapshot has a row with this id.
func (s *Snapshot) HasRow(id string) bool {
	for _, r := range s.Metrics {
		if r.ID == id {
			return true
		}
	}
	return false
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func opinionFamily(_ context.Context, env Env) ([]Metric, error) {
	if env.Backfill {
		return nil, ErrNoPast
	}
	o, _ := LatestOpinion(env.Root, env.Now)
	if o == nil {
		return nil, nil
	}
	out := []Metric{
		{Name: "opinion.score", Value: float64(o.Score), Help: "The newest agent review's score, 0–100 (held until the next review)"},
		{Name: "opinion.age_days", Value: o.AgeDays(env.Now), Help: "Age of the newest review, in days"},
	}
	counts := map[[2]string]int{}
	for _, r := range o.Recommendations {
		st := r.State
		if st == "" {
			st = StateProposed
		}
		counts[[2]string{r.Kind, st}]++
	}
	keys := make([][2]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+keys[i][1] < keys[j][0]+keys[j][1] })
	for _, k := range keys {
		out = append(out, Metric{Name: "opinion.recommendations", Labels: map[string]string{"kind": k[0], "state": k[1]}, Value: float64(counts[k]), Unit: UnitCount, Help: "Recommendations of the newest review, by kind and state"})
	}
	return out, nil
}
