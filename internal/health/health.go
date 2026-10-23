// Package health takes project health snapshots: dated JSON files under
// .health/snapshots/ in the main checkout, one row per metric. Metric
// families (codebase here; workflow, context cost and check speed in their
// own packages) register with Register from init(), so whichever binary
// imports them takes the same snapshot. See docs/workbench-files.md,
// "Health snapshots".
package health

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Units a Metric can carry. Unit is a free string; these are the ones the
// families use.
const (
	UnitCount   = "count"
	UnitSeconds = "seconds"
	UnitShare   = "share" // 0..1
	UnitBytes   = "bytes"
)

// Triggers: what took a snapshot.
const (
	TriggerDaily  = "daily"
	TriggerCI     = "ci"
	TriggerManual = "manual"
	// TriggerBackfill: issue-cli health backfill computed a past day.
	TriggerBackfill = "backfill"
)

// ErrNoPast is a family's answer when env.Backfill asks it about a past day
// it cannot measure (the current milestone, say). The family is left out of
// that snapshot, not recorded as failed.
var ErrNoPast = errors.New("cannot measure a past day")

// Metric is one value a family measured. Name is the stable series name
// (<family>.<snake_case path>); Labels split it (lang=go, status=all).
type Metric struct {
	Name   string
	Labels map[string]string
	Value  float64
	Unit   string
	Help   string // one line; the first non-empty Help per Name is used
}

// Env is what a family measures: the project, at Commit on Branch of the
// main checkout Root.
type Env struct {
	Root     string // main checkout
	Project  *tracker.Project
	Workflow *tracker.WorkflowConfig
	Config   *tracker.HealthConfig
	Branch   string
	Commit   string
	Now      time.Time
	// Backfill: Now is a past day and Commit where the branch stood then
	// (issue-cli health backfill). A family that only knows the present
	// returns ErrNoPast.
	Backfill bool
}

// Family measures one group of metrics. A non-nil error means the whole
// family failed; it is recorded and the snapshot is still written.
type Family func(ctx context.Context, env Env) ([]Metric, error)

type registered struct {
	name string
	fn   Family
}

var (
	registryMu sync.Mutex
	registry   []registered
)

// Register adds a family. Call it from init(); a duplicate name panics.
func Register(name string, f Family) {
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, r := range registry {
		if r.name == name {
			panic("health: family registered twice: " + name)
		}
	}
	registry = append(registry, registered{name, f})
}

var hints = map[string]string{}

// Describe sets a family's one-line pointer ("tables: /stats"), stored with
// its result and printed after it by issue-cli health. Call it from init().
func Describe(name, hint string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	hints[name] = hint
}

func hintFor(name string) string {
	registryMu.Lock()
	defer registryMu.Unlock()
	return hints[name]
}

// Families lists the registered family names, sorted.
func Families() []string {
	var names []string
	for _, r := range families() {
		names = append(names, r.name)
	}
	return names
}

