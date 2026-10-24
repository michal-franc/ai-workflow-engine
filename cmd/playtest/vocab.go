package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/testids"
	"github.com/michal-franc/issue-viewer/internal/tracker"
	"gopkg.in/yaml.v3"
)

// The web vocabulary. DOM checks run in the page (assets/checks.js) in both
// ux-test and the overlay; the rest run in the runner against the scenario.
var (
	domChecks = map[string]bool{
		"exists": true, "absent": true, "visible": true, "hidden": true,
		"enabled": true, "disabled": true, "selected": true,
		"text_equals": true, "text_contains": true, "value_equals": true,
		"count": true, "url": true, "changed": true, "unchanged": true,
		"beside": true, "below": true, "css": true, "size": true,
	}
	runnerChecks = map[string]bool{"issue": true, "cli": true, "tmux": true, "http": true}
	actionKinds  = map[string]bool{
		"open": true, "reload": true, "click": true, "hover": true, "type": true,
		"select": true, "key": true, "drag": true, "wait_for": true, "wait": true,
		"post": true, "write": true, "cli": true, "tmux": true, "snapshot": true, "check": true,
	}
	browserActions = map[string]bool{
		"open": true, "reload": true, "click": true, "hover": true, "type": true,
		"select": true, "key": true, "drag": true, "wait_for": true, "wait": true,
		"snapshot": true,
	}
	layoutRules = map[string]bool{
		"inside_viewport": true, "no_clipping": true, "no_overlap": true, "no_zero_size_text": true,
		"styles_applied": true,
	}
	keyNames = map[string]bool{
		"Enter": true, "Escape": true, "Tab": true, "Backspace": true, "Delete": true,
		"ArrowUp": true, "ArrowDown": true, "ArrowLeft": true, "ArrowRight": true,
		"Home": true, "End": true, "PageUp": true, "PageDown": true, "Space": true,
	}
)

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Validator checks sessions without a browser or a board.
type Validator struct {
	ScenarioDir string
	// Checklist, when set, returns the issue's checkbox ids (uppercase),
	// each with its section, so step ids and proves: ids can be checked
	// against it. It returns ok=false when the issue can't be resolved here
	// (no config, not found); ids are then only checked for shape.
	Checklist func(issue string) (sections map[string]string, ok bool)
	// Notes collects non-fatal remarks (an issue that couldn't be resolved).
	Notes []string
}

