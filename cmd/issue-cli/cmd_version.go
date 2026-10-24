package main

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/milestone"
)

// Version is set at build time via -ldflags "-X main.Version=v0.X.Y".
var Version = "dev"

var versionCommand = &Command{
	Name:      "version",
	ShortHelp: "Print issue-cli version",
	LongHelp: `Print the issue-cli build version: ` + "`git describe`" + ` at build time, set via
-ldflags by make build / make install and the release Action ("dev" for a
plain go build). A release candidate (v0.54.0-rc.1) says so and how to
report a problem against it.`,
	Run: runVersion,
}

func init() {
	registerCommand(versionCommand)
}

func runVersion(ctx *Context, args []string) error {
	if _, err := parsePositionals(ctx, "version", args, 0); err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, "issue-cli", Version+rcNote(Version))
	return nil
}

// rcNote is the version line's tail for a release candidate build: what it
// is and how to report against it. "" for any other build.
func rcNote(v string) string {
	rc, ok := milestone.ParseRC(v)
	if !ok {
		return ""
	}
	what := "release candidate for " + rc.Cut
	if !rc.Exact {
		what = "a build past release candidate " + rc.Tag
	}
	return fmt.Sprintf(" (%s: report problems with issue-cli report-bug \"…\")", what)
}