func families() []registered {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := append([]registered(nil), registry...)
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// Snapshot is one .health/snapshots/<ts>.json file. Its metrics rows keep the
// Metrics view's row shape (id, value, name, group, band.unit).
type Snapshot struct {
	Run      RunInfo                 `json:"run"`
	Families map[string]FamilyResult `json:"families"`
	Metrics  []Row                   `json:"metrics"`
}

// RunInfo is the snapshot header.
type RunInfo struct {
	Date       string `json:"date"` // local time, 2006-01-02T15:04
	Project    string `json:"project,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Commit     string `json:"commit,omitempty"`
	Trigger    string `json:"trigger"`
	DurationMS int64  `json:"duration_ms"`
}

// FamilyResult says how one family went.
type FamilyResult struct {
	MS      int64  `json:"ms"`
	Metrics int    `json:"metrics"`
	Error   string `json:"error,omitempty"`
	Hint    string `json:"hint,omitempty"` // where to read more (Describe)
}

// Row is one metric in the file. ID is Metric plus its labels, sorted:
// codebase.lines{lang=go}.
type Row struct {
	ID     string            `json:"id"`
	Metric string            `json:"metric"`
	Labels map[string]string `json:"labels,omitempty"`
	Value  float64           `json:"value"`
	Name   string            `json:"name,omitempty"`
	Group  string            `json:"group"`
	Band   *Band             `json:"band,omitempty"`
}

// Band carries the unit, as in the Metrics view's format.
type Band struct {
	Unit string `json:"unit"`
}

// Time is the snapshot's date, zero when it doesn't parse.
func (s *Snapshot) Time() time.Time {
	t, _ := time.ParseInLocation(dateLayout, s.Run.Date, time.Local)
	return t
}

// Value is the row with metric name and exactly these labels, if present.
func (s *Snapshot) Value(metric string, labels map[string]string) (float64, bool) {
	id := RowID(metric, labels)
	for _, r := range s.Metrics {
		if r.ID == id {
			return r.Value, true
		}
	}
	return 0, false
}

// Rows lists the rows of one metric name, in file order.
func (s *Snapshot) Rows(metric string) []Row {
	var out []Row
	for _, r := range s.Metrics {
		if r.Metric == metric {
			out = append(out, r)
		}
	}
	return out
}

// RowID is name{k=v,...} with the labels sorted; the bare name without labels.
func RowID(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + labels[k]
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}

const dateLayout = "2006-01-02T15:04"
const fileLayout = "2006-01-02T1504"

// Dir is .health in the main checkout.
func Dir(root string) string { return filepath.Join(root, ".health") }

// SnapshotsDir holds the snapshot files.
func SnapshotsDir(root string) string { return filepath.Join(Dir(root), "snapshots") }

// ErrNotGit is NewEnv's error for a project outside any git repository: it
// has no codebase to measure.
var ErrNotGit = errors.New("not in a git repository")

// NewEnv resolves the project's main checkout, branch and head from dir (the
// project root). It fails when dir is not in a git repository or the branch
// has no commit.
func NewEnv(proj *tracker.Project, dir string, now time.Time) (Env, error) {
	if _, err := gitOut(context.Background(), dir, "rev-parse", "--git-dir"); err != nil {
		return Env{}, fmt.Errorf("%s: %w", dir, ErrNotGit)
	}
	root := ci.RepoRoot(dir)
	wf := proj.LoadWorkflow()
	branch, err := ci.ResolveBranch(root, wf.CI)
	if err != nil {
		return Env{}, err
	}
	head, err := ci.Head(root, branch)
	if err != nil || head == "" {
		return Env{}, fmt.Errorf("branch %s has no commit in %s", branch, root)
	}
	return Env{Root: root, Project: proj, Workflow: wf, Config: wf.Health, Branch: branch, Commit: head, Now: now}, nil
}

// Take runs every registered family and writes the snapshot. It returns the
// snapshot and its path; an error means nothing was written.
func Take(ctx context.Context, env Env, trigger string) (*Snapshot, string, error) {
	return take(ctx, env, trigger, families())
}

func take(ctx context.Context, env Env, trigger string, fams []registered) (*Snapshot, string, error) {
	if env.Now.IsZero() {
		env.Now = time.Now()
	}
	if env.Root == "" {
		return nil, "", errors.New("health: no project root")
	}
	start := time.Now()
	snap := &Snapshot{
		Run:      RunInfo{Date: env.Now.Local().Format(dateLayout), Branch: env.Branch, Commit: ci.Short(env.Commit), Trigger: trigger},
		Families: map[string]FamilyResult{},
	}
	if env.Project != nil {
		snap.Run.Project = env.Project.Slug
	}
	help := map[string]string{}
	for _, f := range fams {
		famStart := time.Now()
		metrics, err := runFamily(ctx, env, f)
		if errors.Is(err, ErrNoPast) {
			continue
		}
		res := FamilyResult{MS: time.Since(famStart).Milliseconds(), Hint: hintFor(f.name)}
		if err != nil {
			res.Error = err.Error()
			snap.Families[f.name] = res
			continue
		}
		for _, m := range metrics {
			if help[m.Name] == "" {
				help[m.Name] = m.Help
			}
			row := Row{ID: RowID(m.Name, m.Labels), Metric: m.Name, Labels: m.Labels, Value: m.Value, Group: f.name}
			if m.Unit != "" {
				row.Band = &Band{Unit: m.Unit}
			}
			snap.Metrics = append(snap.Metrics, row)
		}
		res.Metrics = len(metrics)
		snap.Families[f.name] = res
	}
	for i := range snap.Metrics {
		snap.Metrics[i].Name = help[snap.Metrics[i].Metric]
	}
	snap.Run.DurationMS = time.Since(start).Milliseconds()
	path, err := Write(env.Root, snap, env.Now)
	if err != nil {
		return nil, "", err
	}
	prune(env.Root, env.Now)
	return snap, path, nil
}

// runFamily runs f under the family timeout. A panic or a timeout is the
// family's error; a timed-out family is left to finish in the background.
func runFamily(ctx context.Context, env Env, f registered) ([]Metric, error) {
	limit := env.Config.FamilyTimeout()
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	type result struct {
		metrics []Metric
		err     error
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- result{err: fmt.Errorf("panic: %v", p)}
			}
		}()
		m, err := f.fn(ctx, env)
		done <- result{m, err}
	}()
	select {
	case r := <-done:
		return r.metrics, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("timed out after %s", limit)
	}
}

// Write stores snap as .health/snapshots/<now>.json (a later snapshot in the
// same minute replaces it) and returns the path.
func Write(root string, snap *Snapshot, now time.Time) (string, error) {
	if err := ensureDir(root); err != nil {
		return "", err
	}
	path := filepath.Join(SnapshotsDir(root), now.Local().Format(fileLayout)+".json")
	return path, writeJSONFile(path, snap)
}

// ensureDir creates .health/snapshots and a .gitignore that ignores the
// whole directory, so no repo has to list .health/ itself.
func ensureDir(root string) error {
	if err := os.MkdirAll(SnapshotsDir(root), 0o755); err != nil {
		return err
	}
	ignore := filepath.Join(Dir(root), ".gitignore")
	if _, err := os.Stat(ignore); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(ignore, []byte("# Project health snapshots (issue-cli health); never committed\n*\n"), 0o644)
	}
	return nil
}
