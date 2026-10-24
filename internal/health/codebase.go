package health

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The codebase family: size by kind, area and language, tests, big files and
// growth, read from git objects at the snapshot's commit (never the working
// tree, so a dirty main checkout doesn't count).
func init() {
	Register("codebase", codebaseFamily)
}

const (
	maxBlobBytes = 2 << 20 // bigger blobs are counted as binary, unread
	sniffBytes   = 8000    // a NUL in this prefix makes a blob binary
	bigFilesTop  = 20
)

// growthWindows are the growth comparisons, by label.
var growthWindows = []struct {
	label string
	days  int
}{{"7d", 7}, {"30d", 30}}

type count struct{ files, lines int }

type bigFile struct {
	path  string
	lines int
}

// tally is what one scan of a tree counted. Binary files only add to binary.
type tally struct {
	byKind map[string]*count
	byArea map[string]*count
	byLang map[string]*count
	binary int
	big    []bigFile
}

func newTally() *tally {
	return &tally{byKind: map[string]*count{}, byArea: map[string]*count{}, byLang: map[string]*count{}}
}

func add(m map[string]*count, key string, lines int) {
	c := m[key]
	if c == nil {
		c = &count{}
		m[key] = c
	}
	c.files++
	c.lines += lines
}

// scanRules decide which files count and how.
type scanRules struct {
	skip    func(p string) bool
	tests   []string
	bigFrom int
}

func codebaseFamily(ctx context.Context, env Env) ([]Metric, error) {
	if env.Commit == "" {
		if env.Backfill {
			return nil, ErrNoPast
		}
		return nil, fmt.Errorf("no commit to measure")
	}
	rules := rulesFor(env)
	cur, err := scanTree(ctx, env.Root, env.Commit, rules)
	if err != nil {
		return nil, err
	}
	if cur.byKind[KindTest] == nil {
		Warn(ctx, "%s matched no code file, so test lines count as 0", globNote("test_globs", env.Config != nil && len(env.Config.TestGlobs) > 0, rules.tests))
	}
	out := tallyMetrics(cur, rules.bigFrom)
	g, err := growthMetrics(ctx, env, cur, rules)
	if err != nil {
		return nil, err
	}
	return append(out, g...), nil
}

// rulesFor builds the scan rules from the health config: the exclude globs,
// plus the project's issues dir when it is inside the repo, .worktrees/ and
// .health/.
func rulesFor(env Env) scanRules {
	excl := env.Config.Excluded()
	issues := issuesPrefix(env)
	return scanRules{
		skip: func(p string) bool {
			if strings.HasPrefix(p, ".worktrees/") || strings.HasPrefix(p, ".health/") {
				return true
			}
			if issues != "" && strings.HasPrefix(p, issues) {
				return true
			}
			return matchAny(excl, p)
		},
		tests:   env.Config.Tests(),
		bigFrom: env.Config.BigFileLimit(),
	}
}

// issuesPrefix is the project's issues dir relative to the repo root, with a
// trailing slash; "" when it is outside the repo or is the root itself.
func issuesPrefix(env Env) string {
	if env.Project == nil || env.Project.IssueDir == "" {
		return ""
	}
	dir, err := filepath.Abs(env.Project.IssueDir)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	root := env.Root
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel) + "/"
}

type blobEntry struct {
	sha  string
	path string
	size int64
}

// scanTree counts every blob in commit's tree.
func scanTree(ctx context.Context, root, commit string, rules scanRules) (*tally, error) {
	t := newTally()
	oversized, err := walkBlobs(ctx, root, commit, rules.skip, func(p string, data []byte) {
		t.addFile(p, data, rules)
	})
	if err != nil {
		return nil, err
	}
	t.binary += oversized
	return t, nil
}

