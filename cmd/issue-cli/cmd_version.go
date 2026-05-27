package main

import "fmt"

// Version is set at build time via -ldflags "-X main.Version=v0.X.Y".
var Version = "dev"

var versionCommand = &Command{
	Name:      "version",
	ShortHelp: "Print issue-cli version",
	LongHelp:  "Print the issue-cli build version. Set at release time via -ldflags.",
	Run:       runVersion,
}

func init() {
	registerCommand(versionCommand)
}

func runVersion(ctx *Context, args []string) error {
	fmt.Fprintln(ctx.Stdout, "issue-cli", Version)
	return nil
}
