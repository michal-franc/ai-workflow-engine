package main

var helpCommand = &Command{
	Name:      "help",
	ShortHelp: "Show CLI help (or details on a topic)",
	LongHelp: `Show top-level help, or details on a topic.

Examples:
  issue-cli help
  issue-cli help workflow`,
	Run: runHelp,
}

func init() {
	registerCommand(helpCommand)
}

func runHelp(ctx *Context, args []string) error {
	if len(args) > 0 {
		return runProcess(ctx, args)
	}
	return printHelp(ctx.Stdout, ctx.AllProjects, ctx.ProjectSlug)
}