// Validate returns every problem in s as file:line errors.
func (v *Validator) Validate(s *Session) []error {
	var errs []error
	at := func(line int, format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s:%d: %s", s.File, line, fmt.Sprintf(format, a...)))
	}
	if s.Name == "" {
		at(1, "missing `session:` name")
	} else if base := strings.TrimSuffix(filepath.Base(s.File), ".yaml"); base != s.Name {
		at(1, "session %q should live in %s.yaml (found %s)", s.Name, s.Name, filepath.Base(s.File))
	}
	if s.Issue == "" {
		at(1, "missing `issue:` (the slug whose checkboxes the steps tick)")
	}
	if s.Scenario == "" {
		at(1, "missing `scenario:` (a directory under %s)", v.ScenarioDir)
	} else if _, err := os.Stat(filepath.Join(v.ScenarioDir, s.Scenario, "scenario.yaml")); err != nil {
		at(1, "scenario %q not found: want %s", s.Scenario, filepath.Join(v.ScenarioDir, s.Scenario, "scenario.yaml"))
	}
	for _, vp := range s.Checks.Viewports {
		if _, err := ParseViewport(vp); err != nil {
			at(1, "session_checks.%v", err)
		}
	}
	for _, r := range s.Checks.Layout {
		if !layoutRules[r] {
			at(1, "session_checks.layout: unknown rule %q (rules: %s)", r, strings.Join(sortedKeys(layoutRules), ", "))
		}
	}
	for _, id := range s.Checks.LayoutIgnore {
		v.target(id, 1, at)
	}
	if c := s.Checks.Console; c != "" && c != "no_errors" && c != "off" {
		at(1, "session_checks.console: want no_errors or off, got %q", c)
	}
	for _, re := range s.Checks.ConsoleIgnore {
		if _, err := regexp.Compile(re); err != nil {
			at(1, "session_checks.console_ignore %q: %v", re, err)
		}
	}
	if len(s.Steps) == 0 {
		at(1, "no steps")
	}

	var ids map[string]string
	if v.Checklist != nil && s.Issue != "" {
		var ok bool
		if ids, ok = v.Checklist(s.Issue); !ok {
			ids = nil
			v.Notes = append(v.Notes, fmt.Sprintf("%s: issue %s not resolvable here; step ids checked for shape only", s.File, s.Issue))
		}
	}
	seen := map[string]bool{}
	for _, st := range s.Steps {
		switch {
		case st.ID == "":
			at(st.Line, "step without `id:` (use the issue's checkbox id, e.g. TP3)")
		case !tracker.IsCheckboxRefShaped(st.ID):
			at(st.Line, "step id %q is not a checkbox id (letters then digits, e.g. TP3)", st.ID)
		case seen[st.ID]:
			at(st.Line, "duplicate step id %q", st.ID)
		case ids != nil && ids[strings.ToUpper(st.ID)] == "":
			at(st.Line, "step id %q is not a checkbox in %s (see issue-cli checklist %s)", st.ID, s.Issue, s.Issue)
		}
		for _, id := range st.Proves {
			switch section, known := ids[strings.ToUpper(id)]; {
			case !tracker.IsCheckboxRefShaped(id):
				at(st.Line, "step %s proves %q, which is not a criterion id (e.g. AC2)", st.ID, id)
			case ids != nil && (!known || !strings.EqualFold(section, tracker.CriteriaSection)):
				at(st.Line, "step %s proves %q, which is not an acceptance criterion of %s (see issue-cli checklist %s)", st.ID, id, s.Issue, s.Issue)
			}
		}
		if st.From != "" && !seen[st.From] {
			at(st.Line, "from: %q must name an earlier step", st.From)
		}
		seen[st.ID] = true
		if st.Title == "" {
			at(st.Line, "step %s has no title", st.ID)
		}
		if len(st.checks) == 0 && !st.Judgement() {
			at(st.Line, "step %s checks nothing: add checks:, or expect: for the owner", st.ID)
		}
		if st.Judgement() && st.Do == "" {
			at(st.Line, "judgement step %s needs do: (what the owner does)", st.ID)
		}
		if !st.Judgement() && (st.Do != "" || st.Why != "") {
			at(st.Line, "step %s has do:/why: but no expect:, so the owner never sees it", st.ID)
		}
		for _, a := range append(append([]Action{}, st.setup...), st.act...) {
			v.action(a, at)
		}
		for _, c := range st.checks {
			v.check(c, at)
		}
		for _, t := range st.targets {
			v.target(t.ID, st.Line, at)
		}
	}
	return errs
}

func (v *Validator) target(id string, line int, at errAt) {
	if id == "" {
		at(line, "empty target id")
		return
	}
	if !testids.Known(id) {
		hint := ""
		if s := testids.Suggest(id); s != "" {
			hint = fmt.Sprintf("; did you mean %q?", s)
		}
		at(line, "unknown target %q — ids are listed in internal/testids/testids.go%s", id, hint)
	}
}

