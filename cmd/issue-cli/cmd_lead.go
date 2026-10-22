package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/agentrun"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var leadCommand = &Command{
	Name:      "lead",
	ShortHelp: "The project lead: spawn it, see who leads whom (sub: spawn|whoami|list|message)",
	LongHelp: `The project lead supervises workers. There is one lead per project
(tmux lead-<project>, in the project checkout); every worker reports to it,
epic children included. Only the human decides.

Subcommands:
  spawn [--attach] [--agent claude|codex]
      Start the project lead. A live lead is re-attached, never started
      twice. Runs headless unless --attach.
  whoami
      Your role, lead, and what you may tick.
  list
      The project lead (live or not) and the open epics, with how many of
      their children are running.
  message <target> "text"
      Paste a message into the lead's or a worker's session, signed
      [from <you>]. target: project, a lead-/agent- session name, or an
      issue slug (its worker). Workers can't message; they post question:
      comments instead.

Examples:
  issue-cli lead spawn
  issue-cli lead whoami
  issue-cli lead message ui/workbench-redesign "please rebase on main"`,
	Run:         runLead,
	Subcommands: []string{"spawn", "whoami", "list", "message"},
}

func init() {
	registerCommand(leadCommand)
}

func runLead(ctx *Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("lead requires a subcommand\n\nUsage:\n  issue-cli lead spawn [--attach]\n  issue-cli lead whoami\n  issue-cli lead list\n  issue-cli lead message <target> \"text\"")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "spawn":
		return runLeadSpawn(ctx, rest)
	case "whoami":
		return runLeadWhoami(ctx, rest)
	case "list":
		return runLeadList(ctx, rest)
	case "message":
		return runLeadMessage(ctx, rest)
	default:
		return fmt.Errorf("unknown lead subcommand: %s\n\nValid: spawn, whoami, list, message", sub)
	}
}

// errSubLeadsRemoved answers `lead spawn --epic`, kept so old habits get a
// clear answer instead of an unknown-flag error.
const errSubLeadsRemoved = "sub-leads were removed (one lead per project); issue-cli lead spawn starts the project lead"

func runLeadSpawn(ctx *Context, args []string) error {
	for _, a := range args {
		if name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "="); strings.HasPrefix(a, "-") && name == "epic" {
			return fmt.Errorf("%s", tracker.InjectProject(ctx.Project, errSubLeadsRemoved))
		}
	}
	fs := newFlagSet("lead spawn", ctx)
	attachFlag := fs.Bool("attach", false, "open a terminal on the lead (default: headless)")
	agentFlag := fs.String("agent", "claude", "agent to run: claude or codex")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	proj := ctx.Project
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return fmt.Errorf("cannot load issues: %w", err)
	}
	if me := whoAmI(proj, issues); me.Role == tracker.RoleWorker {
		return fmt.Errorf("workers can't spawn leads (you are %s, detected from %s).\n\nPost a question: comment on your issue instead; your lead relays it:\n  %s", valueOr(me.Name, "a worker"), me.Signal,
			tracker.InjectProject(proj, fmt.Sprintf("issue-cli comment %s \"question: ...\"", valueOr(me.Slug, "<slug>"))))
	}

	spec := agentrun.LeadSpec(proj, proj.LoadWorkflow(), projectRoot(proj), ctx.now())
	spec.AgentType = *agentFlag
	spec.ViewerURL = strings.TrimSpace(os.Getenv("ISSUE_VIEWER_URL"))
	spec.Headless = !*attachFlag

	resp := startAgent(proj, spec)
	if resp.Status == "error" {
		printSteps(ctx, resp.Steps)
		return fmt.Errorf("spawning %s failed (see the steps above)", spec.Session)
	}

	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, resp)
	}
	verb := "Spawned"
	if resp.Status == "reattached" {
		verb = "Already live:"
	}
	fmt.Fprintf(ctx.Stdout, "✓ %s project lead %s\n", verb, resp.Session)
	printSteps(ctx, resp.Steps)
	if resp.AttachCmd != "" {
		fmt.Fprintf(ctx.Stdout, "\nInside tmux:  %s\nFrom a shell: %s\n", resp.SwitchCmd, resp.AttachCmd)
	}
	return nil
}

func printSteps(ctx *Context, steps []agentrun.Step) {
	for _, s := range steps {
		mark := "✓"
		if s.Status == "error" {
			mark = "✗"
		}
		line := fmt.Sprintf("  %s %s", mark, s.Name)
		if s.Detail != "" {
			line += " — " + s.Detail
		}
		fmt.Fprintln(ctx.Stdout, line)
	}
}