// walkBlobs reads every blob in commit's tree that skip keeps: one ls-tree,
// then one cat-file --batch. visit gets each blob's content (binary ones
// too; the caller sniffs). Blobs over maxBlobBytes are not read, only
// counted in oversized. Symlinks and submodules are skipped.
func walkBlobs(ctx context.Context, root, commit string, skip func(string) bool, visit func(p string, data []byte)) (oversized int, err error) {
	listing, err := gitBytes(ctx, root, "ls-tree", "-r", "-l", "-z", commit)
	if err != nil {
		return 0, err
	}
	var toRead []blobEntry
	for _, rec := range bytes.Split(listing, []byte{0}) {
		meta, p, ok := strings.Cut(string(rec), "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta) // mode type object size
		if len(f) != 4 || f[1] != "blob" || f[0] == "120000" {
			continue // submodules, symlinks
		}
		if skip(p) {
			continue
		}
		size, _ := strconv.ParseInt(f[3], 10, 64)
		if size > maxBlobBytes {
			oversized++
			continue
		}
		toRead = append(toRead, blobEntry{f[2], p, size})
	}
	if len(toRead) == 0 {
		return oversized, nil
	}

	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go func() {
		w := bufio.NewWriter(stdin)
		for _, b := range toRead {
			w.WriteString(b.sha + "\n")
		}
		w.Flush()
		stdin.Close()
	}()
	r := bufio.NewReaderSize(stdout, 1<<16)
	var buf []byte
	for _, b := range toRead {
		header, err := r.ReadString('\n')
		if err != nil {
			cmd.Wait()
			return 0, fmt.Errorf("git cat-file: %w", err)
		}
		f := strings.Fields(header) // sha type size
		if len(f) != 3 {
			cmd.Wait()
			return 0, fmt.Errorf("git cat-file: unexpected %q", strings.TrimSpace(header))
		}
		size, _ := strconv.Atoi(f[2])
		if cap(buf) < size+1 {
			buf = make([]byte, size+1)
		}
		data := buf[:size+1] // content plus the trailing newline
		if _, err := io.ReadFull(r, data); err != nil {
			cmd.Wait()
			return 0, fmt.Errorf("git cat-file: %w", err)
		}
		visit(b.path, data[:size])
	}
	if err := cmd.Wait(); err != nil {
		return 0, fmt.Errorf("git cat-file: %w", err)
	}
	return oversized, nil
}

// addFile counts one blob's content.
func (t *tally) addFile(p string, data []byte, rules scanRules) {
	if bytes.IndexByte(data[:min(len(data), sniffBytes)], 0) >= 0 {
		t.binary++
		return
	}
	lines := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}
	lang := classify(p)
	kind := lang.kind
	if kind == KindCode && matchAny(rules.tests, p) {
		kind = KindTest
	}
	area := "."
	if i := strings.IndexByte(p, '/'); i > 0 {
		area = p[:i]
	}
	add(t.byKind, kind, lines)
	add(t.byArea, area, lines)
	add(t.byLang, lang.name, lines)
	if lines > rules.bigFrom {
		t.big = append(t.big, bigFile{p, lines})
	}
}

func (t *tally) kind(k string) count {
	if c := t.byKind[k]; c != nil {
		return *c
	}
	return count{}
}

// tallyMetrics turns a scan into rows. Every kind is always written (0 when
// empty), so the series stay continuous.
func tallyMetrics(t *tally, bigFrom int) []Metric {
	var out []Metric
	for _, k := range kinds {
		c := t.kind(k)
		out = append(out,
			Metric{Name: "codebase.files", Labels: map[string]string{"kind": k}, Value: float64(c.files), Unit: UnitCount, Help: "Text files"},
			Metric{Name: "codebase.lines", Labels: map[string]string{"kind": k}, Value: float64(c.lines), Unit: UnitCount, Help: "Lines in text files"})
	}
	for _, by := range []struct {
		label string
		m     map[string]*count
	}{{"area", t.byArea}, {"lang", t.byLang}} {
		for _, key := range sortedKeys(by.m) {
			c := by.m[key]
			out = append(out,
				Metric{Name: "codebase.files", Labels: map[string]string{by.label: key}, Value: float64(c.files), Unit: UnitCount},
				Metric{Name: "codebase.lines", Labels: map[string]string{by.label: key}, Value: float64(c.lines), Unit: UnitCount})
		}
	}
	code, test := t.kind(KindCode), t.kind(KindTest)
	out = append(out,
		Metric{Name: "codebase.source_lines", Value: float64(code.lines + test.lines), Unit: UnitCount, Help: "Code and test lines"},
		Metric{Name: "codebase.binary_files", Value: float64(t.binary), Unit: UnitCount, Help: "Binary files and files over 2 MB, counted but not read"},
		Metric{Name: "codebase.test_ratio", Value: ratio(test.lines, code.lines), Unit: UnitShare, Help: "Test lines per code line"},
		Metric{Name: "codebase.big_files", Value: float64(len(t.big)), Unit: UnitCount, Help: fmt.Sprintf("Files over %d lines", bigFrom)})
	big := append([]bigFile(nil), t.big...)
	sort.Slice(big, func(i, j int) bool {
		if big[i].lines != big[j].lines {
			return big[i].lines > big[j].lines
		}
		return big[i].path < big[j].path
	})
	if len(big) > bigFilesTop {
		big = big[:bigFilesTop]
	}
	for _, b := range big {
		out = append(out, Metric{Name: "codebase.big_file.lines", Labels: map[string]string{"path": b.path}, Value: float64(b.lines), Unit: UnitCount, Help: "Lines in each of the biggest files"})
	}
	return out
}

