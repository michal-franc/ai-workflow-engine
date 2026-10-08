package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// setupWorktreeProject is a test project whose workflow enables worktrees, so
// prompts carry the worktree section and any accidental worktree creation is
// visible on disk.
func setupWorktreeProject(t *testing.T) (tracker.Project, string) {
	t.Helper()
	proj, tmpDir := setupTestProject(t)
	wfPath := filepath.Join(tmpDir, "workflow.yaml")
	if err := os.WriteFile(wfPath, []byte("worktree: true\nstatuses:\n  - name: \"in progress\"\n    prompt: \"Implement it.\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	proj.WorkflowFile = wfPath
	proj.WorkDir = tmpDir
	return proj, tmpDir
}

func getResp(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

func TestHandleDispatchPrompt_MatchesDispatchAndHasNoSideEffects(t *testing.T) {
	proj, tmpDir := setupWorktreeProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, got := getResp(t, ts.URL+"/p/test-project/issue/bug-in-login/dispatch-prompt")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, ".worktrees")); !os.IsNotExist(err) {
		t.Fatalf("GET created a worktree dir (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, ".agent-logs")); !os.IsNotExist(err) {
		t.Fatalf("GET created .agent-logs (err=%v)", err)
	}

	var dispatched string
	origDispatch := dispatchAgentSession
	dispatchAgentSession = func(_ *tracker.Project, session, prompt, _, _, _ string, _ *tracker.WorkflowConfig) DispatchResponse {
		dispatched = prompt
		return DispatchResponse{Status: "dispatched", Prompt: prompt, Session: session}
	}
	t.Cleanup(func() { dispatchAgentSession = origDispatch })
	post, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login/dispatch", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	post.Body.Close()

	if got != dispatched {
		t.Fatalf("GET prompt differs from the prompt POST /dispatch sends\nGET:  %q\nPOST: %q", got, dispatched)
	}
	for _, want := range []string{"You have been assigned this issue: bug-in-login", "Implement it.", "## Worktree", "issue-cli --project test-project "} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestHandleDispatchPrompt_JSON(t *testing.T) {
	proj, tmpDir := setupWorktreeProject(t)
	proj.TmuxSession = "work"
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, body := getResp(t, ts.URL+"/p/test-project/issue/bug-in-login/dispatch-prompt?format=json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var dp tracker.DispatchPrompt
	if err := json.Unmarshal([]byte(body), &dp); err != nil {
		t.Fatalf("invalid JSON %q: %v", body, err)
	}
	want := tracker.DispatchPrompt{
		Slug:     "bug-in-login",
		Status:   "in progress",
		Worktree: filepath.Join(tmpDir, ".worktrees", "bug-in-login"),
		Branch:   "work/bug-in-login",
		Session:  "work:agent-bug-in-login",
	}
	prompt := dp.Prompt
	dp.Prompt = ""
	if dp != want {
		t.Fatalf("JSON = %+v, want %+v", dp, want)
	}
	if !strings.Contains(prompt, "You have been assigned this issue: bug-in-login") {
		t.Fatalf("JSON prompt = %q", prompt)
	}
}

func TestHandleDispatchPrompt_UnknownIssue404(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, _ := getResp(t, ts.URL+"/p/test-project/issue/nope/dispatch-prompt")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func postReprompt(t *testing.T, url string) (*http.Response, DispatchResponse) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var dr DispatchResponse
	if resp.StatusCode != http.StatusNotFound {
		if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
			t.Fatalf("decode reprompt response: %v", err)
		}
	}
	return resp, dr
}

func TestHandleDispatchReprompt_NoSessionIs409AndCreatesNothing(t *testing.T) {
	proj, tmpDir := setupWorktreeProject(t)
	withMockTmuxHasSession(t, func(string) bool { return false })
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, dr := postReprompt(t, ts.URL+"/p/test-project/issue/bug-in-login/dispatch/reprompt")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if dr.Status != "no-session" || len(dr.Steps) != 1 || !strings.Contains(dr.Steps[0].Detail, "/p/test-project/issue/bug-in-login/dispatch") {
		t.Fatalf("response = %+v, want no-session with a dispatch hint", dr)
	}
	for _, dir := range []string{".worktrees", ".agent-logs"} {
		if _, err := os.Stat(filepath.Join(tmpDir, dir)); !os.IsNotExist(err) {
			t.Fatalf("reprompt without a session created %s (err=%v)", dir, err)
		}
	}
}

func TestHandleDispatchReprompt_UnknownIssue404(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, _ := postReprompt(t, ts.URL+"/p/test-project/issue/nope/dispatch/reprompt")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestHandleDispatchReprompt_PastesIntoLiveSession runs a real (private) tmux
// session with cat standing in for the agent: the pasted prompt is echoed in
// the pane, so the delivery check sees it.
func TestHandleDispatchReprompt_PastesIntoLiveSession(t *testing.T) {
	isolateTmux(t)
	withPromptDeliveryTimeout(t, 5*time.Second)
	proj, tmpDir := setupTestProject(t)
	proj.WorkDir = tmpDir
	session := tmuxSessionName("bug-in-login")
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", session, "-x", "200", "-y", "50", "cat").CombinedOutput(); err != nil {
		t.Fatalf("start stand-in agent: %v: %s", err, out)
	}
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, dr := postReprompt(t, ts.URL+"/p/test-project/issue/bug-in-login/dispatch/reprompt")
	if resp.StatusCode != http.StatusOK || dr.Status != "reprompted" {
		t.Fatalf("status = %d / %q, want 200 / reprompted; steps=%+v", resp.StatusCode, dr.Status, dr.Steps)
	}
	last := dr.Steps[len(dr.Steps)-1]
	if last.Name != "Prompt delivered" || last.Status != "ok" {
		t.Fatalf("last step = %+v, want Prompt delivered ok; steps=%+v", last, dr.Steps)
	}
	saved, err := os.ReadFile(filepath.Join(tmpDir, ".agent-logs", session, "dispatch-prompt.txt"))
	if err != nil || string(saved) != dr.Prompt {
		t.Fatalf("saved prompt mismatch (err=%v)", err)
	}
	if out, _ := exec.Command("tmux", "list-buffers", "-F", "#{buffer_name}").Output(); strings.Contains(string(out), "issue-viewer-") {
		t.Fatalf("named paste buffer left behind: %q", out)
	}
}

func TestStartAgentSession_LaunchesWithPromptArgumentAndVerifiesDelivery(t *testing.T) {
	isolateTmux(t)
	withPromptDeliveryTimeout(t, 5*time.Second)
	proj := &tracker.Project{Slug: "test", Terminal: "none", WorkDir: t.TempDir()}
	prompt := "You have been assigned this issue: x\n\nbody line\nlast line of the briefing\n"

	// echo stands in for the agent: it prints its argument, as Claude renders
	// the prompt it was launched with.
	resp := startAgentSession(proj, "agent-x", prompt, "x", "echo", "", nil)
	if resp.Status != "dispatched" {
		t.Fatalf("Status = %q; steps=%+v", resp.Status, resp.Steps)
	}
	var launched, delivered bool
	for _, step := range resp.Steps {
		if strings.Contains(step.Name, "Paste") || strings.Contains(step.Name, "buffer") {
			t.Errorf("launch must not paste; got step %+v", step)
		}
		if step.Name == "Start echo (interactive, prompt as argument)" && step.Status == "ok" {
			launched = true
		}
		if step.Name == "Prompt delivered" && step.Status == "ok" {
			delivered = true
		}
	}
	if !launched || !delivered {
		t.Fatalf("launched=%v delivered=%v; steps=%+v", launched, delivered, resp.Steps)
	}
}

func TestStartAgentSession_ReportsUndeliveredPromptWithoutResending(t *testing.T) {
	isolateTmux(t)
	proj := &tracker.Project{Slug: "test", Terminal: "none", WorkDir: t.TempDir()}

	// true prints nothing, like a Claude that dropped its prompt.
	resp := startAgentSession(proj, "agent-y", "never shown marker line\n", "y", "true", "", nil)
	if resp.Status != "dispatched" {
		t.Fatalf("Status = %q, want dispatched (the session is still up); steps=%+v", resp.Status, resp.Steps)
	}
	last := resp.Steps[len(resp.Steps)-1]
	if last.Name != "Prompt not delivered" || last.Status != "error" {
		t.Fatalf("last step = %+v, want Prompt not delivered error", last)
	}
	if !strings.Contains(last.Detail, "nothing was re-sent") || !strings.Contains(last.Detail, "/p/test/issue/y/dispatch/reprompt") {
		t.Fatalf("detail = %q, want no-resend note and reprompt hint", last.Detail)
	}
}

func TestPromptDeliveryStep_PollsUntilMarkerAppears(t *testing.T) {
	withPromptDeliveryTimeout(t, 3*time.Second)
	original := tmuxCapturePane
	t.Cleanup(func() { tmuxCapturePane = original })
	calls := 0
	tmuxCapturePane = func(string) (string, error) {
		calls++
		if calls < 2 {
			return "❯ \n", nil
		}
		return "  last line\n● working\n", nil
	}

	var steps []DispatchStep
	promptDeliveryStep(&steps, "t", "first\nlast line\n", "")
	if len(steps) != 1 || steps[0].Name != "Prompt delivered" || calls != 2 {
		t.Fatalf("steps=%+v calls=%d", steps, calls)
	}
}

func TestPromptDeliveryStep_NoRepromptURLSuggestsManualPaste(t *testing.T) {
	original := tmuxCapturePane
	t.Cleanup(func() { tmuxCapturePane = original })
	tmuxCapturePane = func(string) (string, error) { return "", nil }

	var steps []DispatchStep
	promptDeliveryStep(&steps, "t", "marker\n", "")
	if len(steps) != 1 || steps[0].Status != "error" || !strings.Contains(steps[0].Detail, "by hand") {
		t.Fatalf("steps=%+v", steps)
	}
}

func TestRepromptURL_OnlyForTheIssuesMainSession(t *testing.T) {
	proj := &tracker.Project{Slug: "p"}
	if got := repromptURL(proj, tmuxSessionName("a/b"), "a/b"); got != "/p/p/issue/a/b/dispatch/reprompt" {
		t.Fatalf("main session URL = %q", got)
	}
	if got := repromptURL(proj, tmuxSessionName("a/b-act"), "a/b"); got != "" {
		t.Fatalf("custom action session URL = %q, want empty", got)
	}
	if got := repromptURL(proj, "agent-retros", ""); got != "" {
		t.Fatalf("no-issue URL = %q, want empty", got)
	}
}
