package health

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
)

// The storage family: how many MB the project takes on disk, by area, so
// the Stats page shows the growth of worktrees and playtest shots. One walk
// of the main checkout, sizes from Lstat (symlinks are not followed). It
// only knows the present: backfill leaves it out. See docs/workbench-files.md,
// "The storage family".
func init() {
	Register("storage", storageFamily)
	Describe("storage", "card: /stats (Disk use); tidy shots: issue-cli playtest prune")
}

// UnitMB is a size in megabytes (2^20 bytes).
const UnitMB = "MB"

// StorageAreas are the storage family's MB metrics, in the order the Stats
// page lists them, with their labels.
var StorageAreas = []struct{ Metric, Label string }{
	{"storage.worktrees_mb", "worktrees"},
	{"storage.playtest_mb", "playtest shots"},
	{"storage.git_mb", ".git"},
	{"storage.agent_logs_mb", ".agent-logs"},
	{"storage.attachments_mb", "attachments"},
}

type storageSizes struct {
	total, git, worktrees, playtest, agentLogs, attachments int64
	worktreeCount                                           int
}

func storageFamily(ctx context.Context, env Env) ([]Metric, error) {
	if env.Backfill {
		return nil, ErrNoPast
	}
	attachments := ""
	if env.Project != nil && env.Project.IssueDir != "" {
		if abs, err := filepath.Abs(filepath.Join(env.Project.IssueDir, "attachments")); err == nil {
			attachments = abs
		}
	}
	s, err := measureStorage(ctx, env.Root, attachments)
	if err != nil {
		return nil, err
	}
	mb := func(b int64) float64 { return float64(int64(float64(b)/(1<<20)*10+0.5)) / 10 }
	return []Metric{
		{Name: "storage.total_mb", Value: mb(s.total), Unit: UnitMB, Help: "The project checkout on disk, everything included"},
		{Name: "storage.git_mb", Value: mb(s.git), Unit: UnitMB, Help: "The .git folder"},
		{Name: "storage.worktrees_mb", Value: mb(s.worktrees), Unit: UnitMB, Help: "Worktrees under .worktrees/, without their playtest runs"},
		{Name: "storage.worktrees", Value: float64(s.worktreeCount), Unit: UnitCount, Help: "Worktrees under .worktrees/"},
		{Name: "storage.playtest_mb", Value: mb(s.playtest), Unit: UnitMB, Help: "Playtest runs and shots: .playtest/ in the checkout and in every worktree"},
		{Name: "storage.agent_logs_mb", Value: mb(s.agentLogs), Unit: UnitMB, Help: "The .agent-logs folder (clilogs, dispatch prompts)"},
		{Name: "storage.attachments_mb", Value: mb(s.attachments), Unit: UnitMB, Help: "Issue attachments (mockups and their screenshots)"},
	}, nil
}

// measureStorage walks root once and sorts every file's size into its area.
// attachments is the issues' attachments folder; when it is outside root it
// is walked on its own and left out of the total.
func measureStorage(ctx context.Context, root, attachments string) (storageSizes, error) {
	var s storageSizes
	root, _ = filepath.Abs(root)
	n := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable: left out
		}
		if n++; n%2000 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		size := info.Size()
		if strings.HasPrefix(rel, ".worktrees/") && d.Name() == ".git" {
			s.worktreeCount++ // a worktree's .git file (or a nested repo)
		}
		s.total += size
		switch first, _, _ := strings.Cut(rel, "/"); {
		case first == ".git":
			s.git += size
		case first == ".worktrees":
			if strings.Contains(rel, "/.playtest/") {
				s.playtest += size
			} else {
				s.worktrees += size
			}
		case first == ".playtest":
			s.playtest += size
		case first == ".agent-logs":
			s.agentLogs += size
		}
		if attachments != "" && within(attachments, p) {
			s.attachments += size
		}
		return nil
	})
	if err != nil {
		return s, err
	}
	if attachments != "" && !within(root, attachments) {
		filepath.WalkDir(attachments, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if info, err := d.Info(); err == nil {
					s.attachments += info.Size()
				}
			}
			return nil
		})
	}
	return s, nil
}

func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}
