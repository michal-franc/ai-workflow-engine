package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// makeLookupProject builds a temp project with one minimal issue per slug
// ("sim/alpha" → issues/sim/alpha.md titled "alpha").
func makeLookupProject(t *testing.T, slugs ...string) *tracker.Project {
	t.Helper()
	issuesDir := filepath.Join(t.TempDir(), "issues")
	for _, slug := range slugs {
		path := filepath.Join(issuesDir, filepath.FromSlash(slug)+".md")
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		body := "---\ntitle: \"" + filepath.Base(filepath.FromSlash(slug)) + "\"\nstatus: \"in progress\"\n---\n\nbody\n"
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatalf("write issue: %v", err)
		}
	}
	return &tracker.Project{Name: "test", Slug: "test", IssueDir: issuesDir, Version: "0.1"}
}

func TestFindIssueResolution(t *testing.T) {
	cases := []struct {
		name     string
		slugs    []string
		arg      string
		want     string
		wantNote bool
	}{
		{"exact", []string{"sim/alpha", "sim/alpha-two"}, "sim/alpha", "sim/alpha", false},
		{"exact ignores case and .md", []string{"sim/alpha"}, "SIM/Alpha.md", "sim/alpha", false},
		{"unique suffix beats substring", []string{"sim/alpha", "sim/alpha-two"}, "alpha", "sim/alpha", false},
		{"unique substring", []string{"sim/alpha", "sim/beta"}, "alph", "sim/alpha", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, stderr := newTestContext(makeLookupProject(t, tc.slugs...), false)
			issue, _, err := findIssueOrErr(ctx, tc.arg)
			if err != nil {
				t.Fatalf("findIssueOrErr(%q): %v", tc.arg, err)
			}
			if issue.Slug != tc.want {
				t.Fatalf("resolved %q to %s, want %s", tc.arg, issue.Slug, tc.want)
			}
			note := `resolved "` + tc.arg + `" to ` + tc.want
			if got := strings.Contains(stderr.String(), note); got != tc.wantNote {
				t.Fatalf("stderr note present = %v, want %v; stderr: %q", got, tc.wantNote, stderr.String())
			}
		})
	}
}

func TestFindIssueAmbiguousListsCandidates(t *testing.T) {
	cases := []struct {
		name  string
		slugs []string
		arg   string
		want  []string
	}{
		{"substring", []string{"sim/alpha", "sim/beta", "cli/other"}, "sim", []string{"sim/alpha", "sim/beta"}},
		{"suffix in two systems", []string{"sim/foo", "cli/foo"}, "foo", []string{"cli/foo", "sim/foo"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, _ := newTestContext(makeLookupProject(t, tc.slugs...), false)
			_, _, err := findIssueOrErr(ctx, tc.arg)
			if err == nil {
				t.Fatalf("findIssueOrErr(%q): expected ambiguity error", tc.arg)
			}
			msg := err.Error()
			assertContains(t, msg, `ambiguous slug "`+tc.arg+`": `)
			for _, s := range tc.want {
				assertContains(t, msg, s)
			}
			if strings.Contains(msg, "cli/other") {
				t.Fatalf("non-matching slug listed: %s", msg)
			}
		})
	}
}

func TestFindIssueAmbiguousCapsList(t *testing.T) {
	var slugs []string
	for i := 0; i < ambiguousSlugListMax+3; i++ {
		slugs = append(slugs, "sim/item-"+string(rune('a'+i)))
	}
	ctx, _, _ := newTestContext(makeLookupProject(t, slugs...), false)
	_, _, err := findIssueOrErr(ctx, "item")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	assertContains(t, err.Error(), "13 issues match")
	assertContains(t, err.Error(), "… and 3 more (issue-cli list)")
	if strings.Contains(err.Error(), "sim/item-m") {
		t.Fatalf("list not capped: %s", err)
	}
}

func TestRetrospectiveSystemNameIsAmbiguous(t *testing.T) {
	proj := makeLookupProject(t, "sim/alpha", "sim/beta")
	ctx, stdout, _ := newTestContext(proj, false)
	err := runRetrospective(ctx, []string{"Sim", "--body", "x"})
	if err == nil {
		t.Fatalf("retrospective Sim succeeded: %s", stdout.String())
	}
	assertContains(t, err.Error(), `ambiguous slug "Sim": 2 issues match`)
	if _, statErr := os.Stat(filepath.Join(projectRoot(proj), "retros")); !os.IsNotExist(statErr) {
		t.Fatalf("retros/ was created on an ambiguous slug (stat err %v)", statErr)
	}
}

func TestAppendAmbiguousSlugWritesNothing(t *testing.T) {
	proj := makeLookupProject(t, "sim/alpha", "sim/beta")
	ctx, _, _ := newTestContext(proj, false)
	if err := runAppend(ctx, []string{"sim", "--body", "## Notes\nx"}); err == nil {
		t.Fatal("append on an ambiguous slug succeeded")
	}
	for _, slug := range []string{"sim/alpha", "sim/beta"} {
		data, err := os.ReadFile(filepath.Join(proj.IssueDir, slug+".md"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "## Notes") {
			t.Fatalf("%s was changed by an ambiguous append", slug)
		}
	}
}
