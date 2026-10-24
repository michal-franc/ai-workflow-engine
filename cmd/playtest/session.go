package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Session is one playtest session file (playtest/sessions/<name>.yaml). Its
// shape follows the shared playtest contract (issue-cli process playtest):
// steps keyed by the issue's checkbox ids, setup/act/checks, and do/expect/why
// for the owner. The action and check vocabulary is this runner's own.
type Session struct {
	Name     string        `yaml:"session"`
	Issue    string        `yaml:"issue"`
	Title    string        `yaml:"title"`
	Scenario string        `yaml:"scenario"`
	Checks   SessionChecks `yaml:"session_checks"`
	Steps    []*Step       `yaml:"steps"`

	// File is the path the session was loaded from.
	File string `yaml:"-"`
}

// SessionChecks are the run-wide settings ux-test applies to every step.
type SessionChecks struct {
	Viewports     []string `yaml:"viewports"`
	Layout        []string `yaml:"layout"`
	LayoutIgnore  []string `yaml:"layout_ignore"`
	Console       string   `yaml:"console"`
	ConsoleIgnore []string `yaml:"console_ignore"`
	Screenshots   string   `yaml:"screenshots"`
	SettleMS      int      `yaml:"settle_ms"`
}

// Step is one checkbox's worth of checks.
type Step struct {
	ID      string      `yaml:"id"`
	Title   string      `yaml:"title"`
	From    string      `yaml:"from"`
	Setup   []yaml.Node `yaml:"setup"`
	Act     []yaml.Node `yaml:"act"`
	Checks  []yaml.Node `yaml:"checks"`
	Targets []yaml.Node `yaml:"targets"`
	Do      string      `yaml:"do"`
	Expect  string      `yaml:"expect"`
	Why     string      `yaml:"why"`
	// Proves lists the acceptance criteria this step proves (AC2); the
	// runner copies it into the step's result for issue-cli prove.
	Proves []string `yaml:"proves"`

	Line int `yaml:"-"`

	setup, act []Action
	checks     []Check
	targets    []Target
}

// Judgement reports whether the step needs the owner (it has expect:).
func (s *Step) Judgement() bool { return strings.TrimSpace(s.Expect) != "" }

// Action is one setup/act entry: a single-key map such as `click: board.x`.
type Action struct {
	Kind string
	Arg  *yaml.Node
	Line int
}

// Check is one machine check, after list expansion (`visible: [a, b]`
// becomes two checks).
type Check struct {
	Kind string         `json:"kind"`
	Arg  map[string]any `json:"arg"`
	Line int            `json:"-"`
}

// Label is a short human form of the check for logs and reports.
func (c Check) Label() string {
	if t, ok := c.Arg["target"].(string); ok && len(c.Arg) == 1 {
		return c.Kind + " " + t
	}
	if pairCheck[c.Kind] && len(c.Arg) == 2 {
		return fmt.Sprintf("%s [%v, %v]", c.Kind, c.Arg["a"], c.Arg["b"])
	}
	parts := make([]string, 0, len(c.Arg))
	keys := make([]string, 0, len(c.Arg))
	for k := range c.Arg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, c.Arg[k]))
	}
	return c.Kind + " {" + strings.Join(parts, ", ") + "}"
}

// Target is an id the overlay highlights, with an optional note.
type Target struct {
	ID   string `json:"id"`
	Note string `json:"note,omitempty"`
}

// Viewport is a browser window size.
type Viewport struct{ W, H int }

func (v Viewport) String() string { return fmt.Sprintf("%dx%d", v.W, v.H) }

// DefaultViewports are desktop and phone width.
var DefaultViewports = []string{"1440x900", "390x844"}

// ParseViewport reads "1440x900".
func ParseViewport(s string) (Viewport, error) {
	w, h, ok := strings.Cut(strings.TrimSpace(s), "x")
	if !ok {
		return Viewport{}, fmt.Errorf("viewport %q: want WIDTHxHEIGHT", s)
	}
	wi, err1 := strconv.Atoi(w)
	hi, err2 := strconv.Atoi(h)
	if err1 != nil || err2 != nil || wi < 200 || hi < 200 {
		return Viewport{}, fmt.Errorf("viewport %q: want WIDTHxHEIGHT, each at least 200", s)
	}
	return Viewport{wi, hi}, nil
}

// Viewports returns the session's viewports (defaults when unset).
func (s *Session) Viewports() []Viewport {
	src := s.Checks.Viewports
	if len(src) == 0 {
		src = DefaultViewports
	}
	var out []Viewport
	for _, v := range src {
		if vp, err := ParseViewport(v); err == nil {
			out = append(out, vp)
		}
	}
	return out
}

// Settle is how long checks may take to become true after act.
func (s *Session) Settle() int {
	if s.Checks.SettleMS > 0 {
		return s.Checks.SettleMS
	}
	return 3000
}

// Step returns the step with id, or nil.
func (s *Session) Step(id string) *Step {
	for _, st := range s.Steps {
		if st.ID == id {
			return st
		}
	}
	return nil
}