func (v *Validator) action(a Action, at errAt) {
	if !actionKinds[a.Kind] {
		at(a.Line, "unknown action %q (actions: %s)", a.Kind, strings.Join(sortedKeys(actionKinds), ", "))
		return
	}
	need := func(ok bool, form string) {
		if !ok {
			at(a.Line, "%s: want %s", a.Kind, form)
		}
	}
	switch a.Kind {
	case "open":
		p := argString(a.Arg)
		need(strings.HasPrefix(p, "/"), "a path starting with /, e.g. /p/{project}/board")
	case "reload":
	case "click", "hover":
		if t := argString(a.Arg); t != "" {
			v.target(t, a.Line, at)
		} else {
			need(false, "a target id")
		}
	case "type":
		m, ok := argMap(a.Arg)
		need(ok && m["target"] != nil && m["text"] != nil, "{target, text, clear?, enter?}")
		if ok {
			v.target(fmt.Sprint(m["target"]), a.Line, at)
		}
	case "select":
		m, ok := argMap(a.Arg)
		need(ok && m["target"] != nil && m["value"] != nil, "{target, value}")
		if ok {
			v.target(fmt.Sprint(m["target"]), a.Line, at)
		}
	case "key":
		need(keyNames[argString(a.Arg)], "a key name ("+strings.Join(sortedKeys(keyNames), ", ")+")")
	case "drag":
		m, ok := argMap(a.Arg)
		need(ok && m["target"] != nil && m["to"] != nil, "{target, to}")
		if ok {
			v.target(fmt.Sprint(m["target"]), a.Line, at)
			v.target(fmt.Sprint(m["to"]), a.Line, at)
		}
	case "wait_for":
		c, _, err := waitForCheck(a)
		if err != nil {
			at(a.Line, "wait_for: %v", err)
			return
		}
		v.check(c, at)
	case "wait":
		var ms int
		need(a.Arg.Decode(&ms) == nil && ms > 0 && ms <= 30000, "milliseconds (1–30000); prefer wait_for")
	case "post":
		m, ok := argMap(a.Arg)
		p, _ := m["path"].(string)
		need(ok && strings.HasPrefix(p, "/"), "{path, json?|form?, headers?, status?}")
	case "write":
		m, ok := argMap(a.Arg)
		p, _ := m["path"].(string)
		need(ok && p != "" && !strings.HasPrefix(p, "/") && m["text"] != nil, "{path, text}: path relative to the scenario's project root")
	case "cli":
		_, err := cliArgs(a.Arg)
		if err != nil {
			at(a.Line, "cli: %v", err)
		}
	case "tmux":
		_, ok := argList(a.Arg)
		need(ok, "a list of tmux arguments")
	case "snapshot":
		m, ok := argMap(a.Arg)
		need(ok && m["name"] != nil && m["target"] != nil, "{name, target}")
		if ok {
			v.target(fmt.Sprint(m["target"]), a.Line, at)
		}
	case "check":
		for _, c := range checksFromNode(a.Arg, at) {
			v.check(c, at)
		}
	}
}

// waitForCheck turns a wait_for argument into the check it waits for and
// its budget in ms: `wait_for: x` waits for x to be visible;
// `wait_for: {hidden: x, ms: 8000}` waits for any one check.
func waitForCheck(a Action) (Check, int, error) {
	ms := 5000
	if t := argString(a.Arg); t != "" {
		return Check{Kind: "visible", Arg: map[string]any{"target": t}, Line: a.Line}, ms, nil
	}
	if a.Arg == nil || a.Arg.Kind != yaml.MappingNode {
		return Check{}, 0, fmt.Errorf("want a target id or {<check>: …, ms?}")
	}
	var kind string
	var arg *yaml.Node
	for i := 0; i+1 < len(a.Arg.Content); i += 2 {
		k, val := a.Arg.Content[i].Value, a.Arg.Content[i+1]
		if k == "ms" {
			if err := val.Decode(&ms); err != nil || ms <= 0 || ms > 60000 {
				return Check{}, 0, fmt.Errorf("ms: want 1–60000")
			}
			continue
		}
		if kind != "" {
			return Check{}, 0, fmt.Errorf("wait for one check at a time (got %s and %s)", kind, k)
		}
		kind, arg = k, val
	}
	if kind == "" {
		return Check{}, 0, fmt.Errorf("no check to wait for")
	}
	node := &yaml.Node{Kind: yaml.MappingNode, Line: a.Line, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: kind}, arg}}
	var perr error
	cs := checksFromNode(node, func(line int, f string, x ...any) { perr = fmt.Errorf(f, x...) })
	if perr != nil {
		return Check{}, 0, perr
	}
	if len(cs) != 1 {
		return Check{}, 0, fmt.Errorf("wait for one check at a time")
	}
	return cs[0], ms, nil
}

// cliArgs reads `cli: [args…]` or `cli: {args: […], exit: N}`.
func cliArgs(n *yaml.Node) (cliCall, error) {
	if l, ok := argList(n); ok {
		if len(l) == 0 {
			return cliCall{}, fmt.Errorf("empty argument list")
		}
		return cliCall{Args: l}, nil
	}
	var c cliCall
	if n == nil || n.Kind != yaml.MappingNode || n.Decode(&c) != nil || len(c.Args) == 0 {
		return cliCall{}, fmt.Errorf("want [args…] or {args: […], exit: N}")
	}
	return c, nil
}

type cliCall struct {
	Args []string `yaml:"args"`
	Exit *int     `yaml:"exit"`
}

