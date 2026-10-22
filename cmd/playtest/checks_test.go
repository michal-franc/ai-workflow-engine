package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// checksFixture is a page for checks.js's shape checks and the
// styles_applied rule. Its classes cover the rule's cases: plain, ancestor
// context, child combinator, @media either way, @supports either way,
// pseudo-classes, :not, a linked sheet, a js- hook and an ignored id.
const checksFixture = `<!doctype html><html><head>
<link rel="stylesheet" href="/linked.css">
<style>
body { margin: 0; }
.row { display: flex; }
.a { width: 100px; height: 50px; }
.b { width: 200px; height: 50px; margin-left: 30px; }
.c { height: 20px; }
.ctx .inner { color: red; }
.parent > .kid { color: red; }
.x:hover { color: red; }
.y::before { content: ''; }
.p:not(.q) { color: red; }
@media (max-width: 600px) { .narrow-only { color: blue; } .row { display: block; } }
@media (min-width: 601px) { .wide-only { color: blue; } }
@supports (display: grid) { .grid-ok { display: grid; } }
@supports (display: no-such-thing) { .grid-bad { color: red; } }
@keyframes spin { from { opacity: 0; } }
</style></head><body>
<div class="row" data-testid="shape.row">
  <div class="a" data-testid="shape.a">A</div>
  <div class="b" data-testid="shape.b">B</div>
</div>
<div class="c" data-testid="shape.c">C</div>
<div class="ctx" data-testid="styles.ctx"><span class="inner" data-testid="styles.inner">in</span></div>
<div class="parent"><span class="kid" data-testid="styles.kid">kid</span></div>
<div><span class="kid" data-testid="styles.orphan">orphan</span></div>
<div class="x" data-testid="styles.hover">hover</div>
<div class="y" data-testid="styles.before">before</div>
<div class="p q" data-testid="styles.not">not</div>
<div class="narrow-only" data-testid="styles.narrow">narrow</div>
<div class="wide-only" data-testid="styles.wide">wide</div>
<div class="grid-ok" data-testid="styles.grid">grid</div>
<div class="grid-bad" data-testid="styles.nogrid">nogrid</div>
<div class="linked" data-testid="styles.linked">linked</div>
<div class="js-hook" data-testid="styles.hook">hook</div>
<div class="spin" data-testid="styles.spin">spin</div>
<div class="nothing" data-testid="styles.ignored"><span class="nothing-either" data-testid="styles.ignored.child">x</span></div>
<div class="unstyled" data-testid="styles.bare">bare</div>
<div class="unstyled" data-testid="styles.offscreen" style="display:none">gone</div>
</body></html>`

// fixturePage opens the fixture at a viewport with checks.js loaded.
func fixturePage(t *testing.T, w, h int) (*Page, func(fn string, out any, args ...any)) {
	t.Helper()
	if _, err := ChromePath(); err != nil {
		t.Skip(err)
	}
	b, err := NewBrowser(false, w, h)
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(b.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/linked.css" {
			rw.Header().Set("Content-Type", "text/css")
			fmt.Fprint(rw, ".linked { color: green; }")
			return
		}
		fmt.Fprint(rw, checksFixture)
	}))
	t.Cleanup(srv.Close)
	pg, err := b.NewPage(&Viewport{w, h}, checksJS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pg.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	if err := pg.Do(ctx, chromedp.Navigate(srv.URL)); err != nil {
		t.Fatal(err)
	}
	call := func(fn string, out any, args ...any) {
		t.Helper()
		if err := pg.Call(ctx, out, fn, args...); err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
	}
	return pg, call
}

func styleViolations(t *testing.T, call func(string, any, ...any), ignore []string) []string {
	t.Helper()
	var v []LayoutViolation
	call("layout", &v, []string{"styles_applied"}, ignore)
	var got []string
	for _, l := range v {
		if l.Rule != "styles_applied" {
			t.Errorf("unexpected rule %q", l.Rule)
		}
		cls := l.Detail
		if i := strings.Index(cls, `class "`); i >= 0 {
			cls = cls[i+7:]
			cls = cls[:strings.Index(cls, `"`)]
		}
		got = append(got, l.ID+" "+cls)
	}
	sort.Strings(got)
	return got
}

func TestStylesAppliedRule(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want []string
	}{
		{1440, 900, []string{"styles.bare unstyled", "styles.narrow narrow-only", "styles.nogrid grid-bad", "styles.not q", "styles.orphan kid", "styles.spin spin"}},
		{390, 844, []string{"styles.bare unstyled", "styles.nogrid grid-bad", "styles.not q", "styles.orphan kid", "styles.spin spin", "styles.wide wide-only"}},
	} {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			_, call := fixturePage(t, tc.w, tc.h)
			got := styleViolations(t, call, []string{"styles.ignored"})
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("violations:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(tc.want, "\n  "))
			}
		})
	}
}

