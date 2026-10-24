package main

import (
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/proof"
	"github.com/michal-franc/issue-viewer/internal/timeline"
	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// evChange is the Evidence tab's Change card: the work branch's own
// first-parent commits (the range the proof checks read) and their files by
// area.
type evChange struct {
	Commits    []evCommit
	Merges     int
	Files      int
	Add, Del   int
	TestAdd    int
	Areas      []evArea
	Range      string
	Unreadable string
}

type evCommit struct {
	SHA, Subject string
	At           time.Time
	When         string
	Files        int
}

type evArea struct {
	Name            string
	Files, Add, Del int
	// Pct is Add against the largest area's, for the bar.
	Pct int
}

// loadEvidenceChange reads the commits and their numstat in one git log.
func loadEvidenceChange(root, ref, refName string, stats tracker.StatsStore) evChange {
	if ref == "" {
		return evChange{Unreadable: "No work branch, recorded head or merge sha: commits can't be listed."}
	}
	rng := proof.LogRange(root, ref, stats)
	if rng == nil {
		return evChange{Unreadable: "No dispatch or transition recorded: the merged branch's own commits can't be told apart."}
	}
	args := append([]string{"-C", root, "log", "--first-parent", "--numstat", "--format=%x1e%H%x1f%P%x1f%ct%x1f%s"}, rng...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return evChange{Unreadable: "git log on " + refName + ": " + err.Error()}
	}
	c := parseEvidenceChange(string(out))
	c.Range = refName
	return c
}

// parseEvidenceChange reads `git log --numstat` records that start with
// \x1e: sha, parents, time, subject, then one "add\tdel\tpath" per file.
func parseEvidenceChange(out string) evChange {
	var c evChange
	areas := map[string]*evArea{}
	files := map[string]bool{}
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		head, body, _ := strings.Cut(rec, "\n")
		f := strings.SplitN(head, "\x1f", 4)
		if len(f) < 4 {
			continue
		}
		if len(strings.Fields(f[1])) > 1 {
			c.Merges++
			continue
		}
		at, _ := strconv.ParseInt(f[2], 10, 64)
		cm := evCommit{SHA: timeline.ShortSHA(f[0]), Subject: f[3], At: time.Unix(at, 0)}
		for _, line := range strings.Split(body, "\n") {
			parts := strings.SplitN(strings.TrimSpace(line), "\t", 3)
			if len(parts) < 3 {
				continue
			}
			add, _ := strconv.Atoi(parts[0]) // "-" for binary files: 0
			del, _ := strconv.Atoi(parts[1])
			path := numstatPath(parts[2])
			cm.Files++
			c.Add += add
			c.Del += del
			if proof.IsTestPath(path) {
				c.TestAdd += add
			}
			name := changeArea(path)
			a := areas[name]
			if a == nil {
				a = &evArea{Name: name}
				areas[name] = a
			}
			if !files[path] {
				files[path] = true
				a.Files++
			}
			a.Add += add
			a.Del += del
		}
		c.Commits = append(c.Commits, cm)
	}
	c.Files = len(files)
	maxAdd := 0
	for _, a := range areas {
		c.Areas = append(c.Areas, *a)
		maxAdd = max(maxAdd, a.Add)
	}
	sort.Slice(c.Areas, func(i, j int) bool {
		if c.Areas[i].Add != c.Areas[j].Add {
			return c.Areas[i].Add > c.Areas[j].Add
		}
		return c.Areas[i].Name < c.Areas[j].Name
	})
	for i := range c.Areas {
		if maxAdd > 0 {
			c.Areas[i].Pct = max(1, c.Areas[i].Add*100/maxAdd)
		}
	}
	return c
}

// numstatPath turns a rename ("a/{x => y}/b", "x => y") into its new path.
func numstatPath(p string) string {
	if i := strings.Index(p, "{"); i >= 0 {
		if j := strings.Index(p[i:], "}"); j > 0 {
			inner := p[i+1 : i+j]
			if _, to, ok := strings.Cut(inner, " => "); ok {
				return strings.ReplaceAll(p[:i]+to+p[i+j+1:], "//", "/")
			}
		}
	}
	if _, to, ok := strings.Cut(p, " => "); ok {
		return to
	}
	return p
}

// changeArea is a file's area: its first folder, two for internal/ and
// cmd/, and "(root)" for files at the top.
func changeArea(path string) string {
	parts := strings.Split(path, "/")
	switch {
	case len(parts) == 1:
		return "(root)"
	case (parts[0] == "internal" || parts[0] == "cmd") && len(parts) > 2:
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}
