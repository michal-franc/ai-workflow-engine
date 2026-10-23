package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/michal-franc/issue-viewer/internal/viewerstate"
)

func TestNewHTTPServer_SetsTimeouts(t *testing.T) {
	hs := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if hs.ReadHeaderTimeout != 5*time.Second || hs.ReadTimeout != 30*time.Second ||
		hs.WriteTimeout != 60*time.Second || hs.IdleTimeout != 120*time.Second {
		t.Fatalf("timeouts = header %s, read %s, write %s, idle %s; want 5s, 30s, 60s, 120s",
			hs.ReadHeaderTimeout, hs.ReadTimeout, hs.WriteTimeout, hs.IdleTimeout)
	}
}

func TestServe_ShutdownFinishesInFlightRequestAndBackgroundWork(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		io.WriteString(w, "done")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bg := newBackground(ctx)
	var bgFinished atomic.Bool
	bg.Go("slow work", func() {
		<-bg.ctx.Done()
		time.Sleep(100 * time.Millisecond)
		bgFinished.Store(true)
	})

	serveErr := make(chan error, 1)
	go func() { serveErr <- serve(ctx, newHTTPServer(ln.Addr().String(), h), ln, bg) }()

	body := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err != nil {
			body <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		body <- string(b)
	}()

	<-started
	cancel()

	if got := <-body; got != "done" {
		t.Fatalf("in-flight request got %q, want done", got)
	}
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("serve returned %v, want nil after shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after shutdown")
	}
	if !bgFinished.Load() {
		t.Fatal("serve returned before background work finished")
	}
}

func TestServe_ReturnsListenerError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	err = serve(context.Background(), newHTTPServer("", http.NotFoundHandler()), ln, newBackground(context.Background()))
	if err == nil {
		t.Fatal("serve on a closed listener returned nil")
	}
}

func TestRun_Version(t *testing.T) {
	if err := run([]string{"-version"}); err != nil {
		t.Fatalf("run -version = %v", err)
	}
}

func TestRun_InvalidDirReturnsError(t *testing.T) {
	err := run([]string{"-dir", "/nonexistent/issue-viewer-test"})
	if err == nil || !strings.Contains(err.Error(), "not a valid directory") {
		t.Fatalf("run = %v, want a not-a-valid-directory error", err)
	}
}

func TestRun_PortInUseReturnsError(t *testing.T) {
	proj, _ := setupTestProject(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	if err := run([]string{"-dir", proj.IssueDir, "-docs", proj.DocsDir, "-port", port}); err == nil {
		t.Fatal("run on a busy port returned nil")
	}
}

func TestRun_SIGINTShutsDownCleanly(t *testing.T) {
	proj, _ := setupTestProject(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()

	runErr := make(chan error, 1)
	go func() { runErr <- run([]string{"-dir", proj.IssueDir, "-docs", proj.DocsDir, "-port", port}) }()

	// run registers the signal handler before it listens, so once the port
	// answers SIGINT goes to it, not to the test binary's default handler.
	url := "http://127.0.0.1:" + port + "/"
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			break
		}
		select {
		case err := <-runErr:
			t.Fatalf("run exited before serving: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("run after SIGINT = %v, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not return after SIGINT")
	}
}

func TestRun_ConfigModeRecordsViewerForCLI(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("ISSUE_VIEWER_CONFIG", "") // run sets it; restored after the test
	proj, _ := setupTestProject(t)
	cfg := filepath.Join(t.TempDir(), "projects.yaml")
	body := "projects:\n  - name: T\n    slug: t\n    issues: " + proj.IssueDir + "\n    docs: " + proj.DocsDir + "\n"
	if err := os.WriteFile(cfg, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()

	runErr := make(chan error, 1)
	go func() { runErr <- run([]string{"-config", cfg, "-port", port}) }()

	var state viewerstate.State
	deadline := time.Now().Add(5 * time.Second)
	for {
		if state, err = viewerstate.Read(); err == nil {
			break
		}
		select {
		case err := <-runErr:
			t.Fatalf("run exited before recording the viewer: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("viewer.json never written: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if state.Config != cfg || state.URL != "http://127.0.0.1:"+port || state.PID != os.Getpid() || state.Started.IsZero() {
		t.Fatalf("viewer.json = %+v, want config %s on port %s", state, cfg, port)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runErr:
	case <-time.After(15 * time.Second):
		t.Fatal("run did not return after SIGINT")
	}
}
