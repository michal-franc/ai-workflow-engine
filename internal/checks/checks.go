// Package checks is the guided check scripts feature: an issue's
// scripts/checks/<name>.sh walks the owner through its manual checks with the
// helper library embedded here (lib.sh). issue-cli check-script runs the
// scripts; the viewer offers ▶ Run checks while one has no owner run.
package checks

import (
	"bufio"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Lib is the helper library every check script sources ($CHECKS_LIB).
//
//go:embed lib.sh
var Lib []byte

// Dir is where an issue's scripts live, relative to its checkout.
const Dir = "scripts/checks"

// RecordDir is where summary writes run records, relative to the checkout.
const RecordDir = ".agent-logs/checks"

// headerLines is how far down a script its `# issue: <slug>` line may sit.
const headerLines = 15

var issueHeader = regexp.MustCompile(`^#\s*issue:\s*(\S+)`)

// validName is a script name check-script --new accepts.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Script is one check script found for an issue.
type Script struct {
	Name string // file name without .sh
	Path string // absolute path
	Root string // the checkout it was found in (runs from here)
}

// Find returns the scripts under each root's scripts/checks/ whose header
// names slug, the first root winning for a name. Roots that don't exist are
// skipped.
func Find(roots []string, slug string) []Script {
	seen := map[string]bool{}
	var out []Script
	for _, root := range roots {
		if root == "" {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(root, Dir, "*.sh"))
		sort.Strings(files)
		for _, f := range files {
			name := strings.TrimSuffix(filepath.Base(f), ".sh")
			if seen[name] || IssueOf(f) != slug {
				continue
			}
			seen[name] = true
			abs, err := filepath.Abs(f)
			if err != nil {
				abs = f
			}
			out = append(out, Script{Name: name, Path: abs, Root: root})
		}
	}
	return out
}

// IssueOf reads the slug from a script's `# issue: <slug>` header, or "".
func IssueOf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for i := 0; i < headerLines && sc.Scan(); i++ {
		if m := issueHeader.FindStringSubmatch(strings.TrimSpace(sc.Text())); m != nil {
			return m[1]
		}
	}
	return ""
}

// OwnerRun reports whether the script has a finished owner run: a run record
// summary wrote from a terminal, under any of the roots.
func OwnerRun(roots []string, name string) bool {
	for _, root := range roots {
		if root == "" {
			continue
		}
		if runs, _ := filepath.Glob(filepath.Join(root, RecordDir, name, "*-owner.txt")); len(runs) > 0 {
			return true
		}
	}
	return false
}

// Due returns the names of slug's scripts that have no owner run yet.
func Due(roots []string, slug string) []string {
	var due []string
	for _, s := range Find(roots, slug) {
		if !OwnerRun(roots, s.Name) {
			due = append(due, s.Name)
		}
	}
	return due
}

// WindowName is the tmux window (or session) ▶ Run checks opens for slug:
// check-script-<slug>, with the slug sanitised as in agent names.
func WindowName(slug string) string {
	r := strings.NewReplacer("/", "-", ".", "-", " ", "-")
	return "check-script-" + r.Replace(slug)
}

// ValidName reports whether name can be a script's file name.
func ValidName(name string) bool {
	return validName.MatchString(name)
}

// LibPath writes Lib to a cache file named after its content and returns the
// path, so concurrent runs share it and an upgrade never edits a file a
// running script has open.
func LibPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "issue-viewer", "checks")
	sum := sha256.Sum256(Lib)
	path := filepath.Join(dir, "lib-"+hex.EncodeToString(sum[:])[:12]+".sh")
	if data, err := os.ReadFile(path); err == nil && string(data) == string(Lib) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "lib-*.tmp")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(Lib); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return path, nil
}

// Box is one unticked Manual checkbox a new script gets a step for.
type Box struct {
	ID   string
	Text string
}

// Skeleton is the starting script `check-script --new` writes: the header,
// the source line, and one step per box (or one example step).
func Skeleton(slug, name string, boxes []Box) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	fmt.Fprintf(&b, "# issue: %s\n", slug)
	fmt.Fprintf(&b, "# The owner's manual checks for %s, one step at a time. Run it with:\n", slug)
	fmt.Fprintf(&b, "#   issue-cli check-script %s\n", slug)
	b.WriteString("# The helpers (step, run, expect, ask, summary, sandbox_project, private_tmux, …): issue-cli check-script --lib\n")
	b.WriteString("# shellcheck source=/dev/null disable=SC2034,SC2154\n")
	b.WriteString("source \"${CHECKS_LIB:?run me with: issue-cli check-script <slug>}\"\n\n")
	b.WriteString("checks_intro \"TODO: what changed, in a line or two, and how long this takes.\"\n")
	if len(boxes) == 0 {
		boxes = []Box{{ID: "MV1", Text: "TODO: what this step checks"}}
	}
	for _, box := range boxes {
		fmt.Fprintf(&b, "\n# ── %s %s\n", box.ID, strings.Repeat("─", max(1, 66-len(box.ID))))
		fmt.Fprintf(&b, "step %s %s --box %s \\\n", box.ID, shellQuote(box.Text), box.ID)
		b.WriteString("\t--what \"TODO\" --why \"TODO\" --look \"TODO\" --expect \"TODO\"\n")
		b.WriteString("run true\n")
		b.WriteString("expect '[ $code = 0 ]' \"TODO: what a pass means\"\n")
	}
	b.WriteString("\nsummary\n")
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
