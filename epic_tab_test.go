package main

import (
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// TestBuildEpicTab: an epic's children by version, oldest first, open ones
// first within a version; the counts drive the tab's done/total chip.
func TestBuildEpicTab(t *testing.T) {
	wf := groupsWorkflow()
	epic := &tracker.Issue{Slug: "e", Title: "E", Status: "in progress", Type: "epic"}
	issues := []*tracker.Issue{epic,
		iss("a", "done", "", "0.2", "e"), iss("b", "idea", "", "0.2", "e"), iss("c", "idea", "", "0.1", "e"), iss("d", "done", "", "", "e"),
		iss("x", "idea", "", "0.1", ""),
	}
	tab := buildEpicTab(wf, epic, issues)
	if tab == nil || tab.Done != 2 || tab.Total != 4 || tab.AllDone() {
		t.Fatalf("tab = %+v", tab)
	}
	var got []string
	for _, v := range tab.Versions {
		got = append(got, v.Version)
	}
	if len(got) != 3 || got[0] != "0.1" || got[1] != "0.2" {
		t.Errorf("versions = %v, want 0.1, 0.2, none", got)
	}
	if v := tab.Versions[1]; v.Children[0].Slug != "b" || v.Pct() != 50 {
		t.Errorf("0.2: first %s pct %d, want b (open first) and 50", v.Children[0].Slug, v.Pct())
	}
	if buildEpicTab(wf, issues[5], issues) != nil {
		t.Error("a plain issue gets no Epic tab")
	}
}
