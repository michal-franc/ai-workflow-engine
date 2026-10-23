package clilog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/telemetry"
)

// ScrubOptions configures Scrub.
type ScrubOptions struct {
	// Root is the project's primary checkout; its .agent-logs/ is where every
	// agent's logs end up.
	Root string
	// BackupDir receives a copy of each file before Scrub changes or removes
	// it, at its path relative to Root (the legacy log under tmp/).
	BackupDir string
	// Legacy is the pre-v0.54 $TMPDIR log to remove; "" leaves it.
	Legacy string
	// DryRun reports what would change without touching anything.
	DryRun bool
}

// ScrubReport is what Scrub did, or would do on a dry run.
type ScrubReport struct {
	// Merged counts worktree clilogs folded into the project's copy, and
	// Copied the prompt and message files brought over with them.
	Merged, Copied int
	// Rewritten counts clilog files whose records lost free text, Records
	// the records changed in them.
	Rewritten, Records int
	// LegacyRemoved is true when the legacy log was (or would be) removed.
	LegacyRemoved bool
}

// maxWorktreeDepth is how deep under .worktrees/ an .agent-logs dir can be:
// .worktrees/<system>/<slug>/.agent-logs.
const maxWorktreeDepth = 3

// Scrub brings existing logs in line with v0.54, once:
//
//  1. Every worktree's .agent-logs/<name>/ is merged into the project's: the
//     two clilogs are unioned (deduplicated by start, pid and args, sorted by
//     start) and dispatch-prompt.txt and messages/ files are copied where the
//     project has none, so removing a worktree loses no agent history.
//     Anything else there (test sandboxes) is left alone.
//  2. Every clilog under the project and its worktrees is rewritten with
//     Redact, so no record keeps free text. Malformed lines are dropped.
//  3. The legacy $TMPDIR log is removed.
//
// Each rewrite holds the file's append lock.
func Scrub(opt ScrubOptions) (ScrubReport, error) {
	var rep ScrubReport
	projectLogs := filepath.Join(opt.Root, ".agent-logs")

	worktreeLogs, err := findWorktreeLogs(filepath.Join(opt.Root, ".worktrees"))
	if err != nil {
		return rep, err
	}
	for _, wl := range worktreeLogs {
		entries, err := os.ReadDir(wl)
		if err != nil {
			return rep, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if err := mergeAgentDir(opt, filepath.Join(wl, e.Name()), filepath.Join(projectLogs, e.Name()), &rep); err != nil {
				return rep, err
			}
		}
	}

	for _, dir := range append([]string{projectLogs}, worktreeLogs...) {
		files, err := clilogFiles(dir)
		if err != nil {
			return rep, err
		}
		for _, f := range files {
			n, err := redactFile(opt, f)
			if err != nil {
				return rep, err
			}
			if n > 0 {
				rep.Rewritten++
				rep.Records += n
			}
		}
	}

	if opt.Legacy != "" {
		if _, err := os.Stat(opt.Legacy); err == nil {
			rep.LegacyRemoved = true
			if !opt.DryRun {
				if err := backup(opt, opt.Legacy, filepath.Join("tmp", filepath.Base(opt.Legacy))); err != nil {
					return rep, err
				}
				if err := os.Remove(opt.Legacy); err != nil {
					return rep, err
				}
				os.Remove(opt.Legacy + ".lock")
			}
		}
	}
	return rep, nil
}

// findWorktreeLogs lists the .agent-logs dirs of the worktrees under dir.
func findWorktreeLogs(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == dir {
				return filepath.SkipAll
			}
			return err
		}
		if !d.IsDir() || path == dir {
			return nil
		}
		if d.Name() == ".agent-logs" {
			out = append(out, path)
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(dir, path)
		if strings.Count(rel, string(filepath.Separator))+1 >= maxWorktreeDepth {
			return filepath.SkipDir
		}
		return nil
	})
	return out, err
}

// clilogFiles are the clilogs directly in dir (clilog.jsonl) and in its
// per-agent subdirs (<name>/<name>.clilog), rotated copies included.
func clilogFiles(dir string) ([]string, error) {
	var files []string
	for _, pattern := range []string{FileName, FileName + ".1", "*/*.clilog", "*/*.clilog.1"} {
		m, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, err
		}
		files = append(files, m...)
	}
	return files, nil
}

