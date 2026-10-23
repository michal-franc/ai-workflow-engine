// Command clilog-scrub brings a project's existing issue-cli logs in line
// with v0.54, once: it merges every worktree's .agent-logs into the project's,
// replaces free text in every clilog with a placeholder, and removes the old
// $TMPDIR/issue-cli-logs/actions.jsonl. Originals are copied to a backup dir
// first; delete it once the timeline looks right.
//
//	go run ./tools/clilog-scrub -root ~/Work/issue-viewer [-dry-run]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/michal-franc/issue-viewer/internal/clilog"
)

func main() {
	root := flag.String("root", ".", "the project's primary checkout")
	backupDir := flag.String("backup", "", "where originals are copied (default <root>/.agent-logs/.scrub-backup-<date>)")
	dryRun := flag.Bool("dry-run", false, "report what would change without touching anything")
	keepLegacy := flag.Bool("keep-legacy", false, "leave $TMPDIR/issue-cli-logs/actions.jsonl in place")
	flag.Parse()

	abs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if *backupDir == "" {
		*backupDir = filepath.Join(abs, ".agent-logs", ".scrub-backup-"+time.Now().Format("20060102-150405"))
	}
	opt := clilog.ScrubOptions{Root: abs, BackupDir: *backupDir, DryRun: *dryRun}
	if !*keepLegacy {
		opt.Legacy = clilog.LegacyPath()
	}

	rep, err := clilog.Scrub(opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	verb := "Scrubbed"
	if *dryRun {
		verb = "Would scrub"
	}
	fmt.Printf("%s %s\n", verb, abs)
	fmt.Printf("  worktree clilogs merged into the project: %d (plus %d prompt/message files copied)\n", rep.Merged, rep.Copied)
	fmt.Printf("  clilog files rewritten without free text: %d (%d records)\n", rep.Rewritten, rep.Records)
	if rep.LegacyRemoved {
		fmt.Printf("  removed %s\n", opt.Legacy)
	}
	if !*dryRun {
		fmt.Printf("Originals: %s (delete it once the timeline looks right)\n", *backupDir)
	}
}
