// Package clibug is what `issue-cli report-bug` files: a report about a bug
// in issue-cli or the workflow engine, the engine project it goes to, and the
// per-user queue that holds reports while no engine project is reachable.
package clibug

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// Label marks an issue filed by report-bug.
const Label = "cli-bug"

// EngineRepos are the repo names the engine project is known by, the current
// one first. issue-viewer's own entry may still carry the old name.
var EngineRepos = []string{"michal-franc/ai-workflow-engine", "michal-franc/ai-native-project-viewer"}

// titleLimit caps a report's title; the full text always goes in the body.
const titleLimit = 80

// Report is one bug report. The JSON keys ts, description, issue_slug and
// tool are the old bugs/*.json format, so --import and the queue share it.
type Report struct {
	At        time.Time `json:"ts"`
	Text      string    `json:"description"`
	Tool      string    `json:"tool,omitempty"`
	Project   string    `json:"project,omitempty"`
	IssueSlug string    `json:"issue_slug,omitempty"`
	Agent     string    `json:"agent,omitempty"`
	Version   string    `json:"version,omitempty"`
	OS        string    `json:"os,omitempty"`
	Command   string    `json:"command,omitempty"`
}

// Title is the report's first sentence, cut at a word boundary near
// titleLimit characters.
func (r Report) Title() string {
	text := strings.TrimSpace(r.Text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	for i, c := range text {
		if (c == '.' || c == '!' || c == '?') && (i+1 == len(text) || text[i+1] == ' ') {
			text = text[:i]
			break
		}
	}
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= titleLimit {
		return text
	}
	cut := string(runes[:titleLimit])
	if i := strings.LastIndexByte(cut, ' '); i > titleLimit/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:-") + "…"
}

// ReportedFrom is the reporter as <project>:<issue slug>, or the part known.
func (r Report) ReportedFrom() string {
	switch {
	case r.Project != "" && r.IssueSlug != "":
		return r.Project + ":" + r.IssueSlug
	case r.Project != "":
		return r.Project
	case r.IssueSlug != "":
		return "unknown:" + r.IssueSlug
	}
	return ""
}

// Context is the report's context as markdown bullets, for the issue body
// and for report: comments.
func (r Report) Context() string {
	var b strings.Builder
	line := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "- %s: %s\n", k, v)
		}
	}
	line("reported from", r.ReportedFrom())
	line("agent", r.Agent)
	if r.Version != "" {
		line("issue-cli", strings.TrimSpace(r.Version+" "+r.OS))
	}
	if r.Command != "" {
		line("command", "`"+r.Command+"`")
	}
	if !r.At.IsZero() {
		line("at", r.At.UTC().Format(time.RFC3339))
	}
	return b.String()
}

// NormalizeTitle is the dedupe key: lowercase, punctuation dropped,
// whitespace collapsed.
func NormalizeTitle(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(c) || unicode.IsDigit(c):
			b.WriteRune(c)
		case unicode.IsSpace(c) || unicode.IsPunct(c) || unicode.IsSymbol(c):
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// FindEngine picks the engine project from projects.yaml: engine_project
// first, then the project whose repo is one of EngineRepos. nil when this
// machine has none.
func FindEngine(cfg *tracker.ProjectsConfig) *tracker.Project {
	if cfg == nil {
		return nil
	}
	if slug := strings.TrimSpace(cfg.EngineProject); slug != "" {
		for i := range cfg.Projects {
			if cfg.Projects[i].Slug == slug {
				return &cfg.Projects[i]
			}
		}
		return nil
	}
	for _, repo := range EngineRepos {
		for i := range cfg.Projects {
			if strings.EqualFold(cfg.Projects[i].Repo, repo) {
				return &cfg.Projects[i]
			}
		}
	}
	return nil
}

// Result says what happened to a report that reached a board.
type Result struct {
	// Kind is "created" or "commented".
	Kind    string `json:"result"`
	Project string `json:"project"`
	Slug    string `json:"slug"`
	File    string `json:"file"`
	// Reports counts the reports on the issue, this one included.
	Reports int `json:"reports"`
}

// Sender delivers a report somewhere it gets triaged: the local engine board
// today, GitHub issues later.
type Sender interface {
	Send(r Report) (Result, error)
}

// dataDir is ${XDG_DATA_HOME:-~/.local/share}/issue-cli.
func dataDir() string {
	base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "issue-cli")
}

// QueueDir holds reports not sent yet. It is per user, not per project.
func QueueDir() string {
	d := dataDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "bug-reports")
}

// Enqueue saves r in the queue and returns its path.
func Enqueue(r Report) (string, error) {
	dir := QueueDir()
	if dir == "" {
		return "", fmt.Errorf("no home directory for the bug-report queue")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	base := r.At.UTC().Format("20060102-150405")
	// The zero-padded sequence keeps names sorting oldest first.
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("%s-%03d.json", base, i)
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(append(data, '\n'))
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return path, werr
	}
	return "", fmt.Errorf("no free name for a queued report in %s", dir)
}

// Queued is a report waiting in the queue.
type Queued struct {
	Path   string
	Report Report
}

// Pending lists the queued reports, oldest first. Files that don't parse are
// left alone and skipped.
func Pending() []Queued {
	paths, _ := filepath.Glob(filepath.Join(QueueDir(), "*.json"))
	sort.Strings(paths)
	var out []Queued
	for _, p := range paths {
		r, err := ReadReport(p)
		if err != nil {
			continue
		}
		out = append(out, Queued{Path: p, Report: r})
	}
	return out
}

// PendingCount is the number of reports not sent, for the Status page.
func PendingCount() int {
	if QueueDir() == "" {
		return 0
	}
	paths, _ := filepath.Glob(filepath.Join(QueueDir(), "*.json"))
	return len(paths)
}

// ReadReport reads one queued report or old bugs/*.json file.
func ReadReport(path string) (Report, error) {
	var r Report
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
	if strings.TrimSpace(r.Text) == "" {
		return r, fmt.Errorf("%s: no description", path)
	}
	return r, nil
}

// lockTimeout bounds the wait for another report-bug filing at the same time.
const lockTimeout = 15 * time.Second

// WithLock runs fn while holding the machine-wide report-bug lock, so two
// agents reporting the same bug at once get one issue, not two.
func WithLock(fn func() error) error {
	dir := dataDir()
	if dir == "" {
		return fn()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "report-bug.lock"), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	deadline := time.Now().Add(lockTimeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for another report-bug to finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
