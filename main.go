package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func main() {
	configFile := flag.String("config", "", "Path to projects.yaml config file (multi-project mode)")
	dir := flag.String("dir", "./issues", "Directory containing issue markdown files (single-project mode)")
	docsDir := flag.String("docs", "./docs", "Directory containing documentation markdown files (single-project mode)")
	port := flag.Int("port", 8080, "Port to listen on")
	flag.Parse()

	var projects []tracker.Project

	if *configFile != "" {
		var err error
		projects, err = tracker.LoadProjects(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		fmt.Printf("Loaded %d projects from %s\n", len(projects), *configFile)
	} else {
		info, err := os.Stat(*dir)
		if err != nil || !info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: %s is not a valid directory\n", *dir)
			os.Exit(1)
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
		log.Fatalf("Failed to initialize server: %v", err)
	}

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("Issue Viewer running at http://localhost%s\n", addr)
	for _, p := range projects {
		fmt.Printf("  Project: %s (issues: %s, docs: %s)\n", p.Name, p.IssueDir, p.DocsDir)
	}

	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
