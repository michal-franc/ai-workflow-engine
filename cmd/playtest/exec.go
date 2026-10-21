package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// stepEnv is what a step's actions and checks run against: the scenario,
// the browser page (nil when a check needs none), and the snapshots taken so
// far.
type stepEnv struct {
	sc    *Scenario
	page  *Page
	snaps map[string]string
	log   io.Writer
}

// actionBudget is how long an action waits for its target to be there,
// visible, enabled and not covered.
const actionBudget = 5 * time.Second

var keyMap = map[string]string{
	"Enter": kb.Enter, "Escape": kb.Escape, "Tab": kb.Tab, "Backspace": kb.Backspace,
	"Delete": kb.Delete, "ArrowUp": kb.ArrowUp, "ArrowDown": kb.ArrowDown,
	"ArrowLeft": kb.ArrowLeft, "ArrowRight": kb.ArrowRight, "Home": kb.Home, "End": kb.End,
	"PageUp": kb.PageUp, "PageDown": kb.PageDown, "Space": " ",
}

type ptResult struct {
	OK     bool    `json:"ok"`
	Reason string  `json:"reason"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

// retry calls fn until it reports ok or the budget runs out, returning the
// last reason.
func retry(ctx context.Context, budget time.Duration, fn func() (ptResult, error)) (ptResult, error) {
	deadline := time.Now().Add(budget)
	for {
		r, err := fn()
		if err == nil && r.OK {
			return r, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			if err != nil {
				return r, err
			}
			return r, fmt.Errorf("%s", r.Reason)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (e *stepEnv) logf(format string, a ...any) {
	fmt.Fprintf(e.log, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, a...)...)
}

func (e *stepEnv) needPage(kind string) error {
	if e.page == nil {
		return fmt.Errorf("%s needs a browser page", kind)
	}
	return nil
}

// do runs one action.
func (e *stepEnv) do(ctx context.Context, a Action) error {
	e.logf("action %s (line %d)", a.Kind, a.Line)
	if browserActions[a.Kind] {
		if err := e.needPage(a.Kind); err != nil {
			return err
		}
	}
	switch a.Kind {
	case "open":
		u := e.sc.Base + e.sc.Expand(argString(a.Arg))
		return e.page.Do(ctx, chromedp.Navigate(u))
	case "reload":
		return e.page.Do(ctx, chromedp.Reload())
	case "click", "hover":
		t := argString(a.Arg)
		p, err := retry(ctx, actionBudget, func() (ptResult, error) {
			var r ptResult
			return r, e.page.Call(ctx, &r, "point", t)
		})
		if err != nil {
			return fmt.Errorf("%s %s: %v", a.Kind, t, err)
		}
		if a.Kind == "hover" {
			return e.page.Do(ctx, chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, p.X, p.Y))
		}
		return e.page.Do(ctx, chromedp.MouseClickXY(p.X, p.Y))
	case "type":
		m, _ := argMap(a.Arg)
		t, text := fmt.Sprint(m["target"]), fmt.Sprint(m["text"])
		clear := m["clear"] != false
		if _, err := retry(ctx, actionBudget, func() (ptResult, error) {
			var r ptResult
			return r, e.page.Call(ctx, &r, "focus", t, clear)
		}); err != nil {
			return fmt.Errorf("type into %s: %v", t, err)
		}
		steps := []chromedp.Action[chromedp.Void]{chromedp.KeyEvent(text)}
		if m["enter"] == true {
			steps = append(steps, chromedp.KeyEvent(kb.Enter))
		}
		return e.page.Do(ctx, steps...)
	case "select":
		m, _ := argMap(a.Arg)
		t := fmt.Sprint(m["target"])
		if _, err := retry(ctx, actionBudget, func() (ptResult, error) {
			var r ptResult
			return r, e.page.Call(ctx, &r, "select", t, fmt.Sprint(m["value"]))
		}); err != nil {
			return fmt.Errorf("select on %s: %v", t, err)
		}
		return nil
	case "key":
		return e.page.Do(ctx, chromedp.KeyEvent(keyMap[argString(a.Arg)]))
	case "drag":
		m, _ := argMap(a.Arg)
		return e.drag(ctx, fmt.Sprint(m["target"]), fmt.Sprint(m["to"]))
	case "wait_for":
		c, ms, err := waitForCheck(a)
		if err != nil {
			return err
		}
		res := e.settle(ctx, []Check{c}, time.Duration(ms)*time.Millisecond)
		if !res[0].Pass {
			return fmt.Errorf("wait_for %s: still false after %dms (actual: %s)", c.Label(), ms, res[0].Actual)
		}
		return nil
	case "wait":
		var ms int
		_ = a.Arg.Decode(&ms)
		return e.page.Do(ctx, chromedp.Sleep(time.Duration(ms)*time.Millisecond))
	case "snapshot":
		m, _ := argMap(a.Arg)
		var text *string
		expr := fmt.Sprintf(`JSON.stringify((function(){var el=window.__pt.first(%q);return el?window.__pt.text(el):null})())`, fmt.Sprint(m["target"]))
		if err := e.page.Eval(ctx, expr, &text); err != nil {
			return err
		}
		if text == nil {
			return fmt.Errorf("snapshot %s: %s not found", m["name"], m["target"])
		}
		e.snaps[fmt.Sprint(m["name"])] = *text
		return nil
	case "post":
		m, _ := argMap(a.Arg)
		code, body, err := e.post(ctx, m)
		if err != nil {
			return err
		}
		want, hasWant := m["status"]
		if hasWant && fmt.Sprint(want) != fmt.Sprint(code) {
			return fmt.Errorf("post %s: status %d, want %v: %s", m["path"], code, want, trim(body))
		}
		if !hasWant && code >= 400 {
			return fmt.Errorf("post %s: status %d: %s", m["path"], code, trim(body))
		}
		return nil
	case "cli":
		call, err := cliArgs(a.Arg)
		if err != nil {
			return err
		}
		out, code, err := e.sc.CLI(ctx, e.expandAll(call.Args)...)
		e.logf("issue-cli %s → exit %d\n%s", strings.Join(call.Args, " "), code, out)
		if err != nil {
			return err
		}
		want := 0
		if call.Exit != nil {
			want = *call.Exit
		}
		if code != want {
			return fmt.Errorf("issue-cli %s: exit %d, want %d: %s", strings.Join(call.Args, " "), code, want, trim(out))
		}
		return nil
	case "tmux":
		args, _ := argList(a.Arg)
		out, code, err := e.sc.Tmux(ctx, e.expandAll(args)...)
		e.logf("tmux %s → exit %d\n%s", strings.Join(args, " "), code, out)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("tmux %s: exit %d: %s", strings.Join(args, " "), code, trim(out))
		}
		return nil
	case "check":
		var perr error
		cs := checksFromNode(a.Arg, func(line int, f string, x ...any) { perr = fmt.Errorf(f, x...) })
		if perr != nil {
			return perr
		}
		for _, r := range e.settle(ctx, cs, 3*time.Second) {
			if !r.Pass {
				return fmt.Errorf("check %s failed (actual: %s)", r.Check, r.Actual)
			}
		}
		return nil
	}
	return fmt.Errorf("unknown action %q", a.Kind)
}

// drag drags src onto dst with real mouse events when both fit on screen
// (the browser runs native HTML5 drag and drop), else with synthetic drag
// events in the page.
func (e *stepEnv) drag(ctx context.Context, src, dst string) error {
	to, err := retry(ctx, actionBudget, func() (ptResult, error) {
		var r ptResult
		return r, e.page.Call(ctx, &r, "point", dst)
	})
	if err != nil {
		return fmt.Errorf("drag to %s: %v", dst, err)
	}
	var from ptResult
	if err := e.page.Eval(ctx, fmt.Sprintf(`JSON.stringify((function(){
		var el = window.__pt.first(%q); if (!el) return {ok:false, reason:%q};
		var r = el.getBoundingClientRect(), x = r.left + r.width/2, y = r.top + r.height/2;
		var inView = x >= 0 && y >= 0 && x <= innerWidth && y <= innerHeight;
		return {ok: inView, reason: inView ? '' : 'off screen', x: x, y: y};
	})())`, src, src+" not found"), &from); err != nil {
		return err
	}
	if from.OK {
		e.logf("drag %s → %s with the mouse", src, dst)
		return e.page.Do(ctx, chromedp.DragAndDropXY(from.X, from.Y, to.X, to.Y))
	}
	e.logf("drag %s → %s with synthetic events (%s)", src, dst, from.Reason)
	var r ptResult
	if err := e.page.Call(ctx, &r, "drag", src, dst); err != nil {
		return err
	}
	if !r.OK {
		return fmt.Errorf("drag %s → %s: %s", src, dst, r.Reason)
	}
	return nil
}

func (e *stepEnv) expandAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = e.sc.Expand(s)
	}
	return out
}

func (e *stepEnv) post(ctx context.Context, m map[string]any) (int, string, error) {
	u := e.sc.Base + e.sc.Expand(fmt.Sprint(m["path"]))
	var body io.Reader
	ctype := ""
	switch {
	case m["json"] != nil:
		b, err := json.Marshal(m["json"])
		if err != nil {
			return 0, "", err
		}
		body, ctype = bytes.NewReader(b), "application/json"
	case m["form"] != nil:
		vals := url.Values{}
		if f, ok := m["form"].(map[string]any); ok {
			for k, v := range f {
				vals.Set(k, fmt.Sprint(v))
			}
		}
		body, ctype = strings.NewReader(vals.Encode()), "application/x-www-form-urlencoded"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return 0, "", err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	e.logf("POST %s → %d", u, resp.StatusCode)
	return resp.StatusCode, string(b), nil
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// CheckResult is one check's outcome.
type CheckResult struct {
	Check  string `yaml:"check" json:"check"`
	Line   int    `yaml:"line" json:"line"`
	Pass   bool   `yaml:"pass" json:"pass"`
	Actual string `yaml:"actual,omitempty" json:"actual,omitempty"`
	Detail string `yaml:"detail,omitempty" json:"detail,omitempty"`
}

// settle evaluates checks until all pass or budget runs out, and returns the
// last evaluation. Checks see the page settle (a navigation, a fetch) the
// same way every run, so the result doesn't depend on timing.
func (e *stepEnv) settle(ctx context.Context, cs []Check, budget time.Duration) []CheckResult {
	deadline := time.Now().Add(budget)
	for {
		res := make([]CheckResult, len(cs))
		all := true
		for i, c := range cs {
			res[i] = e.eval(ctx, c)
			all = all && res[i].Pass
		}
		if all || time.Now().After(deadline) || ctx.Err() != nil {
			return res
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// eval evaluates one check once.
func (e *stepEnv) eval(ctx context.Context, c Check) CheckResult {
	out := CheckResult{Check: c.Label(), Line: c.Line}
	if domChecks[c.Kind] {
		if e.page == nil {
			out.Detail = "no page"
			return out
		}
		var r struct {
			Pass   bool   `json:"pass"`
			Actual string `json:"actual"`
			Detail string `json:"detail"`
		}
		if err := e.page.Call(ctx, &r, "check", c, e.snaps); err != nil {
			out.Detail = err.Error()
			return out
		}
		out.Pass, out.Actual, out.Detail = r.Pass, r.Actual, r.Detail
		return out
	}
	var err error
	out.Pass, out.Actual, err = e.evalRunner(ctx, c)
	if err != nil {
		out.Detail = err.Error()
	}
	return out
}

func (e *stepEnv) evalRunner(ctx context.Context, c Check) (bool, string, error) {
	str := func(k string) string { return e.sc.Expand(fmt.Sprint(c.Arg[k])) }
	switch c.Kind {
	case "issue":
		return e.evalIssue(c)
	case "cli":
		var args []string
		for _, a := range c.Arg["args"].([]any) {
			args = append(args, e.sc.Expand(fmt.Sprint(a)))
		}
		out, code, err := e.sc.CLI(ctx, args...)
		if err != nil {
			return false, "", err
		}
		want := 0
		if c.Arg["exit"] != nil {
			fmt.Sscan(fmt.Sprint(c.Arg["exit"]), &want)
		}
		ok := code == want
		if c.Arg["contains"] != nil {
			ok = ok && strings.Contains(out, str("contains"))
		}
		if c.Arg["not_contains"] != nil {
			ok = ok && !strings.Contains(out, str("not_contains"))
		}
		return ok, fmt.Sprintf("exit %d: %s", code, trim(out)), nil
	case "tmux":
		if c.Arg["session"] != nil {
			_, code, err := e.sc.Tmux(ctx, "has-session", "-t", str("session"))
			if err != nil {
				return false, "", err
			}
			present := code == 0
			return present == (c.Arg["present"] == true), fmt.Sprintf("present=%v", present), nil
		}
		out, code, err := e.sc.Tmux(ctx, "capture-pane", "-p", "-t", str("pane"))
		if err != nil {
			return false, "", err
		}
		if code != 0 {
			return false, trim(out), nil
		}
		return strings.Contains(out, str("contains")), trim(out), nil
	case "http":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.sc.Base+str("path"), nil)
		if err != nil {
			return false, "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		body := string(b)
		ok := true
		if c.Arg["status"] != nil {
			ok = fmt.Sprint(c.Arg["status"]) == fmt.Sprint(resp.StatusCode)
		}
		if c.Arg["contains"] != nil {
			ok = ok && strings.Contains(body, str("contains"))
		}
		if want, isMap := c.Arg["json"].(map[string]any); isMap {
			var doc any
			if err := json.Unmarshal(b, &doc); err != nil {
				return false, trim(body), fmt.Errorf("not JSON: %v", err)
			}
			for path, v := range want {
				got, found := jsonPath(doc, path)
				if !found || fmt.Sprint(got) != fmt.Sprint(v) {
					return false, fmt.Sprintf("%s = %v", path, got), nil
				}
			}
		}
		return ok, fmt.Sprintf("%d %s", resp.StatusCode, trim(body)), nil
	}
	return false, "", fmt.Errorf("not a runner check: %s", c.Kind)
}

// jsonPath reads a dotted path ("issues.0.status") from decoded JSON.
func jsonPath(doc any, path string) (any, bool) {
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		switch v := cur.(type) {
		case map[string]any:
			var ok bool
			if cur, ok = v[seg]; !ok {
				return nil, false
			}
		case []any:
			var i int
			if _, err := fmt.Sscan(seg, &i); err != nil || i < 0 || i >= len(v) {
				return nil, false
			}
			cur = v[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// evalIssue reads an issue file in the scenario project.
func (e *stepEnv) evalIssue(c Check) (bool, string, error) {
	slug := e.sc.Expand(fmt.Sprint(c.Arg["slug"]))
	path, err := e.sc.IssueFile(slug)
	if err != nil {
		return false, "missing", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, "missing", err
	}
	front := map[string]any{}
	raw, err := tracker.ParseFrontmatter(string(data), &front)
	if err != nil {
		return false, "", err
	}
	body, comments := tracker.ParseComments(raw)
	switch {
	case c.Arg["field"] != nil:
		got := front[fmt.Sprint(c.Arg["field"])]
		gotS := ""
		if got != nil {
			gotS = fmt.Sprint(got)
		}
		return gotS == fmt.Sprint(c.Arg["equals"]), gotS, nil
	case c.Arg["checkbox"] != nil:
		ref := fmt.Sprint(c.Arg["checkbox"])
		items := tracker.ListCheckboxes(body)
		item, st := tracker.ResolveCheckboxRef(items, ref)
		if st != tracker.RefFound {
			found := false
			for _, it := range items {
				if strings.Contains(it.Text, ref) {
					item, found = it, true
					break
				}
			}
			if !found {
				return false, "no such checkbox", nil
			}
		}
		return item.Checked == (c.Arg["checked"] == true), fmt.Sprintf("checked=%v", item.Checked), nil
	case c.Arg["comment_contains"] != nil:
		want := fmt.Sprint(c.Arg["comment_contains"])
		var texts []string
		for _, cm := range comments {
			if strings.Contains(cm.Text, want) {
				return true, cm.Text, nil
			}
			texts = append(texts, cm.Text)
		}
		return false, fmt.Sprintf("%d comments: %s", len(comments), trim(strings.Join(texts, " | "))), nil
	case c.Arg["body_contains"] != nil:
		return strings.Contains(body, fmt.Sprint(c.Arg["body_contains"])), "", nil
	}
	return false, "", fmt.Errorf("issue check needs field, checkbox, comment_contains or body_contains")
}

// consoleErrors filters a page's errors through the session's ignore list.
func consoleErrors(all []string, ignore []string) []string {
	var res []*regexp.Regexp
	for _, s := range ignore {
		if re, err := regexp.Compile(s); err == nil {
			res = append(res, re)
		}
	}
	var out []string
next:
	for _, l := range all {
		for _, re := range res {
			if re.MatchString(l) {
				continue next
			}
		}
		out = append(out, l)
	}
	return out
}
