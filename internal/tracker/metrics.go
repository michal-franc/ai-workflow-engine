package tracker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Guard metrics: target bands and measured values the project writes as JSON
// (docs/workbench-files.md). The viewer only reads them: workbench.metrics
// points at a probe-results glob (the runs) and an optional targets file
// (bands kept apart from results). Metrics are matched by id everywhere: the
// targets file, the master run and each open branch's run.

const (
	// MaxWorkbenchFile caps every metric or knob file the viewer reads.
	MaxWorkbenchFile = 2 << 20
	// MaxWorkbenchRows caps the rows read from one file.
	MaxWorkbenchRows = 2000
)

// Verdicts, normalised from the file's own words.
const (
	VerdictOK         = "ok"
	VerdictNear       = "near"
	VerdictOff        = "off"
	VerdictUnmeasured = "unmeasured"
)

// MetricBand is a target: at least Min, at most Max (below Max when
// MaxExclusive). Unit only affects formatting.
type MetricBand struct {
	Min          *float64 `json:"min"`
	Max          *float64 `json:"max"`
	Unit         string   `json:"unit,omitempty"`
	MaxExclusive bool     `json:"max_exclusive,omitempty"`
}

// MetricRun is the header of one results file.
type MetricRun struct {
	File    string `json:"file"`
	Date    string `json:"date,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Command string `json:"command,omitempty"`
	N       int    `json:"n,omitempty"`
	Dirty   bool   `json:"dirty,omitempty"`
	// Branch runs only.
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Source   string `json:"source,omitempty"` // "worktree" or "commit"
	// Behind counts branch commits after the run's commit: 0 at the head,
	// -1 when the run's commit is not on the branch.
	Behind int `json:"behind,omitempty"`
}

// MetricValue is one measured value.
type MetricValue struct {
	Value   float64 `json:"value"`
	Raw     string  `json:"raw"` // as written, for values that are not numbers
	N       *int    `json:"n,omitempty"`
	Verdict string  `json:"verdict,omitempty"`
}

// BranchValue is an open branch's value for a metric.
type BranchValue struct {
	MetricValue
	Branch   string      `json:"branch"`
	Worktree string      `json:"worktree,omitempty"`
	Source   string      `json:"source"`
	Behind   int         `json:"behind,omitempty"`
	Band     *MetricBand `json:"band,omitempty"`    // set when it differs from master's
	Missing  bool        `json:"missing,omitempty"` // the branch run has no row for this id
}

// GuardMetric is one metric with its master value and branch values.
type GuardMetric struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Group      string        `json:"group,omitempty"`
	Probe      string        `json:"probe,omitempty"`
	Source     string        `json:"source,omitempty"`
	Band       *MetricBand   `json:"band,omitempty"`
	Master     *MetricValue  `json:"master,omitempty"`
	Branches   []BranchValue `json:"branches,omitempty"`
	Systems    []string      `json:"systems,omitempty"`
	BranchOnly bool          `json:"branch_only,omitempty"`
}

// Verdict is the master verdict, or the first branch's for a branch-only
// metric.
func (m GuardMetric) Verdict() string {
	if m.Master != nil {
		return m.Master.Verdict
	}
	if len(m.Branches) > 0 {
		return m.Branches[0].Verdict
	}
	return VerdictUnmeasured
}

// MetricsReport is everything the Guard metrics views show.
type MetricsReport struct {
	Configured bool          `json:"configured"`
	Run        *MetricRun    `json:"run,omitempty"`
	Metrics    []GuardMetric `json:"metrics"`
	BranchRuns []MetricRun   `json:"branch_runs,omitempty"`
	Problems   []string      `json:"problems,omitempty"`
}

// Counts returns the master ok / near / off counts.
func (r MetricsReport) Counts() (ok, near, off int) {
	for _, m := range r.Metrics {
		if m.BranchOnly || m.Master == nil {
			continue
		}
		switch m.Master.Verdict {
		case VerdictOK:
			ok++
		case VerdictNear:
			near++
		case VerdictOff:
			off++
		}
	}
	return
}

// Groups lists the metric groups in first-seen order ("" last).
func (r MetricsReport) Groups() []string {
	var out []string
	seen := map[string]bool{}
	blank := false
	for _, m := range r.Metrics {
		if m.Group == "" {
			blank = true
			continue
		}
		if !seen[m.Group] {
			seen[m.Group] = true
			out = append(out, m.Group)
		}
	}
	if blank {
		out = append(out, "")
	}
	return out
}

// metricFile is the on-disk shape. Top-level date/commit/n are accepted as a
// fallback for `run` (rust-manager's snapshot.json).
type metricFile struct {
	Run     *MetricRun  `json:"run"`
	Date    string      `json:"date"`
	Commit  string      `json:"commit"`
	N       int         `json:"n"`
	Dirty   bool        `json:"dirty"`
	Metrics []metricRow `json:"metrics"`
}

type metricRow struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Group   string          `json:"group"`
	Band    *MetricBand     `json:"band"`
	Value   json.RawMessage `json:"value"`
	N       *int            `json:"n"`
	Verdict string          `json:"verdict"`
	Probe   string          `json:"probe"`
	Source  string          `json:"source"`
}

type parsedRun struct {
	run  MetricRun
	rows []metricRow
	raw  []byte
}

// LoadMetrics reads the master run (the newest file matching
// workbench.metrics.probe_results under root), merges the targets file, and
// adds each open branch's run. Problems never stop the load: what can be read
// is returned.
func LoadMetrics(root string, wb WorkbenchConfig, branches []OpenBranch) MetricsReport {
	rep := MetricsReport{Configured: wb.Metrics != (WorkbenchMetrics{})}
	if !rep.Configured {
		return rep
	}
	var targets []metricRow
	if wb.Metrics.Targets != "" {
		data, err := readProjectFile(root, wb.Metrics.Targets)
		if err == nil {
			var f metricFile
			if err = decodeJSON(data, &f); err == nil {
				targets = capRows(f.Metrics)
			}
		}
		if err != nil {
			rep.Problems = append(rep.Problems, fmt.Sprintf("%s: %v", wb.Metrics.Targets, err))
		}
	}

	var master *parsedRun
	if wb.Metrics.ProbeResults != "" {
		pr, err := newestRun(root, wb.Metrics.ProbeResults, os.ReadFile)
		if err != nil {
			rep.Problems = append(rep.Problems, fmt.Sprintf("%s: %v", wb.Metrics.ProbeResults, err))
		} else {
			master = pr
			rep.Run = &pr.run
		}
	}

	byID := map[string]int{}
	add := func(m GuardMetric) *GuardMetric {
		byID[m.ID] = len(rep.Metrics)
		rep.Metrics = append(rep.Metrics, m)
		return &rep.Metrics[len(rep.Metrics)-1]
	}
	targetByID := map[string]metricRow{}
	for _, t := range targets {
		if t.ID != "" {
			targetByID[t.ID] = t
		}
	}
	if master != nil {
		for _, row := range master.rows {
			if row.ID == "" {
				continue
			}
			if _, dup := byID[row.ID]; dup {
				continue
			}
			m := metricFromRow(row, targetByID[row.ID])
			v := valueFromRow(row, m.Band)
			m.Master = &v
			add(m)
		}
	}
	for _, t := range targets {
		if _, ok := byID[t.ID]; ok || t.ID == "" {
			continue
		}
		m := metricFromRow(t, t)
		m.Master = &MetricValue{Raw: "", Verdict: VerdictUnmeasured}
		add(m)
	}

	missing := 0
	for _, m := range rep.Metrics {
		if m.Band == nil {
			missing++
		}
	}
	if missing > 0 && len(rep.Metrics) > 0 {
		src := wb.Metrics.ProbeResults
		if rep.Run != nil {
			src = rep.Run.File
		}
		rep.Problems = append(rep.Problems, fmt.Sprintf("%d of %d metrics have no band in %s: add band (and name) to each row, see docs/workbench-files.md", missing, len(rep.Metrics), src))
	}

	if wb.Metrics.ProbeResults != "" {
		for _, b := range branches {
			addBranchRun(&rep, root, wb.Metrics.ProbeResults, b, master, targetByID, byID)
		}
	}

	for i := range rep.Metrics {
		rep.Metrics[i].Systems = systemsListing(wb, rep.Metrics[i].ID, func(s WorkbenchSystem) []string { return s.Metrics }, func(f WorkbenchFamily) []string { return f.Metrics })
	}
	return rep
}

// addBranchRun reads b's run (its worktree's file, else the committed file)
// and attaches its values: by id, only where they differ from master.
func addBranchRun(rep *MetricsReport, root, glob string, b OpenBranch, master *parsedRun, targets map[string]metricRow, byID map[string]int) {
	var pr *parsedRun
	var err error
	source := "worktree"
	if b.Worktree != "" {
		pr, err = newestRun(b.Worktree, glob, os.ReadFile)
	} else {
		source = "commit"
		pr, err = committedRun(root, b, glob)
	}
	if err != nil || pr == nil {
		return // no run on this branch: shown as "—"
	}
	if master != nil && bytes.Equal(bytes.TrimSpace(pr.raw), bytes.TrimSpace(master.raw)) {
		return
	}
	run := pr.run
	run.Branch, run.Worktree, run.Source = b.Name, b.Worktree, source
	run.Behind = runBehind(root, run.Commit, b.Head)
	rep.BranchRuns = append(rep.BranchRuns, run)

	seen := map[string]bool{}
	for _, row := range pr.rows {
		if row.ID == "" || seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		i, ok := byID[row.ID]
		if !ok {
			m := metricFromRow(row, targets[row.ID])
			m.BranchOnly = true
			v := valueFromRow(row, m.Band)
			m.Branches = []BranchValue{{MetricValue: v, Branch: b.Name, Worktree: b.Worktree, Source: source, Behind: run.Behind}}
			byID[row.ID] = len(rep.Metrics)
			rep.Metrics = append(rep.Metrics, m)
			continue
		}
		m := &rep.Metrics[i]
		if m.BranchOnly {
			v := valueFromRow(row, m.Band)
			m.Branches = append(m.Branches, BranchValue{MetricValue: v, Branch: b.Name, Worktree: b.Worktree, Source: source, Behind: run.Behind})
			continue
		}
		band := m.Band
		var branchBand *MetricBand
		if row.Band != nil && targets[row.ID].Band == nil && !sameBand(row.Band, m.Band) {
			band, branchBand = row.Band, row.Band
		}
		v := valueFromRow(row, band)
		if m.Master != nil && branchBand == nil && v.Raw == m.Master.Raw && v.Verdict == m.Master.Verdict {
			continue // same as master
		}
		m.Branches = append(m.Branches, BranchValue{MetricValue: v, Branch: b.Name, Worktree: b.Worktree, Source: source, Behind: run.Behind, Band: branchBand})
	}
	// Master ids the branch run does not measure.
	if len(seen) > 0 {
		for i := range rep.Metrics {
			m := &rep.Metrics[i]
			if m.BranchOnly || seen[m.ID] || m.Master == nil || m.Master.Verdict == VerdictUnmeasured {
				continue
			}
			m.Branches = append(m.Branches, BranchValue{Branch: b.Name, Worktree: b.Worktree, Source: source, Behind: run.Behind, Missing: true})
		}
	}
}

func metricFromRow(row, target metricRow) GuardMetric {
	m := GuardMetric{ID: row.ID, Name: row.Name, Group: row.Group, Probe: row.Probe, Source: row.Source, Band: row.Band}
	if target.ID != "" {
		if target.Name != "" {
			m.Name = target.Name
		}
		if target.Group != "" {
			m.Group = target.Group
		}
		if target.Band != nil {
			m.Band = target.Band
		}
		if target.Source != "" {
			m.Source = target.Source
		}
		if m.Probe == "" {
			m.Probe = target.Probe
		}
	}
	if m.Name == "" {
		m.Name = m.ID
	}
	return m
}

func valueFromRow(row metricRow, band *MetricBand) MetricValue {
	v := MetricValue{N: row.N}
	raw := strings.TrimSpace(string(row.Value))
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		v.Value, v.Raw = f, strconv.FormatFloat(f, 'f', -1, 64)
	} else {
		var s string
		if json.Unmarshal(row.Value, &s) == nil {
			raw = s
		}
		v.Raw = strings.TrimSpace(raw)
		if f, err := strconv.ParseFloat(v.Raw, 64); err == nil {
			v.Value = f
		} else {
			v.Value = math.NaN()
		}
	}
	if raw == "" || raw == "null" {
		v.Raw, v.Value = "", math.NaN()
	}
	v.Verdict = NormalizeVerdict(row.Verdict)
	if v.Verdict == "" {
		switch {
		case math.IsNaN(v.Value):
			v.Verdict = VerdictUnmeasured
		case band != nil && band.Contains(v.Value):
			v.Verdict = VerdictOK
		case band != nil:
			v.Verdict = VerdictOff
		}
	}
	return v
}

// NormalizeVerdict maps a file's verdict word onto ok / near / off /
// unmeasured; unknown words give "".
func NormalizeVerdict(s string) string {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", "")) {
	case "pass", "ok", "✓":
		return VerdictOK
	case "near", "≈":
		return VerdictNear
	case "fail", "off", "✗":
		return VerdictOff
	case "notmeasured", "unmeasured", "n/a", "na":
		return VerdictUnmeasured
	}
	return ""
}

// Contains reports whether v is inside the band.
func (b *MetricBand) Contains(v float64) bool {
	if b == nil {
		return true
	}
	if b.Min != nil && v < *b.Min {
		return false
	}
	if b.Max != nil {
		if b.MaxExclusive {
			return v < *b.Max
		}
		return v <= *b.Max
	}
	return true
}

// String renders the band the way the targets read: "≤ 20%", "45–55%",
// "< 2000 ms", "≥ 0.5".
func (b *MetricBand) String() string {
	if b == nil {
		return "—"
	}
	switch {
	case b.Min != nil && b.Max != nil:
		return trimNum(*b.Min) + "–" + FormatMetricValue(*b.Max, b.Unit)
	case b.Max != nil && b.MaxExclusive:
		return "< " + FormatMetricValue(*b.Max, b.Unit)
	case b.Max != nil:
		return "≤ " + FormatMetricValue(*b.Max, b.Unit)
	case b.Min != nil:
		return "≥ " + FormatMetricValue(*b.Min, b.Unit)
	}
	return "—"
}

// FormatMetricValue formats v for unit: pct/share "7%", seconds "132 s",
// ms "268 ms", points "+2", count/other trimmed to 2 decimals.
func FormatMetricValue(v float64, unit string) string {
	if math.IsNaN(v) {
		return "—"
	}
	switch strings.ToLower(unit) {
	case "pct", "share", "%":
		return strconv.FormatFloat(math.Round(v), 'f', -1, 64) + "%"
	case "seconds", "s":
		return strconv.FormatFloat(math.Round(v), 'f', -1, 64) + " s"
	case "ms":
		return strconv.FormatFloat(math.Round(v), 'f', -1, 64) + " ms"
	case "points":
		r := math.Round(v)
		if r > 0 {
			return "+" + strconv.FormatFloat(r, 'f', -1, 64)
		}
		return strconv.FormatFloat(r, 'f', -1, 64)
	}
	return trimNum(v)
}

// FormatValue formats a measured value with the band's unit, or as written.
func (m GuardMetric) FormatValue(v *MetricValue) string {
	if v == nil || (v.Raw == "" && math.IsNaN(v.Value)) {
		return "—"
	}
	if math.IsNaN(v.Value) {
		return v.Raw
	}
	unit := ""
	if m.Band != nil {
		unit = m.Band.Unit
	}
	return FormatMetricValue(v.Value, unit)
}

func trimNum(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

func sameBand(a, b *MetricBand) bool {
	if a == nil || b == nil {
		return a == b
	}
	eq := func(x, y *float64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return eq(a.Min, b.Min) && eq(a.Max, b.Max) && a.MaxExclusive == b.MaxExclusive && strings.EqualFold(a.Unit, b.Unit)
}

// newestRun reads every file matching glob under dir and returns the one with
// the newest run date (ties: newest mtime).
func newestRun(dir, glob string, read func(string) ([]byte, error)) (*parsedRun, error) {
	matches, err := globInRoot(dir, glob)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, errors.New("no file matches")
	}
	var best *parsedRun
	var bestMod int64
	var firstErr error
	for _, rel := range matches {
		data, err := readProjectFile(dir, rel)
		if err != nil {
			firstErr = keepFirst(firstErr, fmt.Errorf("%s: %w", rel, err))
			continue
		}
		pr, err := parseRun(rel, data)
		if err != nil {
			firstErr = keepFirst(firstErr, fmt.Errorf("%s: %w", rel, err))
			continue
		}
		var mod int64
		if st, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			mod = st.ModTime().UnixNano()
		}
		if best == nil || pr.run.Date > best.run.Date || (pr.run.Date == best.run.Date && mod > bestMod) {
			best, bestMod = pr, mod
		}
	}
	if best == nil {
		return nil, firstErr
	}
	return best, nil
}

// committedRun reads b's committed copy of the newest file matching glob.
func committedRun(root string, b OpenBranch, glob string) (*parsedRun, error) {
	if b.Head == "" {
		return nil, errors.New("no head")
	}
	dir := globDir(glob)
	list, err := gitCached(root, "ls-tree", "-r", "--name-only", b.Head, "--", dir)
	if err != nil {
		return nil, err
	}
	var best *parsedRun
	for _, rel := range splitLines(list) {
		if ok, _ := path.Match(glob, rel); !ok {
			continue
		}
		data, err := gitCached(root, "show", b.Head+":"+rel)
		if err != nil || len(data) > MaxWorkbenchFile {
			continue
		}
		pr, err := parseRun(rel, []byte(data))
		if err != nil {
			continue
		}
		if best == nil || pr.run.Date > best.run.Date {
			best = pr
		}
	}
	if best == nil {
		return nil, errors.New("no committed file matches")
	}
	return best, nil
}

func parseRun(rel string, data []byte) (*parsedRun, error) {
	var f metricFile
	if err := decodeJSON(data, &f); err != nil {
		return nil, err
	}
	run := MetricRun{Date: f.Date, Commit: f.Commit, N: f.N, Dirty: f.Dirty}
	if f.Run != nil {
		run = *f.Run
	}
	run.File = rel
	return &parsedRun{run: run, rows: capRows(f.Metrics), raw: data}, nil
}

// runBehind counts commits on head after commit; -1 when commit is not on
// head's history (or unknown), 0 when it is head or unset.
func runBehind(root, commit, head string) int {
	commit = strings.TrimSpace(commit)
	if commit == "" || head == "" || strings.HasPrefix(head, commit) {
		return 0
	}
	full, err := runGit(root, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err != nil {
		return -1
	}
	full = strings.TrimSpace(full)
	mb, err := gitCached(root, "merge-base", full, head)
	if err != nil || strings.TrimSpace(mb) != full {
		return -1
	}
	return countCommits(root, full, head)
}

// CommitsSince counts commits on the root checkout's HEAD after commit that
// touch any of paths (all commits when paths is empty). 0 when unknown.
func CommitsSince(root, commit string, paths []string) int {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return 0
	}
	head, err := runGit(root, "rev-parse", "HEAD")
	if err != nil {
		return 0
	}
	args := []string{"rev-list", "--count", commit + ".." + strings.TrimSpace(head)}
	if len(paths) > 0 {
		args = append(append(args, "--"), paths...)
	}
	text, err := gitCached(root, args...)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(text))
	return n
}

func decodeJSON(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		var se *json.SyntaxError
		if errors.As(err, &se) {
			line := 1 + bytes.Count(data[:min(int(se.Offset), len(data))], []byte("\n"))
			return fmt.Errorf("line %d: %v", line, se)
		}
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) {
			line := 1 + bytes.Count(data[:min(int(te.Offset), len(data))], []byte("\n"))
			return fmt.Errorf("line %d: %s should be %s", line, te.Field, te.Type)
		}
		return err
	}
	return nil
}

func capRows[T any](rows []T) []T {
	if len(rows) > MaxWorkbenchRows {
		return rows[:MaxWorkbenchRows]
	}
	return rows
}

func keepFirst(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// readProjectFile reads rel under root, refusing paths (or symlinks) that
// leave root and files over MaxWorkbenchFile.
func readProjectFile(root, rel string) ([]byte, error) {
	if err := checkProjectRelativePath(rel); err != nil {
		return nil, err
	}
	full := filepath.Join(root, rel)
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("no such file")
		}
		return nil, err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if r, err := filepath.Rel(realRoot, real); err != nil || r == ".." || strings.HasPrefix(r, "../") {
		return nil, errors.New("points outside the project root")
	}
	st, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if st.Size() > MaxWorkbenchFile {
		return nil, fmt.Errorf("file is %d bytes, over the %d MB limit", st.Size(), MaxWorkbenchFile>>20)
	}
	return os.ReadFile(real)
}

// globInRoot returns the files under root matching a root-relative glob, as
// root-relative slash paths, sorted.
func globInRoot(root, glob string) ([]string, error) {
	if err := checkProjectRelativePath(glob); err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(glob)))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range matches {
		if st, err := os.Stat(m); err != nil || st.IsDir() {
			continue
		}
		rel, err := filepath.Rel(root, m)
		if err != nil {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out, nil
}

// globDir is the directory part of glob before its first wildcard.
func globDir(glob string) string {
	dir := path.Dir(glob)
	for strings.ContainsAny(dir, "*?[") {
		dir = path.Dir(dir)
	}
	if dir == "" {
		return "."
	}
	return dir
}

// systemsListing returns the systems (and "system/family") whose list
// matches name. A family's list wins over its system's: a system with a
// family listing name is reported through that family only.
func systemsListing(wb WorkbenchConfig, name string, sysList func(WorkbenchSystem) []string, famList func(WorkbenchFamily) []string) []string {
	var out []string
	for sysName, sys := range wb.Systems {
		if matchesAny(sysList(sys), name) {
			out = append(out, sysName)
			continue
		}
		for _, fam := range sys.Families {
			if matchesAny(famList(fam), name) {
				out = append(out, sysName)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func matchesAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if p == name {
			return true
		}
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

// scopedList is the list that scopes system (and family, when given) to its
// metrics or knobs: the family's list when it has one, else the system's.
func scopedList(wb WorkbenchConfig, system, family string, sysList func(WorkbenchSystem) []string, famList func(WorkbenchFamily) []string) []string {
	_, sys, ok := wb.SystemConfig(system)
	if !ok {
		return nil
	}
	if family != "" {
		if _, fam, ok := sys.Family(family); ok && len(famList(fam)) > 0 {
			return famList(fam)
		}
	}
	return sysList(sys)
}

// MetricsForSystem returns the metrics system (or its family) lists, worst
// verdict first.
func MetricsForSystem(r MetricsReport, wb WorkbenchConfig, system, family string) []GuardMetric {
	list := scopedList(wb, system, family, func(s WorkbenchSystem) []string { return s.Metrics }, func(f WorkbenchFamily) []string { return f.Metrics })
	if len(list) == 0 {
		return nil
	}
	var out []GuardMetric
	for _, m := range r.Metrics {
		if matchesAny(list, m.ID) {
			out = append(out, m)
		}
	}
	rank := map[string]int{VerdictOff: 0, VerdictNear: 1, VerdictOK: 2}
	sort.SliceStable(out, func(i, j int) bool {
		ri, oki := rank[out[i].Verdict()]
		rj, okj := rank[out[j].Verdict()]
		if !oki {
			ri = 3
		}
		if !okj {
			rj = 3
		}
		return ri < rj
	})
	return out
}
