package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/ci"
	"github.com/michal-franc/issue-viewer/internal/milestone"
	"github.com/michal-franc/issue-viewer/internal/privacy"
)

var releaseCommand = &Command{
	Name:      "release",
	ShortHelp: "Cut, soak and publish a release on its branch (sub: setup|cut|pick|publish)",
	LongHelp: `A release is cut as a branch, soaks there as release candidates, and is
published from it (docs/roadmap.md, Cutting a release). With privacy.code:
snapshots the development history stays in a private dev repo (origin) and the
public repo (remote public) gets ONE commit per release.

Subcommands:
  setup --dev <url>
      Once, for code: snapshots. origin becomes the remote public (its tags never
      fetched), <url> (the private dev repo the owner created) becomes origin and
      gets every branch and every non-candidate tag. Installs the pre-push guard.
      Nothing is pushed to public; cleaning its old history is a separate step.
  cut <version>
      When Waiting for release says the milestone is ready (or a pick needs the
      next candidate): creates release/vX.Y from main with its worktree
      .worktrees/release-vX.Y, and tags the next <version>-rc.N on it (never
      pushed). With code: snapshots the branch goes to origin. Main moves on.
  pick [--release <version>] <slug|sha>...
      Cherry-picks (with -x) an issue's commits (work/<slug>) or a commit onto the
      release branch, in its worktree. Fixes land on main first; nothing is fixed
      on the branch itself. A pick after the newest candidate needs the next one.
  publish <version> [--first]
      Only on the owner's ok on a soaked candidate, from the release worktree.
      Names the changelog heading on the branch, pushes the branch tip's tree as
      ONE commit "<version>" onto public's branch (the public repo's release
      Action tags and builds it), tags <version> on the dev branch, pushes both to
      origin, and moves the released lines of main's ## Unreleased under the
      version. --first accepts a public tip that is the old history.

cut and publish read the viewer's Waiting for release card and refuse unless it
says the same (ready to cut; soaking).`,
	Run:         runRelease,
	Subcommands: []string{"setup", "cut", "pick", "publish"},
}

func init() {
	registerCommand(releaseCommand)
}

func runRelease(ctx *Context, args []string) error {
	if len(args) == 0 {
		return &usageError{msg: "release requires a subcommand\n\nUsage:\n  issue-cli release setup --dev <url>\n  issue-cli release cut <version>\n  issue-cli release pick [--release <version>] <slug|sha>...\n  issue-cli release publish <version> [--first]"}
	}
	if ctx.Project == nil {
		return fmt.Errorf("release needs a project — pass --project <slug> or run from a project root")
	}
	switch args[0] {
	case "setup":
		return runReleaseSetup(ctx, args[1:])
	case "cut":
		return runReleaseCut(ctx, args[1:])
	case "pick":
		return runReleasePick(ctx, args[1:])
	case "publish":
		return runReleasePublish(ctx, args[1:])
	}
	return &usageError{msg: fmt.Sprintf("unknown release subcommand %q (setup, cut, pick, publish)", args[0])}
}

const snapshotsNotChosen = `privacy.code is not snapshots in workflow.yaml: the owner has not chosen private development.
Ask them, then record it:
  privacy:
    code: snapshots
    decided: "<date>, owner: \"<their words>\""
(issue-cli workflow upgrade shows the privacy entry and its steps)`

func runReleaseSetup(ctx *Context, args []string) error {
	fs := newFlagSet("release setup", ctx)
	dev := fs.String("dev", "", "URL of the private dev repo (the owner creates it)")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	cfg := ctx.Project.LoadWorkflow().Privacy
	if !cfg.Snapshots() {
		return fmt.Errorf("%s", snapshotsNotChosen)
	}
	if strings.TrimSpace(*dev) == "" {
		return &usageError{msg: "release setup needs --dev <url>: the private dev repo the owner created (agents never create repos)"}
	}
	root := privacy.MainCheckout(ctx.Project.TelemetryRoot())
	res, err := privacy.Setup(root, strings.TrimSpace(*dev), cfg)
	if err != nil {
		return err
	}
	out := ctx.Stdout
	if res.RenamedOrigin {
		fmt.Fprintf(out, "✓ The old origin is now the remote %s (tags never fetched from it)\n", cfg.Remote())
	}
	fmt.Fprintf(out, "✓ origin is the dev repo %s and has every %s\n", *dev, strings.Join(res.Pushed, " and "))
	if res.SkippedRC > 0 {
		fmt.Fprintf(out, "✓ %d release candidate tags stay local\n", res.SkippedRC)
	}
	if res.Base != "" {
		fmt.Fprintf(out, "✓ %s/%s is at %s: the first snapshot goes on top of it\n", cfg.Remote(), cfg.Branch(), shortSha(res.Base))
	}
	if err := installPrivacyHook(ctx, root); err != nil {
		return err
	}
	fmt.Fprintf(out, `Next, with the owner:
  - The public repo still holds the old dev history. Clean it once from a mirror,
    never in this checkout: git clone --mirror <public url>, git filter-repo
    --path issues --path sessions --invert-paths (and the other work paths), git
    push --force --mirror. Then: git fetch %s && issue-cli release publish <version> --first.
  - The release workflow must skip the private dev repo, or it builds releases
    there too: if: ${{ !github.event.repository.private }} on its job.
  - Other machines and cloud sessions clone the dev repo, never the public one.
`, cfg.Remote())
	return nil
}