// mergeAgentDir folds a worktree's agent dir src into the project's dst.
func mergeAgentDir(opt ScrubOptions, src, dst string, rep *ScrubReport) error {
	name := filepath.Base(src)
	srcLog, dstLog := filepath.Join(src, name+".clilog"), filepath.Join(dst, name+".clilog")
	if fileExists(srcLog) || fileExists(srcLog+".1") {
		rep.Merged++
		if !opt.DryRun {
			if err := mergeClilogs(opt, srcLog, dstLog); err != nil {
				return err
			}
		}
	}
	var keep []string
	if fileExists(filepath.Join(src, "dispatch-prompt.txt")) {
		keep = append(keep, filepath.Join(src, "dispatch-prompt.txt"))
	}
	messages, err := filepath.Glob(filepath.Join(src, "messages", "*"))
	if err != nil {
		return err
	}
	keep = append(keep, messages...)
	for _, path := range keep {
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if fileExists(target) {
			continue
		}
		rep.Copied++
		if opt.DryRun {
			continue
		}
		if err := copyFile(path, target); err != nil {
			return err
		}
	}
	return nil
}

// mergeClilogs writes the union of src's and dst's records to dst, oldest
// first, and drops dst's rotated copy (now inside dst).
func mergeClilogs(opt ScrubOptions, src, dst string) error {
	unlock, err := lockFor(dst)
	if err != nil {
		return err
	}
	defer unlock()
	if err := backupRel(opt, dst); err != nil {
		return err
	}
	if err := backupRel(opt, dst+".1"); err != nil {
		return err
	}
	seen := map[string]bool{}
	var recs []Record
	add := func(r Record) {
		key := fmt.Sprintf("%s|%d|%s", r.TS, r.PID, strings.Join(Redact(r.Args), "\x00"))
		if !seen[key] {
			seen[key] = true
			recs = append(recs, r)
		}
	}
	Read(dst, add)
	Read(src, add)
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Start().Before(recs[j].Start()) })
	if err := writeRecords(dst, recs); err != nil {
		return err
	}
	if err := os.Remove(dst + ".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// redactFile rewrites path with every record's args redacted and returns how
// many records changed; an unchanged file is left alone.
func redactFile(opt ScrubOptions, path string) (int, error) {
	if !opt.DryRun {
		unlock, err := lockFor(path)
		if err != nil {
			return 0, err
		}
		defer unlock()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	changed, dropped := 0, false
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec Record
		if json.Unmarshal(line, &rec) != nil {
			dropped = true
			continue
		}
		red := Redact(rec.Args)
		if !equalArgs(red, rec.Args) {
			changed++
			rec.Args = red
		}
		enc, err := json.Marshal(rec)
		if err != nil {
			return 0, err
		}
		out.Write(enc)
		out.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}
	if changed == 0 && !dropped {
		return 0, nil
	}
	if opt.DryRun {
		return changed, nil
	}
	if err := backupRel(opt, path); err != nil {
		return 0, err
	}
	return changed, writeAtomic(path, out.Bytes())
}

func writeRecords(path string, recs []Record) error {
	var buf bytes.Buffer
	for _, r := range recs {
		enc, err := json.Marshal(r)
		if err != nil {
			return err
		}
		buf.Write(enc)
		buf.WriteByte('\n')
	}
	return writeAtomic(path, buf.Bytes())
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.scrub-%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// lockFor takes path's append lock, creating its dir when needed.
func lockFor(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return telemetry.LockFile(path, 5*time.Second)
}

// backupRel copies path (when it exists) into the backup dir at its path
// relative to the project root, or under worktrees/... for other paths.
func backupRel(opt ScrubOptions, path string) error {
	rel, err := filepath.Rel(opt.Root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = filepath.Join("other", filepath.Base(path))
	}
	return backup(opt, path, rel)
}

func backup(opt ScrubOptions, path, rel string) error {
	if opt.BackupDir == "" || !fileExists(path) {
		return nil
	}
	target := filepath.Join(opt.BackupDir, rel)
	if fileExists(target) {
		return nil // first copy wins: it is the original
	}
	return copyFile(path, target)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
