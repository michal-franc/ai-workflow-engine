package main

var initCommand = &Command{
	Name:      "init",
	ShortHelp: "Pick a workflow template and scaffold a new project (alias for `workflow init`)",
	LongHelp: `Bootstrap a new project in the current directory.

Writes workflow.yaml from a bundled template (development, review, writing) and
creates issues/ and docs/ if they don't exist.

Usage:
  issue-cli init                       # interactive picker
  issue-cli init --template <name>     # pick non-interactively
  issue-cli init --force               # overwrite an existing workflow.yaml`,
	Run: runWorkflowInit,
}

func init() {
	registerCommand(initCommand)
}
