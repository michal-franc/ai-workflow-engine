package tracker

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitSections_LevelTwoOnlyAndFencesIgnored(t *testing.T) {
	body := "# Title\n\nIntro.\n\n## Rules\n\nCards.\n\n### Detail\n\nStill rules.\n\n```\n## not a heading\n```\n\n## Tempo\n\nFast.\n\n# Appendix\n\nOutside.\n"
	got := SplitSections(body)
	if len(got) != 2 || got[0].Title != "Rules" || got[1].Title != "Tempo" {
		t.Fatalf("sections = %+v", got)
	}
	if want := "Cards.\n\n### Detail\n\nStill rules.\n\n```\n## not a heading\n```"; got[0].Text != want {
		t.Errorf("Rules text = %q", got[0].Text)
	}
	if got[1].Text != "Fast." {
		t.Errorf("Tempo text = %q (a level-1 heading ends it)", got[1].Text)
	}
}

func TestSectionAnchors_MatchDocPageIDs(t *testing.T) {
	body := "## Key Files\n\nx\n\n## 5. Tuning knobs (`Sim/Tuning.cs`)\n\ny\n\n## Key Files\n"
	got := SectionAnchors(body)
	want := []string{"key-files", "5-tuning-knobs-simtuningcs", "key-files-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("anchors = %q, want %q", got, want)
	}
	page, err := ParseDocPage("ui/overview.md", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range want {
		if !strings.Contains(page.BodyHTML, `id="`+id+`"`) {
			t.Errorf("doc page has no heading id %q", id)
		}
	}
}

func TestDecisionIndex_CitedIn(t *testing.T) {
	issues := []*Issue{
		{Slug: "ai/shouts", Status: "done", BodyRaw: "## Decisions\n- **D-1** · 2026-10-01 · chat: Shouts, \"ok\"\n- **D-2** · 2026-10-03 · chat: Tempo, \"yes\" (replaces ai/shouts#D-1)\n"},
	}
	x := BuildDecisionIndex(issues, func(*Issue) bool { return false })
	got := x.CitedIn("See ai/shouts#D-1 and unknown/slug#D-9.\n```\nai/shouts#D-2\n```")
	if len(got) != 1 || got[0].ID != "D-1" || got[0].ReplacedBy == nil || got[0].ReplacedBy.ID != "D-2" {
		t.Fatalf("CitedIn = %+v", got)
	}
	if (*DecisionIndex)(nil).CitedIn("ai/shouts#D-1") != nil {
		t.Error("nil index cites something")
	}
}