// releaseCard is the viewer's Waiting for release milestone (release.json).
type releaseCard struct {
	Version   string   `json:"version"`
	Cut       string   `json:"cut"`
	RC        bool     `json:"rc"`
	Ready     bool     `json:"ready"`
	CutNext   string   `json:"cut_next"`
	Soaking   bool     `json:"soaking"`
	Candidate string   `json:"candidate"`
	Branch    string   `json:"branch"`
	Unpicked  []string `json:"unpicked"`
	Checks    []struct {
		Key    string `json:"key"`
		Text   string `json:"text"`
		Detail string `json:"detail"`
		OK     bool   `json:"ok"`
	} `json:"checks"`
}

// fetchReleaseCard reads the card from the running viewer.
func fetchReleaseCard(ctx *Context) (*releaseCard, error) {
	url := fmt.Sprintf("%s/p/%s/release.json", viewerBaseURL(ctx.Project), ctx.Project.Slug)
	body, err := viewerGet(url)
	if err != nil {
		return nil, fmt.Errorf("can't read Waiting for release from the viewer at %s: %v\n\nStart it: issue-viewer --config %s\nRunning elsewhere? Set viewer_url in projects.yaml.", url, err, valueOr(ctx.ConfigPath, "<projects.yaml>"))
	}
	var doc struct {
		Milestone *releaseCard `json:"milestone"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	if doc.Milestone == nil {
		return nil, fmt.Errorf("Waiting for release has no milestone to cut (every version is released, or no issue has a version:)")
	}
	return doc.Milestone, nil
}

// unmet lists the card's failing checks, one per line.
func (c *releaseCard) unmet() string {
	var b strings.Builder
	for _, ch := range c.Checks {
		if ch.OK {
			continue
		}
		fmt.Fprintf(&b, "  ✗ %s", ch.Text)
		if ch.Detail != "" {
			fmt.Fprintf(&b, ": %s", ch.Detail)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func runReleaseCut(ctx *Context, args []string) error {
	fs := newFlagSet("release cut", ctx)
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &usageError{msg: "release cut takes the version: issue-cli release cut v0.54.0"}
	}
	cut, err := privacy.ParseVersion(fs.Arg(0))
	if err != nil {
		return &usageError{msg: err.Error()}
	}
	card, err := fetchReleaseCard(ctx)
	if err != nil {
		return err
	}
	if milestone.Norm(card.Cut) != milestone.Norm(cut) {
		return fmt.Errorf("the milestone to cut is %s (version %s), not %s", card.Cut, card.Version, cut)
	}
	if card.CutNext == "" {
		why := card.unmet()
		if card.Soaking {
			why = "  " + card.Candidate + " is soaking and nothing was picked after it: publish it on the owner's ok, or pick a fix first\n"
		}
		return fmt.Errorf("Waiting for release does not say ready to cut %s:\n%s", cut, why)
	}
	wf := ctx.Project.LoadWorkflow()
	root := privacy.MainCheckout(ctx.Project.TelemetryRoot())
	from, err := ci.ResolveBranch(root, wf.CI)
	if err != nil || from == "" {
		from = "main"
	}
	res, err := privacy.Cut(root, cut, from, wf.Privacy.Snapshots())
	if err != nil {
		return err
	}
	out := ctx.Stdout
	if res.Created {
		fmt.Fprintf(out, "✓ Created %s from %s, worktree %s\n", res.Branch, from, res.Worktree)
	}
	fmt.Fprintf(out, "✓ Tagged %s on %s (local; never push a candidate tag)\n", res.Tag, res.Branch)
	if res.Pushed {
		fmt.Fprintf(out, "✓ Pushed %s to origin (the dev repo)\n", res.Branch)
	}
	fmt.Fprintf(out, `Next:
  - Install the candidate from the release worktree: cd %s && make install, then restart what runs it.
  - Tell the leads of the projects that run it; their issue-cli report-bug files join %s.
  - Fixes land on main as usual; pick them onto the branch: issue-cli release pick <slug>. Each pick needs the next candidate (issue-cli release cut %s).
`, res.Worktree, card.Version, cut)
	return nil
}

// releaseWorktrees are the release branches' worktrees under .worktrees/.
func releaseWorktrees(root string) []string {
	matches, _ := filepath.Glob(filepath.Join(root, ".worktrees", "release-v*"))
	sort.Strings(matches)
	return matches
}

func runReleasePick(ctx *Context, args []string) error {
	fs := newFlagSet("release pick", ctx)
	rel := fs.String("release", "", "the release to pick onto (v0.54.0); needed only with more than one release worktree")
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return &usageError{msg: "release pick takes issue slugs or commits: issue-cli release pick <slug|sha>..."}
	}
	root := privacy.MainCheckout(ctx.Project.TelemetryRoot())
	cut := ""
	if *rel != "" {
		v, err := privacy.ParseVersion(*rel)
		if err != nil {
			return &usageError{msg: err.Error()}
		}
		cut = v
	} else {
		wts := releaseWorktrees(root)
		switch len(wts) {
		case 0:
			return fmt.Errorf("no release worktree under %s: cut the release first (issue-cli release cut <version>)", filepath.Join(root, ".worktrees"))
		case 1:
			cut = strings.TrimPrefix(filepath.Base(wts[0]), "release-") + ".0"
		default:
			var names []string
			for _, w := range wts {
				names = append(names, filepath.Base(w))
			}
			return &usageError{msg: fmt.Sprintf("more than one release worktree (%s): pass --release <version>", strings.Join(names, ", "))}
		}
	}
	res, err := privacy.Pick(root, cut, fs.Args())
	if err != nil {
		return err
	}
	branch := milestone.ReleaseBranch(cut)
	for _, a := range res.Already {
		fmt.Fprintf(ctx.Stdout, "✓ %s is already on %s\n", a, branch)
	}
	if len(res.Picked) == 0 {
		fmt.Fprintln(ctx.Stdout, "Nothing to pick.")
		return nil
	}
	fmt.Fprintf(ctx.Stdout, "✓ Picked %d %s onto %s: %s\n", len(res.Picked), pluralWord(len(res.Picked), "commit", "commits"), branch, strings.Join(res.Picked, " "))
	fmt.Fprintf(ctx.Stdout, "Next: cut the next candidate so the fix soaks: issue-cli release cut <version> (Waiting for release names it).\n")
	return nil
}

func runReleasePublish(ctx *Context, args []string) error {
	fs := newFlagSet("release publish", ctx)
	first := fs.Bool("first", false, "the public tip is the old history (cleaned once), not a snapshot: put the first snapshot on it")
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &usageError{msg: "release publish takes the version: issue-cli release publish v0.54.0"}
	}
	cut, err := privacy.ParseVersion(fs.Arg(0))
	if err != nil {
		return &usageError{msg: err.Error()}
	}
	wf := ctx.Project.LoadWorkflow()
	if !wf.Privacy.Snapshots() {
		return fmt.Errorf("privacy.code is not snapshots: this project publishes by pushing main (docs/roadmap.md, Cutting a release)")
	}
	card, err := fetchReleaseCard(ctx)
	if err != nil {
		return err
	}
	if milestone.Norm(card.Cut) != milestone.Norm(cut) || !card.Soaking {
		return fmt.Errorf("Waiting for release does not say a %s candidate is soaking with every check met:\n%sPublish only a soaked candidate, on the owner's ok.", cut, card.unmet())
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root := privacy.MainCheckout(ctx.Project.TelemetryRoot())
	res, err := privacy.Publish(privacy.PublishInput{
		Root: root, Worktree: cwd, Cut: cut, Cfg: wf.Privacy, Changelog: wf.Workbench.Changelog, First: *first, Now: ctx.Now(),
	})
	if err != nil {
		return err
	}
	out := ctx.Stdout
	fmt.Fprintf(out, "✓ Published %s: one commit %s on %s/%s", cut, shortSha(res.Snapshot), wf.Privacy.Remote(), wf.Privacy.Branch())
	if res.Parent != "" {
		fmt.Fprintf(out, " (on top of %s)", shortSha(res.Parent))
	}
	if res.Signed {
		fmt.Fprint(out, ", signed with user.signingkey")
	} else {
		fmt.Fprint(out, ", unsigned (no user.signingkey: GitHub shows it Unverified)")
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "✓ Tagged %s on %s; pushed both to origin\n", cut, milestone.ReleaseBranch(cut))
	if res.MainMoved {
		fmt.Fprintf(out, "✓ Moved the released lines of ## Unreleased on %s under ## %s (committed; push main when you push)\n", res.MainBranch, cut)
	}
	fmt.Fprintf(out, "Next: the public repo's release Action tags %s and builds it. Then make install from main.\n", cut)
	return nil
}

func shortSha(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
