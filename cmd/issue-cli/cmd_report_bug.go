package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var reportBugCommand = &Command{
	Name:      "report-bug",
	ShortHelp: "Report a bug in issue-cli itself",
	LongHelp: `Save a bug report under bugs/ in the server root. The current issue slug
is attached automatically when the dispatched session sets ISSUE_VIEWER_ISSUE_SLUG.`,
	Run: runReportBug,
}

func init() {
	registerCommand(reportBugCommand)
}

func runReportBug(ctx *Context, args []string) error {
	fs := newFlagSet("report-bug", ctx)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("report-bug requires a description\n\nExample:\n  issue-cli report-bug \"transition command rejects valid status name with trailing space\"")
	}
	description := strings.Join(rest, " ")

	entry := map[string]interface{}{
		"ts":          ctx.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		"description": description,
		"tool":        "issue-cli",
	}
	if slug := os.Getenv("ISSUE_VIEWER_ISSUE_SLUG"); slug != "" {
		entry["issue_slug"] = slug
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal bug report: %w", err)
	}
	line = append(line, '\n')

	serverRoot := os.Getenv("ISSUE_VIEWER_SERVER_PWD")
	if serverRoot == "" {
		serverRoot, _ = os.Getwd()
	}
	logDir := filepath.Join(serverRoot, "bugs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create bug directory: %w", err)
	}

	logFile, err := openUniqueBugReport(logDir, ctx.Now().UTC().Format("20060102-150405"))
	if err != nil {
		return fmt.Errorf("failed to write bug report: %w", err)
	}
	defer logFile.Close()
	if _, err := logFile.Write(line); err != nil {
		return fmt.Errorf("failed to write bug report: %w", err)
	}

	fmt.Fprintf(ctx.Stdout, "Bug reported to %s\n", logFile.Name())
	return nil
}

// openUniqueBugReport creates a fresh file under logDir for this bug report,
// appending a numeric suffix when the timestamp-named file already exists.
// This replaces the old O_TRUNC-based path, which would silently overwrite a
// concurrent or same-second report.
func openUniqueBugReport(logDir, base string) (*os.File, error) {
	for i := 0; ; i++ {
		name := fmt.Sprintf("%s-issue-cli-bug.json", base)
		if i > 0 {
			name = fmt.Sprintf("%s-%d-issue-cli-bug.json", base, i)
		}
		path := filepath.Join(logDir, name)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
		if err == nil {
			return f, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if i > 1000 {
			return nil, fmt.Errorf("could not allocate a unique bug report filename after 1000 attempts")
		}
	}
}
