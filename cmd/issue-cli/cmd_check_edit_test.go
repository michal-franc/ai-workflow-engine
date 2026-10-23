package main

import (
	"os"
	"strings"
	"testing"
)

func TestCheckEditKeepsTickAndID(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runCheck(ctx, []string{slug, "D2"}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := runCheckEdit(ctx, []string{slug, "d2", "--text", "D2: second task, reworded"}); err != nil {
		t.Fatalf("runCheckEdit: %v", err)
	}
	assertContains(t, stdout.String(), "✓ Edited D2 [Design #2]\n  was: second task\n  now: second task, reworded")
	data, _ := os.ReadFile(issuePath)
	assertContains(t, string(data), "- [ ] first task\n- [x] second task, reworded\n")
}

func TestCheckEditErrors(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	before, _ := os.ReadFile(issuePath)
	ctx, _, _ := newTestContext(proj, false)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{slug, "D9", "--text", "x"}, "ids in this issue: D1 D2"},
		{[]string{slug, "D1"}, "requires --text"},
		{[]string{slug, "--text", "x"}, "exactly one box id"},
		{[]string{slug, "D1", "D2", "--text", "x"}, "exactly one box id"},
		{[]string{slug, "D1", "--text", `two\nlines`}, "one line"},
	} {
		err := runCheckEdit(ctx, c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: want error containing %q, got %v", c.args, c.want, err)
		}
	}
	if after, _ := os.ReadFile(issuePath); string(after) != string(before) {
		t.Error("a failed check-edit changed the file")
	}
}

func TestReplaceLine(t *testing.T) {
	proj, slug, issuePath := makeSimpleProject(t, "in progress")
	ctx, stdout, _ := newTestContext(proj, false)
	if err := runReplace(ctx, []string{slug, "--section", "Test Plan", "--match", "thing one", "--text", "- thing 1"}); err != nil {
		t.Fatalf("--match: %v", err)
	}
	assertContains(t, stdout.String(), "was: - thing one\n  now: - thing 1")
	// Lines count from the one after "## Design": line 2 is the second box.
	if err := runReplace(ctx, []string{slug, "--section", "Design", "--line", "2", "--text", "- [ ] second task, by line"}); err != nil {
		t.Fatalf("--line: %v", err)
	}
	data, _ := os.ReadFile(issuePath)
	assertContains(t, string(data), "- [ ] first task\n- [ ] second task, by line\n")
	assertContains(t, string(data), "- thing 1\n")

	before := string(data)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--line", "99"}, "no line 99:\n    1 | - [ ] first task"},
		{[]string{"--match", "task"}, "matches 2 lines"},
		{[]string{"--match", "nope"}, "matches no line"},
		{[]string{"--match", "("}, "not a valid regexp"},
		{[]string{"--line", "1", "--match", "x"}, "--line or --match, not both"},
	} {
		args := append([]string{slug, "--section", "Design", "--text", "x"}, c.args...)
		err := runReplace(ctx, args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: want error containing %q, got %v", c.args, c.want, err)
		}
	}
	if after, _ := os.ReadFile(issuePath); string(after) != before {
		t.Error("a failed replace changed the file")
	}
}