type whoamiJSON struct {
	Role      string `json:"role"`
	Session   string `json:"session,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Issue     string `json:"issue,omitempty"`
	Guards    string `json:"guards,omitempty"`
	Lead      string `json:"lead,omitempty"`
	LeadLive  bool   `json:"lead_live"`
	MayTick   string `json:"may_tick"`
	Reach     string `json:"reach,omitempty"`
	Signal    string `json:"signal"`
	LegacyKey bool   `json:"legacy_umbrella,omitempty"`
}

func runLeadWhoami(ctx *Context, args []string) error {
	fs := newFlagSet("lead whoami", ctx)
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	proj := ctx.Project
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return fmt.Errorf("cannot load issues: %w", err)
	}
	live := sessionLive(proj)
	me := whoAmI(proj, issues)
	projectLead := tracker.LeadSessionName(proj)
	out := whoamiJSON{Role: string(me.Role), Session: me.Name, Scope: me.Scope, Issue: me.Slug, Signal: me.Signal}

	switch me.Role {
	case tracker.RoleProjectLead:
		out.Guards = "main (the project checkout)"
		out.MayTick = "gates the human approved in your chat, with issue-cli approve --quote; never Human Testing"
	case tracker.RoleWorker:
		issue := tracker.FindIssue(issues, me.Slug)
		route := tracker.LeadFor(proj, issue, issues, live)
		out.Lead, out.LeadLive = route.Lead, route.Live
		if issue != nil {
			out.Guards = tracker.AgentSessionName(issue.Slug) + "'s branch only"
			out.Reach = fmt.Sprintf("issue-cli comment %s \"question: ...\" — your lead follows your issue trail", issue.Slug)
			out.LegacyKey = tracker.UsesLegacyUmbrella(issue)
		} else {
			out.Reach = "post question: comments on your issue — your lead follows your issue trail"
		}
		out.MayTick = "nothing: workers never tick gates"
	default:
		out.Lead, out.LeadLive = projectLead, live(projectLead)
		out.MayTick = "as the human, anything; an agent started by hand should be spawned instead"
	}

	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, out)
	}
	w := ctx.Stdout
	fmt.Fprintf(w, "Role: %s", out.Role)
	if out.Session != "" {
		fmt.Fprintf(w, " (%s)", out.Session)
	}
	fmt.Fprintf(w, "  [detected from %s]\n", out.Signal)
	if out.Scope != "" {
		fmt.Fprintf(w, "Scope: %s\n", out.Scope)
	}
	if out.Issue != "" {
		fmt.Fprintf(w, "Issue: %s\n", out.Issue)
	}
	if out.Guards != "" {
		fmt.Fprintf(w, "Guards: %s\n", out.Guards)
	}
	if out.Lead != "" {
		fmt.Fprintf(w, "Reports to: %s (%s)\n", out.Lead, liveWord(out.LeadLive))
	} else {
		fmt.Fprintln(w, "Reports to: the human")
	}
	if out.Reach != "" {
		fmt.Fprintf(w, "Reach your lead: %s\n", tracker.InjectProject(proj, out.Reach))
	}
	fmt.Fprintf(w, "May tick: %s\n", out.MayTick)
	if out.LegacyKey {
		fmt.Fprintf(w, "Hint: your issue links its epic with umbrella: (legacy alias). Rename it: issue-cli set-meta %s --key parent --value %s\n", out.Issue, tracker.ParentSlug(tracker.FindIssue(issues, out.Issue)))
	}
	if me.Role == tracker.RoleUnmanaged {
		fmt.Fprintf(w, "\nNot a spawned lead or worker. If you are acting as the project lead, run: %s\n", tracker.InjectProject(proj, "issue-cli lead spawn"))
	}
	return nil
}

func liveWord(live bool) string {
	if live {
		return "live"
	}
	return "not running"
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

type leadRow struct {
	Session string `json:"session"`
	Live    bool   `json:"live"`
}

type epicRow struct {
	Slug            string `json:"slug"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	ChildrenRunning int    `json:"children_running"`
	ChildrenTotal   int    `json:"children_total"`
}

type leadListJSON struct {
	Lead  leadRow   `json:"lead"`
	Epics []epicRow `json:"epics"`
}

// leadList builds the project lead's row and one row per open epic.
func leadList(proj *tracker.Project, issues []*tracker.Issue, live map[string]bool) leadListJSON {
	projectLead := tracker.LeadSessionName(proj)
	out := leadListJSON{Lead: leadRow{Session: projectLead, Live: live[projectLead]}, Epics: []epicRow{}}
	for _, issue := range issues {
		if issue.Status == "done" || !tracker.IsEpic(issue, issues) {
			continue
		}
		row := epicRow{Slug: issue.Slug, Title: issue.Title, Status: issue.Status}
		for _, child := range tracker.EpicChildren(issues, issue.Slug) {
			row.ChildrenTotal++
			if live[tracker.AgentSessionName(child.Slug)] {
				row.ChildrenRunning++
			}
		}
		out.Epics = append(out.Epics, row)
	}
	return out
}

