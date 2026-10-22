package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestBackgroundWait_ReturnsWhenWorkFinishes(t *testing.T) {
	bg := newBackground(context.Background())
	bg.Go("quick", func() { time.Sleep(20 * time.Millisecond) })
	deadline, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if left := bg.Wait(deadline); left != nil {
		t.Fatalf("Wait = %v, want nothing left", left)
	}
}

func TestBackgroundWait_NamesStragglersAndCancelsHard(t *testing.T) {
	bg := newBackground(context.Background())
	stopped := make(chan struct{})
	bg.Go("stuck", func() {
		<-bg.hard.Done()
		close(stopped)
	})
	bg.Go("quick", func() {})

	deadline, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond) // let quick finish
	if left := bg.Wait(deadline); !reflect.DeepEqual(left, []string{"stuck"}) {
		t.Fatalf("Wait = %v, want [stuck]", left)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("hard context was not cancelled at the deadline")
	}
}

func TestBackgroundWait_NothingRunning(t *testing.T) {
	bg := newBackground(context.Background())
	if left := bg.Wait(context.Background()); left != nil {
		t.Fatalf("Wait = %v, want nil", left)
	}
}

func TestCloseGithubIssue_KilledByContext(t *testing.T) {
	home := t.TempDir()
	tokenDir := filepath.Join(home, "Work", "michal-franc-agent")
	binDir := t.TempDir()
	writeScript(t, filepath.Join(tokenDir, "gh-app-token"), "echo token")
	writeScript(t, filepath.Join(binDir, "gh"), "exec sleep 30")
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		closeGithubIssue(ctx, &tracker.Issue{Title: "t", Repo: "o/r", Number: 7})
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("closeGithubIssue ignored its context")
	}
}

func TestHandleProjectList_LogsLoadErrorsWithSlug(t *testing.T) {
	proj, _ := setupTestProject(t)
	missing := tracker.Project{Name: "Missing", Slug: "missing", IssueDir: "/nonexistent/issues", DocsDir: proj.DocsDir}

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	ts := newTestServer(t, []tracker.Project{proj, missing})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if got := buf.String(); !strings.Contains(got, "project list: missing: load issues:") {
		t.Fatalf("log = %q, want the missing project's load error with its slug", got)
	}
}

func TestAwaitBodyEdit_ShutdownKillsSessionAndRecordsComment(t *testing.T) {
	isolateTmux(t)
	proj, _ := setupTestProject(t)
	issuePath := filepath.Join(proj.IssueDir, "bug-in-login.md")
	session := "agent-bug-in-login-edit"
	if err := exec.Command("tmux", "new-session", "-d", "-s", session, "sleep 60").Run(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	bg := newBackground(ctx)
	cleaned := false
	bg.Go("nvim edit", func() {
		awaitBodyEdit(bg, bodyEdit{
			proj:       &proj,
			session:    session,
			waitSignal: session + "-done",
			issuePath:  issuePath,
			cleanup:    func() { cleaned = true },
		})
	})
	time.Sleep(100 * time.Millisecond) // let tmux wait-for start
	cancel()

	deadline, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if left := bg.Wait(deadline); left != nil {
		t.Fatalf("edit watcher still running after shutdown: %v", left)
	}
	if exec.Command("tmux", "has-session", "-t", session).Run() == nil {
		t.Fatal("edit session still alive after shutdown")
	}
	if !cleaned {
		t.Fatal("temp files not cleaned up")
	}
	data, err := os.ReadFile(issuePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "edit-in-nvim aborted: server shut down; nvim session closed, changes discarded") {
		t.Fatalf("issue has no shutdown comment:\n%s", data)
	}
}

func TestAwaitBodyEdit_WritesBackWhenEditorExits(t *testing.T) {
	isolateTmux(t)
	proj, _ := setupTestProject(t)
	issuePath := filepath.Join(proj.IssueDir, "bug-in-login.md")
	base, err := os.ReadFile(issuePath)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	editPath := filepath.Join(dir, "edit.md")
	statusPath := filepath.Join(dir, "status")
	edited := strings.Replace(string(base), "\n---\n", "\n---\nEdited in nvim.\n", 1)
	if err := os.WriteFile(editPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statusPath, []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	session := "agent-bug-in-login-edit"
	if err := exec.Command("tmux", "new-session", "-d", "-s", session, "sleep 60").Run(); err != nil {
		t.Fatal(err)
	}

	bg := newBackground(context.Background())
	bg.Go("nvim edit", func() {
		awaitBodyEdit(bg, bodyEdit{
			proj:       &proj,
			session:    session,
			waitSignal: session + "-done",
			editPath:   editPath,
			statusPath: statusPath,
			issuePath:  issuePath,
			baseHash:   sha256.Sum256(base),
			cleanup:    func() {},
		})
	})
	time.Sleep(100 * time.Millisecond) // let tmux wait-for start
	if err := exec.Command("tmux", "wait-for", "-S", session+"-done").Run(); err != nil {
		t.Fatal(err)
	}

	deadline, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if left := bg.Wait(deadline); left != nil {
		t.Fatalf("edit watcher still running: %v", left)
	}
	data, err := os.ReadFile(issuePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Edited in nvim.") {
		t.Fatalf("edit not written back:\n%s", data)
	}
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestWatchCI_StopsOnShutdown(t *testing.T) {
	s := &Server{}
	ctx, cancel := context.WithCancel(context.Background())
	bg := newBackground(ctx)
	bg.Go("ci watcher", func() { s.watchCI(ctx.Done()) })
	cancel()
	deadline, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if left := bg.Wait(deadline); left != nil {
		t.Fatalf("still running after shutdown: %v", left)
	}
}