// LoadSession reads and parses one session file. Structural problems in
// actions and checks are returned as a list of file:line errors; the
// session is still returned so callers can report everything at once.
func LoadSession(path string) (*Session, []error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []error{err}
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, []error{fmt.Errorf("%s: %v", path, err)}
	}
	s := &Session{File: relPath(path)}
	if err := root.Decode(s); err != nil {
		return nil, []error{fmt.Errorf("%s: %v", path, err)}
	}
	// Line numbers for steps come from the raw node tree.
	if len(root.Content) > 0 {
		if steps := mapValue(root.Content[0], "steps"); steps != nil && steps.Kind == yaml.SequenceNode {
			for i, n := range steps.Content {
				if i < len(s.Steps) {
					s.Steps[i].Line = n.Line
				}
			}
		}
	}
	var errs []error
	at := func(line int, format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s:%d: %s", path, line, fmt.Sprintf(format, a...)))
	}
	for _, st := range s.Steps {
		st.setup = parseActions(st.Setup, at)
		st.act = parseActions(st.Act, at)
		st.checks = parseChecks(st.Checks, at)
		st.targets = parseTargets(st.Targets, at)
	}
	return s, errs
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

type errAt func(line int, format string, a ...any)

func parseActions(nodes []yaml.Node, at errAt) []Action {
	var out []Action
	for i := range nodes {
		n := &nodes[i]
		if n.Kind != yaml.MappingNode || len(n.Content) != 2 {
			at(n.Line, "an action is a single-key map, e.g. `click: board.search`")
			continue
		}
		out = append(out, Action{Kind: n.Content[0].Value, Arg: n.Content[1], Line: n.Line})
	}
	return out
}

func parseChecks(nodes []yaml.Node, at errAt) []Check {
	var out []Check
	for i := range nodes {
		out = append(out, checksFromNode(&nodes[i], at)...)
	}
	return out
}

// pairCheck marks the checks whose list form is one pair of targets
// (`beside: [a, b]`) rather than one check per target.
var pairCheck = map[string]bool{"beside": true, "below": true}

// checksFromNode turns `visible: x`, `visible: [x, y]` and
// `text_equals: {target: x, text: y}` into canonical checks. A bare string
// argument becomes {target: <string>}.
func checksFromNode(n *yaml.Node, at errAt) []Check {
	if n.Kind != yaml.MappingNode || len(n.Content) != 2 {
		at(n.Line, "a check is a single-key map, e.g. `visible: board.search`")
		return nil
	}
	kind, arg := n.Content[0].Value, n.Content[1]
	switch arg.Kind {
	case yaml.ScalarNode:
		return []Check{{Kind: kind, Arg: map[string]any{"target": arg.Value}, Line: n.Line}}
	case yaml.SequenceNode:
		if pairCheck[kind] {
			if len(arg.Content) != 2 || arg.Content[0].Kind != yaml.ScalarNode || arg.Content[1].Kind != yaml.ScalarNode {
				at(arg.Line, "%s: want two target ids, [a, b]", kind)
				return nil
			}
			return []Check{{Kind: kind, Arg: map[string]any{"a": arg.Content[0].Value, "b": arg.Content[1].Value}, Line: n.Line}}
		}
		var out []Check
		for _, item := range arg.Content {
			if item.Kind != yaml.ScalarNode {
				at(item.Line, "%s: a list holds target ids only", kind)
				continue
			}
			out = append(out, Check{Kind: kind, Arg: map[string]any{"target": item.Value}, Line: item.Line})
		}
		return out
	case yaml.MappingNode:
		m := map[string]any{}
		if err := arg.Decode(&m); err != nil {
			at(arg.Line, "%s: %v", kind, err)
			return nil
		}
		return []Check{{Kind: kind, Arg: m, Line: n.Line}}
	}
	at(n.Line, "%s: unsupported argument", kind)
	return nil
}

func parseTargets(nodes []yaml.Node, at errAt) []Target {
	var out []Target
	for i := range nodes {
		n := &nodes[i]
		switch n.Kind {
		case yaml.ScalarNode:
			out = append(out, Target{ID: n.Value})
		case yaml.MappingNode:
			var t Target
			if err := n.Decode(&t); err != nil || t.ID == "" {
				at(n.Line, "a target is an id or {id, note}")
				continue
			}
			out = append(out, t)
		default:
			at(n.Line, "a target is an id or {id, note}")
		}
	}
	return out
}

// relPath shortens path to be relative to the working directory when it is
// inside it, so errors read `playtest/sessions/x.yaml:12: …`.
func relPath(path string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}

// LoadSessions loads every *.yaml under dir, sorted by file name.
func LoadSessions(dir string) ([]*Session, []error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, []error{err}
	}
	sort.Strings(files)
	var out []*Session
	var errs []error
	for _, f := range files {
		s, e := LoadSession(f)
		errs = append(errs, e...)
		if s != nil {
			out = append(out, s)
		}
	}
	return out, errs
}

// SelectSessions filters by session name and/or issue slug.
func SelectSessions(all []*Session, name, issue string) []*Session {
	var out []*Session
	for _, s := range all {
		if name != "" && s.Name != name {
			continue
		}
		if issue != "" && s.Issue != issue {
			continue
		}
		out = append(out, s)
	}
	return out
}

// argString reads a scalar action argument, or "" when it is not one.
func argString(n *yaml.Node) string {
	if n != nil && n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return ""
}

// argMap decodes a mapping action argument.
func argMap(n *yaml.Node) (map[string]any, bool) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, false
	}
	m := map[string]any{}
	if err := n.Decode(&m); err != nil {
		return nil, false
	}
	return m, true
}

// argList decodes a sequence of scalars (for cli/tmux args).
func argList(n *yaml.Node) ([]string, bool) {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil, false
	}
	var out []string
	for _, c := range n.Content {
		if c.Kind != yaml.ScalarNode {
			return nil, false
		}
		out = append(out, c.Value)
	}
	return out, true
}

func scalarNode(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: v} }
