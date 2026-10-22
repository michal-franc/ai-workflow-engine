package tracker

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Tuning knobs: the project exports its knobs as JSON (name, value, comment,
// file, line; docs/workbench-files.md) and lists the export files under
// workbench.tuning. The viewer never parses source code for values; git
// history and branch edits are found by the knob's name in the changed lines
// of its file.

const (
	knobHistoryMax     = 10
	knobHistoryCommits = 300 // commits of a knob file scanned for history
)

// KnobChange is one commit that changed a knob's value.
type KnobChange struct {
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	From    string `json:"from,omitempty"` // "" when the knob was added
	To      string `json:"to,omitempty"`   // "" when it was removed
	Subject string `json:"subject"`
}

// Label renders the change as "30 → 20", "added 30" or "removed".
func (c KnobChange) Label() string {
	switch {
	case c.From == "" && c.To == "":
		return "changed"
	case c.From == "":
		return "added " + c.To
	case c.To == "":
		return "removed"
	}
	return c.From + " → " + c.To
}

// BranchEdit is an open branch that changes a knob.
type BranchEdit struct {
	Branch    string `json:"branch"`
	Worktree  string `json:"worktree,omitempty"`
	IssueSlug string `json:"issue,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
}

// Label renders the edit like KnobChange.Label.
func (e BranchEdit) Label() string {
	return KnobChange{From: e.From, To: e.To}.Label()
}

// Knob is one tuning knob with its history and branch edits.
type Knob struct {
	Name    string       `json:"name"`
	Value   string       `json:"value"`
	Comment string       `json:"comment,omitempty"`
	File    string       `json:"file,omitempty"`
	Line    int          `json:"line,omitempty"`
	Export  string       `json:"export"` // the export file it came from
	History []KnobChange `json:"history,omitempty"`
	Edits   []BranchEdit `json:"edits,omitempty"`
	Systems []string     `json:"systems,omitempty"`
}

// Last is the newest change on the main branch, or nil.
func (k Knob) Last() *KnobChange {
	if len(k.History) == 0 {
		return nil
	}
	return &k.History[0]
}

type knobFile struct {
	Knobs []struct {
		Name    string `json:"name"`
		Value   any    `json:"value"`
		Comment string `json:"comment"`
		File    string `json:"file"`
		Line    int    `json:"line"`
	} `json:"knobs"`
}

// LoadKnobs reads every export file in workbench.tuning, then adds git
// history (on root's HEAD) and the edits each open branch makes. Problems are
// returned alongside what could be read.
func LoadKnobs(root string, wb WorkbenchConfig, branches []OpenBranch) ([]Knob, []string) {
	var knobs []Knob
	var problems []string
	seen := map[string]bool{}
	for _, export := range wb.Tuning {
		data, err := readProjectFile(root, export)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", export, err))
			continue
		}
		var f knobFile
		if err := decodeJSON(data, &f); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", export, err))
			continue
		}
		for _, k := range capRows(f.Knobs) {
			name := strings.TrimSpace(k.Name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			file := k.File
			if file != "" && checkProjectRelativePath(file) != nil {
				problems = append(problems, fmt.Sprintf("%s: knob %s: file %q is outside the project", export, name, file))
				file = ""
			}
			knobs = append(knobs, Knob{Name: name, Value: knobValueString(k.Value), Comment: strings.TrimSpace(k.Comment), File: file, Line: k.Line, Export: export})
		}
	}

	byFile := map[string][]int{}
	for i, k := range knobs {
		if k.File != "" {
			byFile[k.File] = append(byFile[k.File], i)
		}
	}
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)

	head, headErr := runGit(root, "rev-parse", "HEAD")
	head = strings.TrimSpace(head)
	if headErr != nil && len(files) > 0 {
		problems = append(problems, "knob history: "+headErr.Error())
	}
	for _, file := range files {
		idx := byFile[file]
		if headErr == nil {
			if err := addKnobHistory(root, head, file, knobs, idx); err != nil {
				problems = append(problems, fmt.Sprintf("knob history for %s: %v", file, err))
			}
		}
		for _, b := range branches {
			addKnobEdits(root, b, file, knobs, idx)
		}
	}

	for i := range knobs {
		knobs[i].Systems = systemsListing(wb, knobs[i].Name, func(s WorkbenchSystem) []string { return s.Knobs }, func(f WorkbenchFamily) []string { return f.Knobs })
	}
	return knobs, problems
}

// addKnobHistory scans the file's history once and records, per knob, the
// commits whose changed lines assign it.
func addKnobHistory(root, head, file string, knobs []Knob, idx []int) error {
	text, err := gitCached(root, "log", "-p", "-U0", "--no-color", "--no-renames", "--date=short",
		fmt.Sprintf("-n%d", knobHistoryCommits), "--format=%x1e%h%x1f%ad%x1f%s", head, "--", file)
	if err != nil {
		return err
	}
	for _, block := range strings.Split(text, "\x1e") {
		header, diff, _ := strings.Cut(block, "\n")
		parts := strings.SplitN(header, "\x1f", 3)
		if len(parts) != 3 {
			continue
		}
		minus, plus := changedLines(diff)
		for _, i := range idx {
			k := &knobs[i]
			if len(k.History) >= knobHistoryMax {
				continue
			}
			from, inMinus := assignedValue(minus, k.Name)
			to, inPlus := assignedValue(plus, k.Name)
			if !inMinus && !inPlus || (inMinus && inPlus && from == to) {
				continue
			}
			k.History = append(k.History, KnobChange{Commit: parts[0], Date: parts[1], From: from, To: to, Subject: parts[2]})
		}
	}
	return nil
}

// addKnobEdits records the knobs in file that branch b assigns differently
// from its merge-base.
func addKnobEdits(root string, b OpenBranch, file string, knobs []Knob, idx []int) {
	if b.MergeBase == "" || b.Ahead == 0 {
		return
	}
	text, err := gitCached(root, "diff", "-U0", "--no-color", "--no-renames", b.MergeBase, b.Head, "--", file)
	if err != nil || text == "" {
		return
	}
	minus, plus := changedLines(text)
	for _, i := range idx {
		k := &knobs[i]
		from, inMinus := assignedValue(minus, k.Name)
		to, inPlus := assignedValue(plus, k.Name)
		if !inMinus && !inPlus || (inMinus && inPlus && from == to) {
			continue
		}
		k.Edits = append(k.Edits, BranchEdit{Branch: b.Name, Worktree: b.Worktree, IssueSlug: b.IssueSlug, From: from, To: to})
	}
}

// changedLines splits a unified diff into its removed and added lines.
func changedLines(diff string) (minus, plus []string) {
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"):
		case strings.HasPrefix(line, "-"):
			minus = append(minus, line[1:])
		case strings.HasPrefix(line, "+"):
			plus = append(plus, line[1:])
		}
	}
	return
}

var knobRegexCache sync.Map // name → *regexp.Regexp

// assignedValue finds `name = value` or `name: value` in lines and returns
// the value up to `;`, `,` or a comment. Code comments are skipped.
func assignedValue(lines []string, name string) (string, bool) {
	var re *regexp.Regexp
	if v, ok := knobRegexCache.Load(name); ok {
		re = v.(*regexp.Regexp)
	} else {
		re = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])` + regexp.QuoteMeta(name) + `\s*(?:=|:)\s*([^;,]*)`)
		knobRegexCache.Store(name, re)
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		m := re.FindStringSubmatch(line)
		if m == nil || strings.HasPrefix(strings.TrimSpace(m[1]), "=") {
			continue // `==` comparison, not an assignment
		}
		return cleanKnobValue(m[1]), true
	}
	return "", false
}

