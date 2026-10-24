package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/health"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// healthURL is the viewer's Health view for the project.
func healthURL(proj *tracker.Project) string {
	return viewerBaseURL(proj) + "/p/" + proj.Slug + "/health"
}

// reviewSession is the review agent's tmux name.
func reviewSession(slug string) string { return "review-" + slug }

const healthReviewUsage = `Usage:
  issue-cli health review [--spawn --quote "<the human's words>" [--how "lead chat"] [--attach] [--agent claude|codex]]
  issue-cli health review --schema
  issue-cli health review show
  issue-cli health review record <file.json> [--reviewer <name>]
  issue-cli health review file <R#> --quote "<owner's words>" [--how "lead chat"]
  issue-cli health review dismiss <R#> --quote "<owner's words>" [--reason "..."]`

// runHealthReview is `issue-cli health review`: the agent opinion.
func runHealthReview(ctx *Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "record":
			return runReviewRecord(ctx, args[1:])
		case "file":
			return runReviewFile(ctx, args[1:])
		case "dismiss":
			return runReviewDismiss(ctx, args[1:])
		case "show":
			return runReviewShow(ctx, args[1:])
		}
	}
	fs := newFlagSet("health review", ctx)
	spawn := fs.Bool("spawn", false, "start a review agent in tmux (review-<project>) with the brief")
	attach := fs.Bool("attach", false, "with --spawn: open a terminal on it (default: headless)")
	agent := fs.String("agent", "claude", "with --spawn: claude or codex")
	schema := fs.Bool("schema", false, "print the JSON schema of the file review record takes")
	quoteFlag := fs.String("quote", "", "with --spawn: the human's words that ask for the review (required)")
	howFlag := fs.String("how", "", "with --spawn: how the words reached you")
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &usageError{msg: fmt.Sprintf("unknown health review subcommand %q\n\n%s", fs.Arg(0), healthReviewUsage)}
	}
	quote := strings.TrimSpace(normalizeEscapedText(*quoteFlag))
	how := strings.TrimSpace(*howFlag)
	if *spawn {
		issues, err := tracker.LoadIssues(ctx.Project.IssueDir)
		if err != nil {
			return fmt.Errorf("cannot load issues: %w", err)
		}
		me := whoAmI(ctx.Project, issues)
		if me.Role == tracker.RoleWorker {
			return fmt.Errorf("workers can't start agents (you are %s, detected from %s). Ask your lead on your issue trail:\n  %s", valueOr(me.Name, "a worker"), me.Signal,
				tracker.InjectProject(ctx.Project, fmt.Sprintf("issue-cli comment %s \"question: ...\"", valueOr(me.Slug, "<slug>"))))
		}
		if quote == "" {
			return &usageError{msg: "health review --spawn needs --quote: the human's words that ask for the review. Nothing was started.\n\nA human starts every agent. Without --spawn, health review prints the brief and starts nothing.\n\n" + healthReviewUsage}
		}
		if how == "" {
			how = "owner's shell"
			if me.Role == tracker.RoleProjectLead {
				how = "lead chat"
			}
		}
	}
	if *schema {
		fmt.Fprintln(ctx.Stdout, health.ReviewSchema)
		return nil
	}
	env, err := health.NewEnv(ctx.Project, projectRoot(ctx.Project), ctx.now())
	if err != nil {
		return err
	}
	snap, _, err := health.Latest(env.Root)
	if err != nil {
		return err
	}
	if snap == nil {
		return fmt.Errorf("%s", healthNoSnapshot)
	}
	rev, err := health.PrepareReview(health.ReviewInput{Env: env, Snap: snap, RetrosDir: retrosDir(ctx)})
	if err != nil {
		return err
	}
	if !*spawn {
		if ctx.JSONOutput {
			return writeJSON(ctx.Stdout, map[string]string{"brief": rev.Brief, "evidence": rev.Pack})
		}
		fmt.Fprintln(ctx.Stdout, rev.Brief)
		return nil
	}
	askedBy := fmt.Sprintf("Started on the human's words: %q (%s, %s).", quote, how, ctx.now().Format("2006-01-02"))
	resp := startAgent(ctx.Project, agentrun.Spec{
		Session:   reviewSession(ctx.Project.Slug),
		Prompt:    rev.Brief + "\n\n" + askedBy,
		AgentType: *agent,
		WorkDir:   env.Root,
		Workflow:  env.Workflow,
		Headless:  !*attach,
	})
	if resp.Status == "error" {
		printSteps(ctx, resp.Steps)
		return fmt.Errorf("spawning %s failed (see the steps above)", reviewSession(ctx.Project.Slug))
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, resp)
	}
	verb := "Started"
	if resp.Status == "reattached" {
		verb = "Already running:"
	}
	fmt.Fprintf(ctx.Stdout, "✓ %s review agent %s (evidence: %s)\n", verb, resp.Session, rev.Pack)
	fmt.Fprintf(ctx.Stdout, "  %s\n", askedBy)
	printSteps(ctx, resp.Steps)
	if resp.AttachCmd != "" {
		fmt.Fprintf(ctx.Stdout, "\nInside tmux:  %s\nFrom a shell: %s\n", resp.SwitchCmd, resp.AttachCmd)
	}
	fmt.Fprintf(ctx.Stdout, "It records its opinion with issue-cli health review record; the result shows on %s\n", healthURL(ctx.Project))
	return nil
}

func retrosDir(ctx *Context) string {
	return filepath.Join(projectRoot(ctx.Project), "retros")
}

