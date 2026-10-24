package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/michal-franc/issue-viewer/internal/proof" // registers the criteria_proven check for transition previews
	"github.com/michal-franc/issue-viewer/internal/tracker"
	"github.com/michal-franc/issue-viewer/internal/viewerstate"
)

// Version is set at build time via -ldflags "-X main.Version=v0.X.Y".
var Version = "dev"

const (
	// readHeaderTimeout bounds a client trickling headers (Slowloris).
	readHeaderTimeout = 5 * time.Second
	// readTimeout covers the body too; uploads are the largest requests.
	readTimeout = 30 * time.Second
	// writeTimeout bounds a whole handler; the slowest shell out to gh.
	writeTimeout = 60 * time.Second
	// idleTimeout closes keep-alive connections the board's polling left open.
	idleTimeout = 120 * time.Second
	// shutdownGrace is how long a signal waits for in-flight requests and
	// background work before the process exits anyway.
	shutdownGrace = 10 * time.Second
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "issue-viewer:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("issue-viewer", flag.ContinueOnError)
	configFile := fs.String("config", "", "Path to projects.yaml config file (multi-project mode)")
	dir := fs.String("dir", "./issues", "Directory containing issue markdown files (single-project mode)")
	docsDir := fs.String("docs", "./docs", "Directory containing documentation markdown files (single-project mode)")
	host := fs.String("host", defaultHost, "Interface to listen on; anything but loopback exposes agent dispatch to the network")
	port := fs.Int("port", 8080, "Port to listen on")
	versionFlag := fs.Bool("version", false, "Print version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *versionFlag {
		fmt.Println("issue-viewer", Version)
		return nil
	}

	var projects []tracker.Project
	var configuredHosts []string
	absConfig, engine := "", ""

	if *configFile != "" {
		cfg, err := tracker.LoadProjectsConfig(*configFile)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		projects, configuredHosts, engine = cfg.Projects, configHosts(cfg), engineSlug(cfg)
		// Make the config path discoverable to dispatched bot sessions so the
		// CLI doesn't fall back to its hard-coded "projects.yaml" default when
		// the viewer was launched with a differently-named config.
		absConfig = *configFile
		if abs, absErr := filepath.Abs(*configFile); absErr == nil {
			absConfig = abs
		}
		os.Setenv("ISSUE_VIEWER_CONFIG", absConfig)
		fmt.Printf("Loaded %d projects from %s\n", len(projects), *configFile)
	} else {
		info, err := os.Stat(*dir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("%s is not a valid directory", *dir)
		}
		projects = []tracker.Project{{
			Name:     "Issues",
			Slug:     "default",
			IssueDir: *dir,
			DocsDir:  *docsDir,
		}}
	}

	srv, err := NewServer(projects)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}
	srv.allowedHosts = allowedHostsFor(*host, configuredHosts)
	if engine != "" {
		srv.engine = engine
	}

	// The first signal starts a graceful shutdown; stop() then restores the
	// default handling, so a second one kills the process straight away.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)
	srv.bg = newBackground(ctx)

	addr := listenAddr(*host, *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	// Local CI: start runs when a project's CI branch moves (ci_watch.go).
	// Needs the config, which the detached issue-cli runs read.
	if *configFile != "" {
		srv.bg.Go("ci watcher", func() { srv.watchCI(ctx.Done()) })
	}
	// Project health: a snapshot a day per project (health_watch.go).
	srv.bg.Go("health snapshots", func() { srv.watchHealth(ctx.Done()) })
	// Private trackers: commit, pull and push the work files (tracker_watch.go).
	srv.bg.Go("tracker sync", func() { srv.watchTrackers(ctx.Done()) })
	// The stats page's aggregate and CLI usage, built before the first
	// visit (stats_cache.go).
	srv.bg.Go("stats warm-up", func() { srv.warmStats(ctx.Done()) })

	url := "http://" + listenAddr(displayHost(*host), *port)
	// Shells and leads the viewer did not start have no ISSUE_VIEWER_CONFIG;
	// viewer.json is how issue-cli finds this config from any folder.
	if absConfig != "" {
		state := viewerstate.State{Config: absConfig, URL: url, PID: os.Getpid(), Started: time.Now()}
		if err := viewerstate.Write(state); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not record the viewer for issue-cli in %s: %v\n", viewerstate.Path(), err)
		}
	}

	fmt.Printf("Issue Viewer running at %s\n", url)
	if len(projects) > 0 && projects[0].ViewerURL != "" {
		fmt.Printf("Links and issue-cli use viewer_url %s\n", projects[0].ViewerURL)
	}
	if !isLoopbackHost(*host) {
		fmt.Fprintf(os.Stderr, "WARNING: listening on %s, not loopback: anyone who can reach it can dispatch agents and edit issues\n", addr)
		fmt.Fprintf(os.Stderr, "WARNING: answering Host %s; other names get 421 (add them under allowed_hosts: in projects.yaml)\n", hostList(srv.allowedHosts))
	}
	for _, p := range projects {
		fmt.Printf("  Project: %s (issues: %s, docs: %s)\n", p.Name, p.IssueDir, p.DocsDir)
	}
	fmt.Printf("Ctrl-C to stop (waits up to %s for in-flight work; Ctrl-C again to force)\n", shutdownGrace)

	return serve(ctx, newHTTPServer(addr, srv.Routes()), ln, srv.bg)
}

func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// serve runs hs on ln until ctx is done, then shuts down: in-flight requests
// and bg work share one shutdownGrace deadline, and whatever is still running
// when it passes is logged and abandoned.
func serve(ctx context.Context, hs *http.Server, ln net.Listener, bg *background) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- hs.Serve(ln) }()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	log.Printf("shutting down (up to %s; signal again to force)", shutdownGrace)
	graceCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()

	if err := hs.Shutdown(graceCtx); err != nil {
		log.Printf("shutdown: requests still in flight after %s, closing them", shutdownGrace)
		hs.Close()
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	if left := bg.Wait(graceCtx); len(left) > 0 {
		log.Printf("shutdown: still running after %s: %s", shutdownGrace, strings.Join(left, ", "))
		return nil
	}
	log.Printf("shut down cleanly")
	return nil
}