// growthMetrics compares the kind totals with the newest snapshot at or
// before each window. Without one, the tree where the branch stood at that
// date stands in (commitBefore); a repo younger than the window gets no
// growth for it.
func growthMetrics(ctx context.Context, env Env, cur *tally, rules scanRules) ([]Metric, error) {
	var out []Metric
	for _, w := range growthWindows {
		at := env.Now.AddDate(0, 0, -w.days)
		base, baseAt := snapshotKinds(env.Root, at)
		if base == nil {
			commit, when, err := commitBefore(ctx, env.Root, env.Branch, env.Commit, at)
			if err != nil {
				return nil, err
			}
			if commit == "" {
				continue
			}
			t := cur
			if commit != env.Commit {
				if t, err = scanTree(ctx, env.Root, commit, rules); err != nil {
					return nil, err
				}
			}
			base, baseAt = map[string]count{}, when
			for _, k := range kinds {
				base[k] = t.kind(k)
			}
		}
		for _, k := range kinds {
			c, b := cur.kind(k), base[k]
			labels := map[string]string{"kind": k, "window": w.label}
			out = append(out,
				Metric{Name: "codebase.growth.files", Labels: labels, Value: float64(c.files - b.files), Unit: UnitCount, Help: "Change in text files over the window"},
				Metric{Name: "codebase.growth.lines", Labels: labels, Value: float64(c.lines - b.lines), Unit: UnitCount, Help: "Change in lines over the window"})
		}
		age := math.Round(env.Now.Sub(baseAt).Hours()/24*10) / 10
		out = append(out, Metric{Name: "codebase.growth.base_age_days", Labels: map[string]string{"window": w.label}, Value: age, Help: "Age of what growth is measured against, in days"})
	}
	return out, nil
}

// snapshotKinds reads the kind totals of the newest snapshot at or before t
// whose codebase family succeeded; nil when there is none.
func snapshotKinds(root string, t time.Time) (map[string]count, time.Time) {
	s := Before(root, t)
	if s == nil {
		return nil, time.Time{}
	}
	if res, ok := s.Families["codebase"]; !ok || res.Error != "" {
		return nil, time.Time{}
	}
	out := map[string]count{}
	for _, k := range kinds {
		files, ok1 := s.Value("codebase.files", map[string]string{"kind": k})
		lines, ok2 := s.Value("codebase.lines", map[string]string{"kind": k})
		if !ok1 || !ok2 {
			return nil, time.Time{}
		}
		out[k] = count{int(files), int(lines)}
	}
	return out, s.Time()
}

// commitBefore is where branch stood at t, and since when. The branch's
// reflog says exactly; when it doesn't reach back to t, the newest commit
// reachable from head made at or before t stands in (first-parent history is
// no better: fast-forward merges put work branches on it). "" when the
// history starts after t.
func commitBefore(ctx context.Context, root, branch, head string, t time.Time) (string, time.Time, error) {
	if branch != "" {
		reflog, _ := gitOut(ctx, root, "reflog", "show", "--date=unix", "--format=%H %gd", "refs/heads/"+branch)
		for _, line := range strings.Split(reflog, "\n") { // newest first
			sha, sel, _ := strings.Cut(line, " ")
			i := strings.LastIndex(sel, "@{")
			if i < 0 {
				continue
			}
			secs, err := strconv.ParseInt(strings.TrimSuffix(sel[i+2:], "}"), 10, 64)
			if err != nil {
				continue
			}
			if when := time.Unix(secs, 0); !when.After(t) {
				return sha, t, nil
			}
		}
	}
	out, err := gitOut(ctx, root, "log", "-1", "--format=%H %ct", "--before="+t.Format(time.RFC3339), head)
	if err != nil || out == "" {
		return "", time.Time{}, err
	}
	sha, ts, _ := strings.Cut(out, " ")
	secs, _ := strconv.ParseInt(ts, 10, 64)
	return sha, time.Unix(secs, 0), nil
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return math.Round(float64(a)/float64(b)*1000) / 1000
}

func sortedKeys(m map[string]*count) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// gitBytes runs git in dir and returns its raw stdout.
func gitBytes(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
