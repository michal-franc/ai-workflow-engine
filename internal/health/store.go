package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// keepAll is how far back every snapshot is kept; older days keep their
// last snapshot only.
const keepAll = 35 * 24 * time.Hour

// stored is a snapshot file and the time its name says.
type stored struct {
	path string
	at   time.Time
}

// list returns the snapshot files, oldest first. Files whose name is not a
// snapshot time are ignored.
func list(root string) []stored {
	paths, _ := filepath.Glob(filepath.Join(SnapshotsDir(root), "*.json"))
	var out []stored
	for _, p := range paths {
		at, err := time.ParseInLocation(fileLayout, strings.TrimSuffix(filepath.Base(p), ".json"), time.Local)
		if err != nil {
			continue
		}
		out = append(out, stored{p, at})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

// Read parses one snapshot file.
func Read(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// Latest is the newest snapshot and its path; nil, "" when there is none.
func Latest(root string) (*Snapshot, string, error) {
	all := list(root)
	if len(all) == 0 {
		return nil, "", nil
	}
	p := all[len(all)-1].path
	s, err := Read(p)
	return s, p, err
}

// TakenOn reports whether a snapshot was taken on day's local date.
func TakenOn(root string, day time.Time) bool {
	all := list(root)
	return len(all) > 0 && sameDay(all[len(all)-1].at, day.Local())
}

// Before is the newest snapshot taken at or before t; nil when there is
// none. Unreadable files are skipped.
func Before(root string, t time.Time) *Snapshot {
	all := list(root)
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].at.After(t) {
			continue
		}
		if s, err := Read(all[i].path); err == nil {
			return s
		}
	}
	return nil
}

// Count is how many snapshot files there are.
func Count(root string) int { return len(list(root)) }

// HasDay reports whether any snapshot was taken on day's local date.
func HasDay(root string, day time.Time) bool {
	for _, s := range list(root) {
		if sameDay(s.at, day.Local()) {
			return true
		}
	}
	return false
}

// Range is the snapshots taken in [from, to], oldest first. A zero from
// means all of them. Unreadable files are skipped. Files are parsed once
// and kept while their size and time are unchanged, so a stats page reads
// 90 days of snapshots without parsing 90 files on every request.
func Range(root string, from, to time.Time) []*Snapshot {
	var out []*Snapshot
	for _, s := range list(root) {
		if (!from.IsZero() && s.at.Before(from)) || s.at.After(to) {
			continue
		}
		if snap := readCached(s.path); snap != nil {
			out = append(out, snap)
		}
	}
	return out
}

type cachedSnap struct {
	size int64
	mod  time.Time
	snap *Snapshot
}

var (
	snapCacheMu sync.Mutex
	snapCache   = map[string]cachedSnap{}
)

// readCached is Read through the parse cache; nil when the file is gone or
// does not parse. Callers must not change the snapshot.
func readCached(path string) *Snapshot {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	snapCacheMu.Lock()
	c, ok := snapCache[path]
	snapCacheMu.Unlock()
	if ok && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
		return c.snap
	}
	snap, err := Read(path)
	if err != nil {
		return nil
	}
	snapCacheMu.Lock()
	snapCache[path] = cachedSnap{fi.Size(), fi.ModTime(), snap}
	snapCacheMu.Unlock()
	return snap
}

// prune thins snapshots older than keepAll to the last one of each day.
func prune(root string, now time.Time) {
	cutoff := now.Add(-keepAll)
	all := list(root)
	for i, s := range all {
		if !s.at.Before(cutoff) || i+1 >= len(all) {
			continue
		}
		if sameDay(s.at, all[i+1].at) {
			os.Remove(s.path)
		}
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// writeJSONFile writes v atomically: a temp file renamed over path, so a
// reader never sees half a file.
func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// gitOut runs git in dir and returns its trimmed stdout.
func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
