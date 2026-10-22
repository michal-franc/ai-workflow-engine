package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

//go:embed assets/checks.js
var checksJS string

// noMotionJS turns off CSS animations and transitions so screenshots and
// layout checks don't depend on timing. ux-test only; the owner's overlay
// shows the real UI.
const noMotionJS = `document.addEventListener('DOMContentLoaded', function () {
  var s = document.createElement('style');
  s.textContent = '*,*::before,*::after{transition:none!important;animation:none!important;caret-color:transparent!important}';
  document.head.appendChild(s);
});`

// chromeCandidates are tried in order when PLAYTEST_CHROME is unset.
var chromeCandidates = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"}

// ChromePath finds the browser to drive.
func ChromePath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("PLAYTEST_CHROME")); p != "" {
		return p, nil
	}
	for _, c := range chromeCandidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no Chrome or Chromium found (tried %s); install one or set PLAYTEST_CHROME=/path/to/chrome", strings.Join(chromeCandidates, ", "))
}

// Browser is one Chrome process. ux-test opens a fresh browser context
// (separate storage, cookies) per step and viewport; the overlay uses the
// browser's own window.
type Browser struct {
	ctx     context.Context
	cancel  context.CancelFunc
	visible bool
	// last is the visible window's newest page: the next session opens a
	// tab of its own, because the overlay bridge can't be added twice.
	last *Page
}

// NewBrowser starts Chrome: headless for ux-test, a visible window for the
// owner's playtest.
func NewBrowser(visible bool, w, h int) (*Browser, error) {
	path, err := ChromePath()
	if err != nil {
		return nil, err
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(path),
		chromedp.WindowSize(w, h),
		chromedp.Flag("hide-scrollbars", true),
	)
	if visible {
		opts = append(opts, chromedp.VisibleWindow, chromedp.Flag("start-maximized", false))
	}
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancelCtx := chromedp.NewContext(alloc)
	b := &Browser{ctx: ctx, visible: visible, cancel: func() { cancelCtx(); cancelAlloc() }}
	if err := chromedp.Do(ctx); err != nil {
		b.cancel()
		return nil, fmt.Errorf("start chrome (%s): %v", path, err)
	}
	return b, nil
}

// Close stops the browser.
func (b *Browser) Close() { b.cancel() }

// Page is one tab the runner drives.
type Page struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	console []string
}

// NewPage opens a tab in a fresh browser context (or, for the visible
// browser, takes its window first and a new tab in it after), sets the
// viewport and installs scripts that run in every document it loads.
func (b *Browser) NewPage(vp *Viewport, scripts ...string) (*Page, error) {
	var ctx context.Context
	var cancel context.CancelFunc
	switch {
	case b.visible && b.last == nil:
		ctx, cancel = context.WithCancel(b.ctx)
	case b.visible:
		// A later session (make playtest over several sessions): its own
		// tab, and the previous one is blanked so its dead overlay can't
		// be mistaken for the live one.
		ctx, cancel = chromedp.NewContext(b.ctx)
		prev := b.last
		defer func() { _ = prev.Do(prev.ctx, chromedp.Navigate("about:blank")) }()
	default:
		ctx, cancel = chromedp.NewContext(b.ctx, chromedp.WithNewBrowserContext())
	}
	p := &Page{ctx: ctx, cancel: cancel}
	steps := []chromedp.Action[chromedp.Void]{chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		for _, src := range scripts {
			if _, err := cdp.Call(ctx, t, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: src}); err != nil {
				return err
			}
		}
		return nil
	})}
	if vp != nil {
		steps = append(steps, chromedp.EmulateViewport(int64(vp.W), int64(vp.H)))
	}
	if err := chromedp.Do(ctx, steps...); err != nil {
		cancel()
		return nil, err
	}
	if b.visible {
		b.last = p
	}
	msgs := chromedp.Console(ctx)
	go func() {
		for m, err := range msgs {
			if err != nil {
				return
			}
			if m.Type != chromedp.ConsoleError && m.Type != chromedp.ConsoleException {
				continue
			}
			line := m.Text
			if m.URL != "" {
				line += fmt.Sprintf(" (%s:%d)", m.URL, m.Line+1)
			}
			p.mu.Lock()
			p.console = append(p.console, line)
			p.mu.Unlock()
		}
	}()
	return p, nil
}

// Close ends the tab (and its browser context).
func (p *Page) Close() { p.cancel() }

// Console returns the errors and exceptions seen so far.
func (p *Page) Console() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.console...)
}

// Do runs chromedp actions on the page.
func (p *Page) Do(ctx context.Context, steps ...chromedp.Action[chromedp.Void]) error {
	ctx, cancel := mergeCtx(p.ctx, ctx)
	defer cancel()
	return chromedp.Do(ctx, steps...)
}

// Screenshot captures the page as PNG.
func (p *Page) Screenshot(ctx context.Context, full bool) ([]byte, error) {
	ctx, cancel := mergeCtx(p.ctx, ctx)
	defer cancel()
	if full {
		return chromedp.Run(ctx, chromedp.FullScreenshot(100))
	}
	return chromedp.Run(ctx, chromedp.CaptureScreenshot())
}

// Call runs window.__pt.<fn>(args…) and decodes the result into out. It
// retries while a navigation replaces the document, up to budget.
func (p *Page) Call(ctx context.Context, out any, fn string, args ...any) error {
	enc := make([]string, len(args))
	for i, a := range args {
		b, err := json.Marshal(a)
		if err != nil {
			return err
		}
		enc[i] = string(b)
	}
	expr := fmt.Sprintf(`(function(){ if (!window.__pt) throw new Error('playtest helpers not loaded (no page open yet?)'); return JSON.stringify(window.__pt.%s(%s)); })()`, fn, strings.Join(enc, ","))
	return p.Eval(ctx, expr, out)
}

// Eval evaluates expr, which must return a JSON string, into out.
func (p *Page) Eval(ctx context.Context, expr string, out any) error {
	ctx, cancel := mergeCtx(p.ctx, ctx)
	defer cancel()
	var last error
	for range 20 {
		s, err := chromedp.Run(ctx, chromedp.Evaluate[string](expr))
		if err == nil {
			if out == nil {
				return nil
			}
			return json.Unmarshal([]byte(s), out)
		}
		last = err
		if !transient(err) || ctx.Err() != nil {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return last
}

// transient reports errors that a navigation in progress causes.
func transient(err error) bool {
	s := err.Error()
	for _, m := range []string{"context was destroyed", "Cannot find context", "Execution context", "Inspected target navigated", "not loaded"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// mergeCtx returns a context that ends when either ends, keeping the
// chromedp values of base.
func mergeCtx(base, other context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(base)
	if other == nil {
		return ctx, cancel
	}
	if dl, ok := other.Deadline(); ok {
		var c2 context.CancelFunc
		ctx, c2 = context.WithDeadline(ctx, dl)
		prev := cancel
		cancel = func() { c2(); prev() }
	}
	stop := context.AfterFunc(other, cancel)
	return ctx, func() { stop(); cancel() }
}
