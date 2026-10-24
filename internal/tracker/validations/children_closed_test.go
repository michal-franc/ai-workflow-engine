package validations

import (
	"strings"
	"testing"
)

func TestChildrenClosed(t *testing.T) {
	kids := map[string][]*IssueView{
		"e1": {{Slug: "a", Status: "done"}, {Slug: "b", Status: "parked"}, {Slug: "c", Status: "Obsolete"}},
		"e2": {{Slug: "a", Status: "done"}, {Slug: "d", Status: "in progress"}},
	}
	cfg := Config{Children: func(slug string) []*IssueView { return kids[slug] }}
	act := Action{Rule: "children_closed"}
	if err := ChildrenClosed(act, &IssueView{Slug: "e1"}, cfg); err != nil {
		t.Errorf("all closed: %v", err)
	}
	if err := ChildrenClosed(act, &IssueView{Slug: "none"}, cfg); err != nil {
		t.Errorf("no children: %v", err)
	}
	err := ChildrenClosed(act, &IssueView{Slug: "e2"}, cfg)
	if err == nil || !strings.Contains(err.Error(), "1 of its children are still open: d (in progress)") {
		t.Errorf("open child: %v", err)
	}
	// values: narrows what closes a child.
	if err := ChildrenClosed(Action{Rule: "children_closed", Values: []string{"done"}}, &IssueView{Slug: "e1"}, cfg); err == nil {
		t.Error("values: [done] should leave parked and obsolete open")
	}
	if err := ChildrenClosed(act, &IssueView{Slug: "e1"}, Config{}); err == nil {
		t.Error("no issue store should fail")
	}
}