var numberSuffixRe = regexp.MustCompile(`^(-?[0-9][0-9_]*\.?[0-9_]*(?:[eE][-+]?[0-9]+)?)[fFdDmMlLuU]$`)

func cleanKnobValue(v string) string {
	for _, c := range []string{"//", " #", "/*"} {
		if i := strings.Index(v, c); i >= 0 {
			v = v[:i]
		}
	}
	v = strings.TrimSpace(v)
	if m := numberSuffixRe.FindStringSubmatch(v); m != nil {
		v = m[1]
	}
	return v
}

func knobValueString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return trimNum(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return fmt.Sprint(v)
}

// KnobsForSystem returns the knobs system (or its family) lists, most
// recently changed first.
func KnobsForSystem(knobs []Knob, wb WorkbenchConfig, system, family string) []Knob {
	list := scopedList(wb, system, family, func(s WorkbenchSystem) []string { return s.Knobs }, func(f WorkbenchFamily) []string { return f.Knobs })
	if len(list) == 0 {
		return nil
	}
	var out []Knob
	for _, k := range knobs {
		if matchesAny(list, k.Name) {
			out = append(out, k)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := "", ""
		if l := out[i].Last(); l != nil {
			di = l.Date
		}
		if l := out[j].Last(); l != nil {
			dj = l.Date
		}
		return di > dj
	})
	return out
}