func runLeadList(ctx *Context, args []string) error {
	fs := newFlagSet("lead list", ctx)
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	proj := ctx.Project
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return fmt.Errorf("cannot load issues: %w", err)
	}
	list := leadList(proj, issues, liveSessionNames(proj))
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, list)
	}
	w := ctx.Stdout
	fmt.Fprintln(w, "== Lead ==")
	fmt.Fprintf(w, "  %-40s project lead  %s\n", list.Lead.Session, liveWord(list.Lead.Live))
	if len(list.Epics) > 0 {
		fmt.Fprintln(w, "\n== Epics ==")
		for _, r := range list.Epics {
			fmt.Fprintf(w, "  %s (%s) · children running %d/%d\n", r.Slug, r.Status, r.ChildrenRunning, r.ChildrenTotal)
		}
	}
	if !list.Lead.Live {
		fmt.Fprintf(w, "\nThe project lead is not running. Start it: %s\n", tracker.InjectProject(proj, "issue-cli lead spawn"))
	}
	return nil
}

func runLeadMessage(ctx *Context, args []string) error {
	fs := newFlagSet("lead message", ctx)
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	args = fs.Args()
	if len(args) < 2 {
		return fmt.Errorf("lead message requires <target> and \"text\"\n\nExample:\n  issue-cli lead message <slug> \"please rebase on main\"")
	}
	target := args[0]
	text := normalizeEscapedText(strings.Join(args[1:], " "))
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("lead message: text is empty")
	}
	proj := ctx.Project
	issues, err := tracker.LoadIssues(proj.IssueDir)
	if err != nil {
		return fmt.Errorf("cannot load issues: %w", err)
	}
	me := whoAmI(proj, issues)
	if me.Role == tracker.RoleWorker {
		slug := valueOr(me.Slug, "<slug>")
		return fmt.Errorf("workers can't message sessions. Ask your lead on your issue trail instead:\n  %s", tracker.InjectProject(proj, fmt.Sprintf("issue-cli comment %s \"question: ...\"", slug)))
	}
	session := resolveMessageTarget(proj, issues, target)
	textFile := filepath.Join(projectRoot(proj), ".agent-logs", session, "messages", ctx.now().Format("20060102-150405")+".txt")
	resp := pasteIntoSession(proj, session, fmt.Sprintf("[from %s] %s", me.sender(), text), textFile, "")
	switch resp.Status {
	case "no-session":
		hint := ""
		if session == tracker.LeadSessionName(proj) {
			hint = "\n\nStart it: " + tracker.InjectProject(proj, "issue-cli lead spawn")
		}
		return fmt.Errorf("%s is not running; nothing was sent.%s", resp.Session, hint)
	case "error":
		printSteps(ctx, resp.Steps)
		return fmt.Errorf("sending to %s failed (see the steps above)", resp.Session)
	}
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, resp)
	}
	fmt.Fprintf(ctx.Stdout, "✓ Sent to %s\n", resp.Session)
	printSteps(ctx, resp.Steps)
	return nil
}

// resolveMessageTarget turns a message target into a session name: "project"
// is the project lead; lead-/agent- names are taken as is; an issue slug is
// its worker.
func resolveMessageTarget(proj *tracker.Project, issues []*tracker.Issue, target string) string {
	switch {
	case target == tracker.LeadScopeProject:
		return tracker.LeadSessionName(proj)
	case strings.HasPrefix(target, "lead-"), strings.HasPrefix(target, "agent-"):
		return target
	}
	slug := target
	if issue := tracker.FindIssue(issues, target); issue != nil {
		slug = issue.Slug
	}
	return tracker.AgentSessionName(slug)
}

// leadHeaderLine is the "Lead:" line show and start print for an issue.
func leadHeaderLine(proj *tracker.Project, issue *tracker.Issue, issues []*tracker.Issue) string {
	route := tracker.LeadFor(proj, issue, issues, sessionLive(proj))
	return fmt.Sprintf("Lead: %s (%s)", route.Lead, liveWord(route.Live))
}

// umbrellaHint nudges issues still on the legacy umbrella: key to parent:.
func umbrellaHint(proj *tracker.Project, issue *tracker.Issue) string {
	if !tracker.UsesLegacyUmbrella(issue) {
		return ""
	}
	return tracker.InjectProject(proj, fmt.Sprintf("Hint: umbrella: is a legacy alias for parent:. Rename it: issue-cli set-meta %s --key parent --value %s && issue-cli set-meta %s --key umbrella --clear",
		issue.Slug, tracker.ParentSlug(issue), issue.Slug))
}

// printLeadLines prints who the issue reports to, plus the umbrella: rename
// hint when it applies. show and start print it under the status line.
func printLeadLines(ctx *Context, issue *tracker.Issue) {
	issues, err := tracker.LoadIssues(ctx.Project.IssueDir)
	if err != nil {
		return
	}
	fmt.Fprintln(ctx.Stdout, leadHeaderLine(ctx.Project, issue, issues))
	if hint := umbrellaHint(ctx.Project, issue); hint != "" {
		fmt.Fprintln(ctx.Stdout, hint)
	}
}
