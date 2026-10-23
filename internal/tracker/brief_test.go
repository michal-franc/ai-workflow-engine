package tracker

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBrief(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"empty", "", ""},
		{"lead paragraph", "The tab answers in 10 seconds.\nWhat it needs.\n\n## Idea\nMore.", "The tab answers in 10 seconds. What it needs."},
		{"in short wins", "Lead text.\n\n## Idea\n\n**In short:** the real brief.\n\nOther.", "The real brief."},
		{"in short without colon inside", "**In short**: keep this.", "Keep this."},
		{"bug first line", "## Bug\n\n- [ ] repro\nThe CLI drops the flag.\nMore.\n", "The CLI drops the flag."},
		{"lead skips table and image", "| a | b |\n|--|--|\n\n![x](y.png)\n\nReal lead.\n", "Real lead."},
		{"first section paragraph", "## Why\n\n- list\n\nOwners rarely read issues.\n", "Owners rarely read issues."},
		{"fenced code is not prose", "```\nnot this\n```\n\nThis one.", "This one."},
		{"html comment skipped", "<!-- data\nhidden\n-->\nShown.", "Shown."},
		{"numbered list skipped", "1. step\n\nProse.", "Prose."},
		{"only structure", "## Idea\n- [ ] box\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Brief(c.body); got != c.want {
				t.Errorf("Brief() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestBriefCutsLongText(t *testing.T) {
	long := strings.Repeat("word ", 40) + "ends here. " + strings.Repeat("more words ", 40)
	got := Brief(long)
	if !strings.HasSuffix(got, "ends here.") {
		t.Errorf("want cut at the sentence end, got %q", got)
	}

	noStop := strings.Repeat("word ", 100)
	got = Brief(noStop)
	if !strings.HasSuffix(got, "…") || utf8.RuneCountInString(got) > BriefMax+1 {
		t.Errorf("want a word cut with an ellipsis within %d runes, got %d: %q", BriefMax, utf8.RuneCountInString(got), got)
	}
}

func TestLead(t *testing.T) {
	if got := Lead("- [ ] box\n\n### Sub\n\nFirst prose. Second sentence here.\n", 20); got != "First prose." {
		t.Errorf("Lead() = %q", got)
	}
	if got := Lead("- [ ] only boxes\n", 80); got != "" {
		t.Errorf("Lead() of structure = %q, want empty", got)
	}
}

func TestLeadIsPlainText(t *testing.T) {
	got := Lead("**In short:** the `tab` reads [the docs](docs/x.md) and **bold**.", 200)
	if got != "The tab reads the docs and bold." {
		t.Errorf("Lead() = %q", got)
	}
}
