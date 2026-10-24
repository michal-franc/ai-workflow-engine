package health

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/ci"
)

//go:embed review_prompt.md
var reviewPrompt string

// ReviewSchema is the JSON schema of the file issue-cli health review record
// takes (issue-cli health review --schema).
const ReviewSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "Health review",
  "type": "object",
  "required": ["score", "assessment", "recommendations"],
  "properties": {
    "score": {"type": "integer", "minimum": 0, "maximum": 100},
    "assessment": {"type": "string", "description": "60–200 words"},
    "model": {"type": "string", "description": "the model that reviewed, e.g. claude-opus-5-5"},
    "recommendations": {
      "type": "array", "minItems": 1, "maxItems": 6,
      "items": {
        "type": "object",
        "required": ["id", "kind", "title", "evidence", "effort", "expected_effect"],
        "properties": {
          "id": {"type": "string", "pattern": "^R[1-9][0-9]?$"},
          "kind": {"enum": ["remove_tests", "merge_tests", "speed_up_tests", "add_e2e", "split_file", "simplify", "fix_flaky", "config", "other"]},
          "title": {"type": "string", "maxLength": 80},
          "evidence": {
            "type": "array", "minItems": 1,
            "items": {
              "type": "object", "required": ["shows"],
              "properties": {
                "metric": {"type": "string", "description": "a row id from the evidence pack, e.g. tests.wall_seconds{kind=unit}"},
                "path": {"type": "string", "description": "a repo path that exists at the main head"},
                "lines": {"type": "string", "pattern": "^[1-9][0-9]*(-[1-9][0-9]*)?$"},
                "shows": {"type": "string"}
              }
            }
          },
          "effort": {"enum": ["S", "M", "L"]},
          "expected_effect": {"type": "string"}
        }
      }
    }
  }
}`

// ReviewInput is what a review brief is built from.
type ReviewInput struct {
	Env       Env
	Snap      *Snapshot // the newest snapshot
	RetrosDir string
}

// Review is a prepared review: the brief to hand an agent and the evidence
// pack it points at.
type Review struct {
	Brief string
	Pack  string // path of evidence.md
}

// ReviewsDir holds the evidence packs, one folder per review.
func ReviewsDir(root string) string { return filepath.Join(Dir(root), "reviews") }

// PrepareReview writes the evidence pack for in and returns the brief.
func PrepareReview(in ReviewInput) (*Review, error) {
	if in.Snap == nil {
		return nil, fmt.Errorf("no health snapshot yet: run issue-cli health snapshot first")
	}
	dir := filepath.Join(ReviewsDir(in.Env.Root), in.Env.Now.Local().Format(fileLayout))
	if err := ensureDir(in.Env.Root); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	pack := filepath.Join(dir, "evidence.md")
	if err := os.WriteFile(pack, []byte(EvidencePack(in)), 0o644); err != nil {
		return nil, err
	}
	slug := ""
	if in.Env.Project != nil {
		slug = in.Env.Project.Slug
	}
	derived := "none yet"
	if v, ok := in.Snap.Value("score.derived", nil); ok {
		derived = fmt.Sprintf("%.0f", v)
	}
	brief := strings.NewReplacer(
		"{{project}}", slug, "{{slug}}", slug, "{{languages}}", codeLanguages(in.Snap),
		"{{pack}}", pack, "{{root}}", in.Env.Root, "{{derived}}", derived,
	).Replace(reviewPrompt)
	return &Review{Brief: brief, Pack: pack}, nil
}

// codeLanguages names the biggest code languages of the snapshot.
func codeLanguages(s *Snapshot) string {
	type lang struct {
		name  string
		lines float64
	}
	var ls []lang
	for _, r := range s.Rows("codebase.lines") {
		name, ok := r.Labels["lang"]
		if !ok || len(r.Labels) != 1 {
			continue
		}
		if l := classifyName(name); l == KindCode {
			ls = append(ls, lang{name, r.Value})
		}
	}
	sort.Slice(ls, func(i, j int) bool { return ls[i].lines > ls[j].lines })
	var names []string
	for i, l := range ls {
		if i == 3 {
			break
		}
		names = append(names, l.name)
	}
	if len(names) == 0 {
		return "unknown languages"
	}
	return strings.Join(names, ", ")
}

// classifyName is the kind of a language name from the languages table.
func classifyName(name string) string {
	for _, l := range languages {
		if l.name == name {
			return l.kind
		}
	}
	for _, l := range specialNames {
		if l.name == name {
			return l.kind
		}
	}
	return KindOther
}

// EvidencePack is the markdown the review agent reads first. Every number
// carries its row id in backticks, so a recommendation can cite it.
func EvidencePack(in ReviewInput) string {
	s, root, now := in.Snap, in.Env.Root, in.Env.Now
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }
	id := func(r Row) string { return "`" + r.ID + "`" }
	slug := ""
	if in.Env.Project != nil {
		slug = in.Env.Project.Slug
	}
	w("# Health evidence: %s, snapshot %s (%s at %s)\n\n", slug, s.Run.Date, s.Run.Branch, s.Run.Commit)
	w("Cite rows by the id in backticks. Values are from this snapshot unless a change is shown.\n\n")

	w("## Derived score\n\n")
	if v, ok := s.Value("score.derived", nil); ok {
		w("- `score.derived` = %.0f", v)
		for _, d := range []int{7, 30, 90} {
			if then, ok := ScoreAt(root, now.AddDate(0, 0, -d)); ok {
				w(" · %d d ago %.0f", d, then)
			}
		}
		w("\n")
	} else {
		w("- no total (under half the weight had data)\n")
	}
	cov, _ := s.Value("score.coverage", nil)
	w("- `score.coverage` = %.2f\n\n| component | measured | sub-score | weight |\n|:--|--:|--:|--:|\n", cov)
	for _, c := range Components(in.Env.Config) {
		l := map[string]string{"component": c.Name}
		weight, ok := s.Value("score.component.weight", l)
		if !ok {
			continue
		}
		sub, has := s.Value("score.component", l)
		if !has {
			w("| %s | no data: %s | – | %.0f |\n", c.Name, c.Missing, weight)
			continue
		}
		val, _ := s.Value("score.component.value", l)
		w("| %s (%s) | %s | %.0f | %.0f |\n", c.Name, c.Help, FormatValue(c.Unit, val), sub, weight)
	}

	w("\n## Tests\n\n")
	past := Before(root, now.AddDate(0, 0, -30))
	for _, kind := range TestKinds {
		k := map[string]string{"kind": kind}
		n, ok := s.Value("tests.count", map[string]string{"kind": kind, "result": "all"})
		if !ok {
			w("### %s\n\nNo results from local CI for %s tests.\n\n", kind, kind)
			continue
		}
		wall, _ := s.Value("tests.wall_seconds", k)
		mean, _ := s.Value("tests.per_test_ms", map[string]string{"kind": kind, "stat": "mean"})
		p90, _ := s.Value("tests.per_test_ms", map[string]string{"kind": kind, "stat": "p90"})
		age, _ := s.Value("tests.run_age_days", k)
		w("### %s\n\n- `tests.count{kind=%s,result=all}` = %.0f, `tests.wall_seconds{kind=%s}` = %.1f s, per test mean %.1f ms / p90 %.1f ms (run %.1f days old)\n", kind, kind, n, kind, wall, mean, p90, age)
		if past != nil {
			pn, ok1 := past.Value("tests.count", map[string]string{"kind": kind, "result": "all"})
			pw, ok2 := past.Value("tests.wall_seconds", k)
			if ok1 && ok2 {
				w("- 30 days ago (%s): %.0f tests, %.1f s\n", past.Run.Date, pn, pw)
			}
		}
		if rows := s.Rows("tests.part.seconds"); len(rows) > 0 {
			w("- slowest parts (the wall clock's critical path):\n")
			for _, r := range rows {
				if r.Labels["kind"] == kind {
					w("  - %s %.1f s\n", id(r), r.Value)
				}
			}
		}
		w("- slowest tests:\n")
		for _, r := range s.Rows("tests.slowest.seconds") {
			if r.Labels["kind"] == kind {
				w("  - %s %.2f s\n", id(r), r.Value)
			}
		}
		var areas []string
		for _, r := range s.Rows("tests.count_by_area") {
			if r.Labels["kind"] != kind {
				continue
			}
			lines, _ := s.Value("codebase.lines", map[string]string{"area": r.Labels["area"]})
			areas = append(areas, fmt.Sprintf("  - %s %.0f tests, `codebase.lines{area=%s}` %.0f lines", id(r), r.Value, r.Labels["area"], lines))
		}
		if len(areas) > 0 {
			w("- tests per area next to the area's lines (find the least-tested):\n%s\n", strings.Join(areas, "\n"))
		}
		if _, sum, ok := ci.LatestResults(root, kind, now); ok && sum.List != "" {
			w("- every test with its time: %s (JSON: name, area, part, s, outcome)\n", filepath.Join(root, sum.List))
		}
		w("\n")
	}

	w("## End-to-end coverage\n\n")
	if rows := s.Rows("tests.e2e.sessions"); len(rows) > 0 {
		for _, r := range rows {
			w("- %s = %.0f\n", id(r), r.Value)
		}
		if v, ok := s.Value("tests.e2e.coverage", nil); ok {
			w("- `tests.e2e.coverage` = %.2f of the issues done in 90 days are in a system with a session\n", v)
		}
		for _, r := range s.Rows("tests.e2e.uncovered") {
			w("- %s: %.0f issues done in 90 days, no session\n", id(r), r.Value)
		}
	} else {
		w("No playtest sessions found (health.e2e_sessions).\n")
	}

	w("\n## Shape\n\n")
	commits := map[string]float64{}
	for _, r := range s.Rows("shape.churn.commits") {
		commits[r.Labels["path"]] = r.Value
	}
	if big := s.Rows("codebase.big_file.lines"); len(big) > 0 {
		w("Biggest files (lines; commits in 30 days when among the most churned):\n")
		for _, r := range big {
			if c, ok := commits[r.Labels["path"]]; ok {
				w("- %s %.0f lines, %.0f commits\n", id(r), r.Value, c)
			} else {
				w("- %s %.0f lines\n", id(r), r.Value)
			}
		}
	}
	for _, sec := range []struct{ title, metric string }{
		{"Hotspots (big and churned: lines × commits)", "shape.hotspot"},
		{"Longest functions", "shape.long_func.lines"},
		{"Most-churned source files (lines changed, 30 days)", "shape.churn.lines"},
	} {
		rows := s.Rows(sec.metric)
		if len(rows) == 0 {
			continue
		}
		w("\n%s:\n", sec.title)
		for i, r := range rows {
			if i == 10 {
				break
			}
			w("- %s %.0f\n", id(r), r.Value)
		}
	}
	for _, m := range []string{"shape.func_lines", "shape.long_funcs", "shape.long_funcs.share", "shape.big_lines.share", "shape.churn.big_share", "codebase.test_ratio", "codebase.growth.lines"} {
		for _, r := range s.Rows(m) {
			w("- %s %v\n", id(r), r.Value)
		}
	}

	w("\n## Recent retros\n\n")
	w("%s", retroLines(in.RetrosDir))

	w("\n## The previous review\n\n")
	if o, _ := LatestOpinion(root, now); o != nil {
		w("%s: score %d (derived then: %s)\n\n", o.Date, o.Score, derivedText(o.Derived))
		for _, r := range o.Recommendations {
			st := r.State
			if st == "" {
				st = StateProposed
			}
			w("- %s [%s, %s] %s", r.ID, r.Kind, st, r.Title)
			if r.Issue != "" {
				w(" → %s", r.Issue)
			}
			w("\n")
			for _, e := range r.Evidence {
				if e.Metric == "" {
					continue
				}
				if v, ok := s.Value(rowNameLabels(e.Metric)); ok {
					w("  - %s now %v (%s)\n", "`"+e.Metric+"`", v, e.Shows)
				}
			}
		}
	} else {
		w("None: this is the first review.\n")
	}
	return b.String()
}

func derivedText(d *float64) string {
	if d == nil {
		return "none"
	}
	return fmt.Sprintf("%.0f", *d)
}

// rowNameLabels splits a row id back into name and labels.
func rowNameLabels(id string) (string, map[string]string) {
	name, rest, ok := strings.Cut(id, "{")
	if !ok {
		return id, nil
	}
	labels := map[string]string{}
	for _, kv := range strings.Split(strings.TrimSuffix(rest, "}"), ",") {
		k, v, _ := strings.Cut(kv, "=")
		labels[k] = v
	}
	return name, labels
}

var retroMatch = regexp.MustCompile(`(?i)\b(tests?|slow|flak|big file|too long|context|runtime|timeout)`)

// retroLines lists the newest retros' file names and the lines that mention
// tests, speed or size (at most 3 per retro).
func retroLines(dir string) string {
	if dir == "" {
		return "No retros folder.\n"
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	if len(paths) > 10 {
		paths = paths[:10]
	}
	if len(paths) == 0 {
		return "None.\n"
	}
	var b strings.Builder
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", filepath.Base(p))
		n := 0
		for _, l := range strings.Split(string(data), "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "---") || !retroMatch.MatchString(l) {
				continue
			}
			if len(l) > 240 {
				l = l[:240] + "…"
			}
			fmt.Fprintf(&b, "  - %s\n", l)
			if n++; n == 3 {
				break
			}
		}
	}
	return b.String()
}

// FormatValue shows a component's value in its unit.
func FormatValue(unit string, v float64) string {
	switch unit {
	case UnitSeconds:
		if v >= 120 {
			return fmt.Sprintf("%.1f min", v/60)
		}
		return fmt.Sprintf("%.1f s", v)
	case UnitShare:
		return fmt.Sprintf("%.1f%%", v*100)
	case "change":
		return fmt.Sprintf("%+.0f%%", v*100)
	case "delta":
		return fmt.Sprintf("%+.3f", v)
	}
	return fmt.Sprintf("%v", v)
}

// NewOpinion stamps a reviewed file before it is stored: the date, the
// snapshot it read, its derived score, and every recommendation proposed.
func NewOpinion(o *Opinion, snap *Snapshot, now time.Time, reviewer string) {
	o.Date = now.Local().Format(dateLayout)
	o.Reviewer = reviewer
	if snap != nil {
		o.Snapshot = snap.Run.Date
		if v, ok := snap.Value("score.derived", nil); ok {
			o.Derived = &v
		}
	}
	for i := range o.Recommendations {
		o.Recommendations[i].State = StateProposed
	}
}