func (v *Validator) check(c Check, at errAt) {
	if !domChecks[c.Kind] && !runnerChecks[c.Kind] {
		all := append(sortedKeys(domChecks), sortedKeys(runnerChecks)...)
		at(c.Line, "unknown check %q (checks: %s)", c.Kind, strings.Join(all, ", "))
		return
	}
	has := func(keys ...string) bool {
		for _, k := range keys {
			if c.Arg[k] != nil {
				return true
			}
		}
		return false
	}
	need := func(ok bool, form string) {
		if !ok {
			at(c.Line, "%s: want %s", c.Kind, form)
		}
	}
	str := func(k string) string { s, _ := c.Arg[k].(string); return s }
	switch c.Kind {
	case "exists", "absent", "visible", "hidden", "enabled", "disabled", "selected":
		need(str("target") != "", "a target id")
	case "text_equals", "text_contains":
		need(str("target") != "" && c.Arg["text"] != nil, "{target, text}")
	case "value_equals":
		need(str("target") != "" && c.Arg["value"] != nil, "{target, value}")
	case "count":
		need(str("pattern") != "" && has("equals", "min", "max"), "{pattern, within?, equals|min|max}")
		if p := str("pattern"); p != "" {
			v.target(p, c.Line, at)
		}
		if w := str("within"); w != "" {
			v.target(w, c.Line, at)
		}
	case "url":
		need(has("equals", "contains", "matches"), "{equals|contains|matches}")
		if m := str("matches"); m != "" {
			if _, err := regexp.Compile(m); err != nil {
				at(c.Line, "url.matches: %v", err)
			}
		}
	case "changed", "unchanged":
		need(str("target") != "" && str("since") != "", "{target, since: <snapshot name>}")
	case "beside", "below":
		form := "[a, b] or {a, b}"
		if c.Kind == "beside" {
			form = "[a, b] or {a, b, gap_max?} (b to the right of a, on the same row)"
		}
		need(str("a") != "" && str("b") != "", form)
		for _, k := range []string{"a", "b"} {
			if t := str(k); t != "" {
				v.target(t, c.Line, at)
			}
		}
		if g, ok := c.Arg["gap_max"]; ok {
			_, isNum := number(g)
			need(c.Kind == "beside" && isNum, "gap_max: a number of px (beside only)")
		}
	case "css":
		n := 0
		for _, k := range []string{"equals", "in", "not"} {
			if c.Arg[k] != nil {
				n++
			}
		}
		need(str("target") != "" && str("property") != "" && n == 1, "{target, property, equals | in: [..] | not}")
		if in, ok := c.Arg["in"]; ok {
			_, isList := in.([]any)
			need(isList, "in: a list of values")
		}
	case "size":
		bounds := map[string]float64{}
		for _, k := range []string{"min_width", "max_width", "min_height", "max_height"} {
			if x, ok := c.Arg[k]; ok {
				f, isNum := number(x)
				need(isNum && f >= 0, k+": a number of CSS px")
				bounds[k] = f
			}
		}
		need(str("target") != "" && len(bounds) > 0, "{target, min_width?, max_width?, min_height?, max_height?} with at least one bound")
		for _, d := range []string{"width", "height"} {
			lo, hasLo := bounds["min_"+d]
			hi, hasHi := bounds["max_"+d]
			if hasLo && hasHi && lo > hi {
				at(c.Line, "size: min_%s %v is above max_%s %v", d, lo, d, hi)
			}
		}
	case "issue":
		need(str("slug") != "" && has("field", "checkbox", "comment_contains", "body_contains"),
			"{slug, field+equals | checkbox+checked | comment_contains | body_contains}")
		if has("field") {
			need(has("equals"), "{slug, field, equals}")
		}
		if has("checkbox") {
			need(has("checked"), "{slug, checkbox, checked: true|false}")
		}
	case "cli":
		args, ok := c.Arg["args"].([]any)
		need(ok && len(args) > 0, "{args: […], exit?, contains?, not_contains?}")
	case "tmux":
		need(has("session", "pane"), "{session, present} or {pane, contains}")
		if has("session") {
			need(has("present"), "{session, present: true|false}")
		}
		if has("pane") {
			need(has("contains"), "{pane, contains}")
		}
	case "http":
		p := str("path")
		need(strings.HasPrefix(p, "/") && has("status", "contains", "json"), "{path, status?, contains?, json?}")
	}
	if t := str("target"); t != "" && c.Kind != "url" && domChecks[c.Kind] {
		v.target(t, c.Line, at)
	}
}

// number reads a YAML number (int or float) as float64.
func number(x any) (float64, bool) {
	switch n := x.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