// TestStylesAppliedDetail: the message names the class, the viewport and
// the ways out; a page with no stylesheet (31 classes here) is cut at
// stylesCap with a count of the rest.
func TestStylesAppliedDetail(t *testing.T) {
	pg, call := fixturePage(t, 1440, 900)
	var v []LayoutViolation
	call("layout", &v, []string{"styles_applied"}, []string{"styles.*"})
	if len(v) != 0 {
		t.Fatalf("shape.* should all be styled, got %+v", v)
	}
	call("layout", &v, []string{"styles_applied"}, []string{"styles.ignored"})
	var bare LayoutViolation
	for _, l := range v {
		if l.ID == "styles.bare" {
			bare = l
		}
	}
	want := `class "unstyled" has no CSS rule active at 1440x900 (add a rule, rename a JS-only hook to js-unstyled, or layout_ignore the id)`
	if bare.Detail != want {
		t.Errorf("detail = %q\nwant %q", bare.Detail, want)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pg.Eval(ctx, `(function(){ document.querySelectorAll('style,link').forEach(function(s){ s.remove(); }); for (var i = 0; i < 10; i++) { var d = document.createElement('div'); d.className = 'extra' + i; d.setAttribute('data-testid', 'extra.' + i); d.textContent = 'x'; document.body.appendChild(d); } return '1'; })()`, nil); err != nil {
		t.Fatal(err)
	}
	call("layout", &v, []string{"styles_applied"}, []string{})
	if len(v) != 26 || v[25].ID != "(page)" || !strings.HasPrefix(v[25].Detail, "6 more classes") {
		t.Errorf("want 25 violations and a (page) count of 6 more, got %d, last %+v", len(v), v[len(v)-1])
	}
}

type checkOut struct {
	Pass   bool   `json:"pass"`
	Actual string `json:"actual"`
}

func TestShapeChecks(t *testing.T) {
	_, call := fixturePage(t, 1440, 900)
	run := func(kind string, arg map[string]any) checkOut {
		var r checkOut
		call("check", &r, map[string]any{"kind": kind, "arg": arg}, map[string]string{})
		return r
	}
	pair := func(a, b string) map[string]any { return map[string]any{"a": a, "b": b} }
	for _, tc := range []struct {
		kind   string
		arg    map[string]any
		pass   bool
		actual string
	}{
		{"beside", pair("shape.a", "shape.b"), true, "shape.a x 0..100 y 0..50, shape.b x 130..330 y 0..50"},
		{"beside", pair("shape.b", "shape.a"), false, ""},
		{"beside", pair("shape.a", "shape.c"), false, ""},
		{"beside", map[string]any{"a": "shape.a", "b": "shape.b", "gap_max": 40}, true, ""},
		{"beside", map[string]any{"a": "shape.a", "b": "shape.b", "gap_max": 10}, false, ""},
		{"beside", pair("shape.a", "shape.nope"), false, "shape.nope missing"},
		{"beside", pair("styles.offscreen", "shape.a"), false, "styles.offscreen hidden"},
		{"below", pair("shape.a", "shape.c"), true, "shape.a x 0..100 y 0..50, shape.c x 0..1440 y 50..70"},
		{"below", pair("shape.c", "shape.a"), false, ""},
		{"below", pair("shape.a", "shape.b"), false, ""},
		{"css", map[string]any{"target": "shape.row", "property": "display", "equals": "flex"}, true, "flex"},
		{"css", map[string]any{"target": "shape.row", "property": "display", "equals": "block"}, false, "flex"},
		{"css", map[string]any{"target": "shape.row", "property": "display", "in": []any{"grid", "flex"}}, true, ""},
		{"css", map[string]any{"target": "shape.row", "property": "display", "in": []any{"grid"}}, false, ""},
		{"css", map[string]any{"target": "shape.row", "property": "display", "not": "flex"}, false, ""},
		{"css", map[string]any{"target": "shape.b", "property": "margin-left", "not": "0px"}, true, "30px"},
		{"css", map[string]any{"target": "shape.nope", "property": "display", "not": "none"}, false, "missing"},
		{"size", map[string]any{"target": "shape.a", "min_width": 100, "max_width": 100}, true, "100x50"},
		{"size", map[string]any{"target": "shape.a", "min_width": 101}, false, "100x50"},
		{"size", map[string]any{"target": "shape.a", "max_height": 49}, false, ""},
		{"size", map[string]any{"target": "shape.a", "min_height": 50, "max_height": 50.5}, true, ""},
		{"size", map[string]any{"target": "shape.nope", "min_width": 1}, false, "missing"},
	} {
		got := run(tc.kind, tc.arg)
		if got.Pass != tc.pass || (tc.actual != "" && got.Actual != tc.actual) {
			t.Errorf("%s %v = %+v, want pass=%v actual=%q", tc.kind, tc.arg, got, tc.pass, tc.actual)
		}
	}
}

// TestShapeChecksFollowMedia: at phone width the fixture's row stacks, and
// the checks see it.
func TestShapeChecksFollowMedia(t *testing.T) {
	_, call := fixturePage(t, 390, 844)
	var r checkOut
	call("check", &r, map[string]any{"kind": "beside", "arg": map[string]any{"a": "shape.a", "b": "shape.b"}}, map[string]string{})
	if r.Pass {
		t.Errorf("beside at 390 = %+v, want fail (row stacks)", r)
	}
	call("check", &r, map[string]any{"kind": "below", "arg": map[string]any{"a": "shape.a", "b": "shape.b"}}, map[string]string{})
	if !r.Pass {
		t.Errorf("below at 390 = %+v, want pass", r)
	}
}
