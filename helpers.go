package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func projectRoot(proj *tracker.Project) string {
	if proj != nil && proj.WorkDir != "" {
		return proj.WorkDir
	}
	if proj != nil && proj.IssueDir != "" {
		if abs, err := filepath.Abs(proj.IssueDir); err == nil {
			return filepath.Dir(abs)
		}
		return filepath.Dir(proj.IssueDir)
	}
	cwd, _ := os.Getwd()
	return cwd
}

func resolveProjectWorkDir(proj *tracker.Project) string {
	if proj != nil && proj.WorkDir != "" {
		return proj.WorkDir
	}
	workDir, _ := os.Getwd()
	return workDir
}

func workflowFileTarget(proj *tracker.Project) string {
	if proj.WorkflowFile != "" {
		return proj.WorkflowFile
	}
	return "workflow.yaml"
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func valueOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func trimSnippet(s string, max int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