func runReviewRecord(ctx *Context, args []string) error {
	fs := newFlagSet("health review record", ctx)
	reviewer := fs.String("reviewer", "", "who reviewed (default: $AGENT_NAME)")
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &usageError{msg: "health review record needs the review file\n\n" + healthReviewUsage}
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var o health.Opinion
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return fmt.Errorf("%s: %v (the shape: issue-cli health review --schema)", fs.Arg(0), err)
	}
	env, err := health.NewEnv(ctx.Project, projectRoot(ctx.Project), ctx.now())
	if err != nil {
		return err
	}
	snap, _, err := health.Latest(env.Root)
	if err != nil {
		return err
	}
	if snap == nil {
		return fmt.Errorf("%s", healthNoSnapshot)
	}
	if problems := o.Validate(snap, func(p string) bool { return health.PathAt(env.Root, env.Commit, p) }); len(problems) > 0 {
		return fmt.Errorf("%s: %d problem(s), nothing recorded:\n  - %s\n\nFix the file and run the same command again.", fs.Arg(0), len(problems), strings.Join(problems, "\n  - "))
	}
	who := *reviewer
	if who == "" {
		who = os.Getenv("AGENT_NAME")
	}
	health.NewOpinion(&o, snap, ctx.now(), who)
	path, err := health.WriteOpinion(env.Root, &o)
	if err != nil {
		return err
	}
	if _, _, err := health.Take(context.Background(), env, health.TriggerReview); err != nil {
		fmt.Fprintf(ctx.Stderr, "health: no snapshot after the review: %v\n", err)
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, map[string]any{"opinion": path, "score": o.Score, "recommendations": len(o.Recommendations), "health": healthURL(ctx.Project)})
	}
	fmt.Fprintf(ctx.Stdout, "✓ Recorded the review: score %d, %d recommendation(s) · %s\n", o.Score, len(o.Recommendations), path)
	fmt.Fprintf(ctx.Stdout, "Nothing becomes an issue by itself: the owner files or dismisses each one on %s\n", healthURL(ctx.Project))
	fmt.Fprintf(ctx.Stdout, "(or, with their words: issue-cli health review file R1 --quote \"...\")\n")
	return nil
}

func runReviewShow(ctx *Context, args []string) error {
	fs := newFlagSet("health review show", ctx)
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	root := projectRoot(ctx.Project)
	env, err := health.NewEnv(ctx.Project, root, ctx.now())
	if err == nil {
		root = env.Root
	}
	o, path := health.LatestOpinion(root, ctx.now())
	if o == nil {
		if ctx.JSONOutput {
			return writeJSON(ctx.Stdout, map[string]any{"opinion": nil, "hint": "no review yet: issue-cli health review"})
		}
		fmt.Fprintln(ctx.Stdout, "No health review yet: issue-cli health review (or --spawn to start a review agent)")
		return nil
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, o)
	}
	printOpinion(ctx, o, path)
	return nil
}

func printOpinion(ctx *Context, o *health.Opinion, path string) {
	w := ctx.Stdout
	fmt.Fprintf(w, "Health review %s · score %d · %.1f days old · by %s\nFile: %s\n\n%s\n\n", o.Date, o.Score, o.AgeDays(ctx.now()), valueOr(o.Reviewer, "an agent"), path, o.Assessment)
	for _, r := range o.Recommendations {
		state := valueOr(r.State, health.StateProposed)
		if r.Issue != "" {
			state += " → " + r.Issue
		}
		fmt.Fprintf(w, "%s [%s, effort %s, %s] %s\n", r.ID, r.Kind, r.Effort, state, r.Title)
		for _, e := range r.Evidence {
			ref := e.Metric
			if ref == "" {
				ref = e.Path
				if e.Lines != "" {
					ref += ":" + e.Lines
				}
			}
			fmt.Fprintf(w, "    %s: %s\n", ref, e.Shows)
		}
		fmt.Fprintf(w, "    expected: %s\n", r.ExpectedEffect)
	}
	if n := o.Proposed(); n > 0 {
		fmt.Fprintf(w, "\n%d proposed: the owner files or dismisses them on %s (or: issue-cli health review file <R#> --quote \"<their words>\")\n", n, healthURL(ctx.Project))
	}
}

func runReviewFile(ctx *Context, args []string) error {
	fs := newFlagSet("health review file", ctx)
	quote := fs.String("quote", "", "the owner's words approving it (required)")
	how := fs.String("how", "lead chat", "how the owner decided")
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &usageError{msg: "health review file needs a recommendation id (R1)\n\n" + healthReviewUsage}
	}
	env, err := health.NewEnv(ctx.Project, projectRoot(ctx.Project), ctx.now())
	if err != nil {
		return err
	}
	slug, err := health.FileRecommendation(ctx.Project, env.Workflow, env.Root, fs.Arg(0), health.Decision{How: *how, Quote: *quote}, ctx.now())
	if err != nil {
		return err
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, map[string]string{"slug": slug})
	}
	fmt.Fprintf(ctx.Stdout, "✓ Filed %s as %s (status idea, the owner's words recorded as a decision)\n", strings.ToUpper(fs.Arg(0)), slug)
	return nil
}

func runReviewDismiss(ctx *Context, args []string) error {
	fs := newFlagSet("health review dismiss", ctx)
	quote := fs.String("quote", "", "the owner's words (required)")
	reason := fs.String("reason", "", "why it is dismissed")
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &usageError{msg: "health review dismiss needs a recommendation id (R1)\n\n" + healthReviewUsage}
	}
	env, err := health.NewEnv(ctx.Project, projectRoot(ctx.Project), ctx.now())
	if err != nil {
		return err
	}
	r, err := health.DismissRecommendation(env.Root, fs.Arg(0), valueOr(*reason, *quote), health.Decision{Quote: *quote}, ctx.now())
	if err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stdout, "✓ Dismissed %s: %s\n", r.ID, r.Title)
	return nil
}
