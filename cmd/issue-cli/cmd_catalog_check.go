package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/workflowupgrade"
)

const catalogCheckUsage = `Usage:
  issue-cli workflow catalog-check [--base main]   # in the issue-viewer repo; make validate runs it

For issue-cli developers: when a work branch changes the code behind a
catalog feature (its sources: in cmd/issue-cli/templates/workflow-features.yaml)
or a bundled template, the entry's version: (or the template's stamp) must
be newer than the last release in cmd/issue-cli/CHANGELOG.md, so projects
that have the feature get an update proposal. When nothing a project adopts
changed (a refactor, a fix), mark it on a branch commit instead:
  Catalog: n/a <id>[,<id>...] <why>`

// runCatalogCheck compares the branch with its merge-base and fails when a
// touched catalog entry or template wasn't bumped (or marked n/a).
func runCatalogCheck(ctx *Context, args []string) error {
	fs := newFlagSet("workflow catalog-check", ctx)
	base := fs.String("base", "main", "branch the work branch merges into")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	root, err := catalogGit("", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("catalog-check runs inside a git checkout of issue-viewer: %w", err)
	}
	if _, err := os.Stat(filepath.Join(root, workflowupgrade.CatalogPath)); err != nil {
		return fmt.Errorf("%s not found: catalog-check runs in the issue-viewer repository\n\n%s", workflowupgrade.CatalogPath, catalogCheckUsage)
	}
	mergeBase, err := catalogGit(root, "merge-base", *base, "HEAD")
	if err != nil {
		fmt.Fprintf(ctx.Stdout, "catalog-check skipped: no merge-base with %s\n", *base)
		return nil
	}
	head, _ := catalogGit(root, "rev-parse", "HEAD")
	if head == mergeBase {
		fmt.Fprintf(ctx.Stdout, "catalog-check skipped: not a work branch (HEAD is %s)\n", *base)
		return nil
	}

	changed, err := catalogChangedFiles(root, mergeBase)
	if err != nil {
		return err
	}
	trailers, err := catalogGit(root, "log", "--no-merges", "--format=%(trailers:key=Catalog,valueonly,separator=%x0A)", mergeBase+"..HEAD")
	if err != nil {
		return err
	}
	changelog, err := os.ReadFile(filepath.Join(root, workflowupgrade.ChangelogPath))
	if err != nil {
		return err
	}
	last, ok := workflowupgrade.LastRelease(changelog)
	if !ok {
		return fmt.Errorf("no `## vX.Y.Z` release heading in %s", workflowupgrade.ChangelogPath)
	}
	rep, err := workflowupgrade.CheckBranch(workflowupgrade.Branch{
		Changed: changed,
		Base: func(p string) ([]byte, bool) {
			out, err := exec.Command("git", "-C", root, "show", mergeBase+":"+p).Output()
			return out, err == nil
		},
		Head: func(p string) ([]byte, bool) {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
			return data, err == nil
		},
		Trailers: strings.Split(trailers, "\n"),
	}, last)
	if err != nil {
		return err
	}
	if ctx.JSONOutput {
		if err := writeJSON(ctx.Stdout, rep); err != nil {
			return err
		}
	} else {
		printCatalogCheck(ctx.Stdout, rep)
	}
	if !rep.OK() {
		return fmt.Errorf("catalog-check: bump the entries above, or mark them n/a")
	}
	return nil
}

func printCatalogCheck(w io.Writer, rep *workflowupgrade.CheckReport) {
	if len(rep.Entries) == 0 && len(rep.Templates) == 0 {
		fmt.Fprintln(w, "catalog-check ok: this branch changes no catalog feature's sources and no bundled template")
		return
	}
	stale := 0
	for _, e := range rep.Entries {
		switch e.State {
		case "bumped":
			fmt.Fprintf(w, "✓ catalog: %s is %s (newer than %s)\n", e.ID, e.Version, rep.LastRelease)
		case "n/a":
			fmt.Fprintf(w, "✓ catalog: %s marked n/a: %s\n", e.ID, e.Why)
		default:
			stale++
			fmt.Fprintf(w, "✗ catalog: this branch changes %s\n", strings.Join(e.Files, ", "))
			fmt.Fprintf(w, "    entry %s is %s; the next release is %s (last: %s in %s)\n", e.ID, e.Version, rep.NextRelease, rep.LastRelease, workflowupgrade.ChangelogPath)
			fmt.Fprintf(w, "    → if what projects adopt changed: set version: %s in %s and update its what/snippet\n", rep.NextRelease, workflowupgrade.CatalogPath)
			fmt.Fprintf(w, "    → if not (refactor, fix): add a commit trailer  Catalog: n/a %s <why>\n", e.ID)
		}
	}
	for _, t := range rep.Templates {
		if t.State == "bumped" {
			fmt.Fprintf(w, "✓ template: %s is stamped %s\n", t.Name, t.Stamp)
			continue
		}
		stale++
		fmt.Fprintf(w, "✗ template: this branch changes %s/%s.yaml (stamp %s)\n", workflowupgrade.TemplatesPath, t.Name, t.Stamp)
		fmt.Fprintf(w, "    → bump it to template: %s@%s, then run issue-cli workflow scan on it so its features: record matches\n", t.Name, rep.NextRelease)
	}
	if stale == 0 {
		fmt.Fprintf(w, "catalog-check ok: %d entries and %d templates touched, all bumped or marked n/a\n", len(rep.Entries), len(rep.Templates))
	}
}

// catalogChangedFiles lists the files changed since mergeBase, in commits and
// in the working tree, untracked files included.
func catalogChangedFiles(root, mergeBase string) ([]string, error) {
	diff, err := catalogGit(root, "diff", "--name-only", mergeBase)
	if err != nil {
		return nil, err
	}
	untracked, err := catalogGit(root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(diff+"\n"+untracked, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

func catalogGit(dir string, args ...string) (string, error) {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	var stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
