package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
	"github.com/michal-franc/issue-viewer/internal/workflowupgrade"
)

// stdinIsTTY reports whether the supplied reader is a terminal. Used by the
// interactive workflow-init prompt to decide whether to read a selection.
func stdinIsTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// doWorkflowInit is the testable core. It takes explicit IO and a TTY hint so
// tests can drive interactive selection without a real terminal, and returns
// errors instead of calling os.Exit.
func doWorkflowInit(template string, force bool, in io.Reader, out io.Writer, isTTY bool) error {
	// One buffered reader for every prompt, so a later prompt doesn't lose
	// what the first one buffered.
	br := bufio.NewReader(in)
	templates := workflowupgrade.Templates()
	if len(templates) == 0 {
		return fmt.Errorf("no workflow templates are bundled in this binary — this is a build issue, not a usage issue")
	}

	chosen, err := resolveTemplate(template, templates, br, out, isTTY)
	if err != nil {
		return err
	}

	target := "workflow.yaml"
	if _, statErr := os.Stat(target); statErr == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", target)
	}

	data, err := workflowupgrade.ReadTemplate(chosen)
	if err != nil {
		return fmt.Errorf("reading bundled template %q: %w", chosen, err)
	}

	if err := os.WriteFile(target, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}

	scaffolded, err := scaffoldProjectDirs()
	if err != nil {
		return err
	}

	if len(scaffolded) > 0 {
		fmt.Fprintf(out, "✓ Wrote %s (template: %s) and scaffolded %s\n", target, chosen, strings.Join(scaffolded, ", "))
	} else {
		fmt.Fprintf(out, "✓ Wrote %s (template: %s)\n", target, chosen)
	}
	return askPrivacy(target, br, out, isTTY, time.Now())
}

// privacyUndecided is printed when init could not ask the owner.
const privacyUndecided = "privacy not decided: ask the owner whether the work files are private (issue-cli workflow upgrade, entry privacy)"

// pickOne reads a line and maps it through choices; "" for anything else.
func pickOne(in *bufio.Reader, choices map[string]string) string {
	line, _ := in.ReadString('\n')
	return choices[strings.ToLower(strings.TrimSpace(line))]
}

// askPrivacy asks the owner, on a terminal, whether the work files are
// private and whether development is, and appends their answers and words
// to workflow.yaml's privacy: block. Without a terminal, or with no answer, nothing is decided and the
// hint says how to decide later.
func askPrivacy(target string, in *bufio.Reader, out io.Writer, isTTY bool, now time.Time) error {
	if !isTTY {
		fmt.Fprintln(out, privacyUndecided)
		return nil
	}
	fmt.Fprintln(out, "Work files (issues, sessions, retros, attachments): private or public?")
	fmt.Fprintln(out, "  1) private: a private tracker repo, outside the code repo (issue-cli tracker init sets it up)")
	fmt.Fprintln(out, "  2) public: keep them in the code repo")
	fmt.Fprint(out, "Choice (1, 2, or Enter to decide later): ")
	work := pickOne(in, map[string]string{"1": tracker.WorkFilesTracker, "private": tracker.WorkFilesTracker, "2": tracker.WorkFilesInRepo, "public": tracker.WorkFilesInRepo})
	fmt.Fprintln(out, "Development: private, with only releases published, or in the public repo?")
	fmt.Fprintln(out, "  1) private: a private dev repo; the public repo gets one commit per release (issue-cli release setup)")
	fmt.Fprintln(out, "  2) public: develop in the public repo")
	fmt.Fprint(out, "Choice (1, 2, or Enter to decide later): ")
	code := pickOne(in, map[string]string{"1": tracker.CodeSnapshots, "private": tracker.CodeSnapshots, "2": tracker.CodePublic, "public": tracker.CodePublic})
	if work == "" && code == "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, privacyUndecided)
		return nil
	}
	fmt.Fprint(out, "In your words, why (recorded in privacy.decided; Enter to skip): ")
	words, _ := in.ReadString('\n')
	decided := now.Format("2006-01-02") + ", owner at issue-cli init"
	if w := strings.TrimSpace(words); w != "" {
		decided = now.Format("2006-01-02") + ", owner: " + strconv.Quote(w)
	}
	block := "\nprivacy:\n"
	if work != "" {
		block += "  work_files: " + work + "\n"
	}
	if code != "" {
		block += "  code: " + code + "\n"
	}
	block += "  decided: " + strconv.Quote(decided) + "\n"
	f, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(block); err != nil {
		return err
	}
	fmt.Fprintln(out)
	if work != "" {
		fmt.Fprintf(out, "✓ Recorded privacy.work_files: %s in %s\n", work, target)
	}
	if code != "" {
		fmt.Fprintf(out, "✓ Recorded privacy.code: %s in %s\n", code, target)
	}
	if work == tracker.WorkFilesTracker {
		fmt.Fprintln(out, "Next: create a private repo for the tracker (e.g. <name>-tracker), then: issue-cli tracker init --remote <its url>")
	}
	if code == tracker.CodeSnapshots {
		fmt.Fprintln(out, "Next: create a private dev repo (e.g. <name>-dev), then: issue-cli release setup --dev <its url>")
	}
	if work == "" || code == "" {
		fmt.Fprintln(out, privacyUndecided)
	}
	return nil
}

func resolveTemplate(flagValue string, templates []string, in io.Reader, out io.Writer, isTTY bool) (string, error) {
	if flagValue != "" {
		for _, t := range templates {
			if t == flagValue {
				return t, nil
			}
		}
		return "", fmt.Errorf("unknown template %q\n\nAvailable templates: %s", flagValue, strings.Join(templates, ", "))
	}

	if !isTTY {
		return "", fmt.Errorf("--template is required when stdin is not a terminal\n\nAvailable templates: %s\n\nExample:\n  issue-cli workflow init --template %s", strings.Join(templates, ", "), templates[0])
	}

	fmt.Fprintln(out, "Pick a workflow template:")
	for i, t := range templates {
		fmt.Fprintf(out, "  %d) %s\n", i+1, t)
	}
	fmt.Fprint(out, "Selection (number or name): ")

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("reading selection: %v", err)
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		return "", fmt.Errorf("no selection provided")
	}

	if n, err := strconv.Atoi(choice); err == nil {
		if n < 1 || n > len(templates) {
			return "", fmt.Errorf("selection %d out of range (1..%d)", n, len(templates))
		}
		return templates[n-1], nil
	}

	for _, t := range templates {
		if t == choice {
			return t, nil
		}
	}
	return "", fmt.Errorf("unknown template %q\n\nAvailable templates: %s", choice, strings.Join(templates, ", "))
}

// scaffoldProjectDirs creates ./issues and ./docs if they do not already exist.
// Returns the list of directories that were freshly created, in display order.
func scaffoldProjectDirs() ([]string, error) {
	var created []string
	for _, dir := range []string{"issues", "docs"} {
		if _, err := os.Stat(dir); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("checking %s: %w", dir, err)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", dir, err)
		}
		created = append(created, dir+"/")
	}
	return created, nil
}
