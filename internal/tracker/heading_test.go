package tracker

import "testing"

func TestSectionText(t *testing.T) {
	body := "Intro.\n\n## Next\n1. One.\n\n### Detail\nMore.\n\n```\n## Not a heading\n```\n\n## Resume\nRun."
	got, ok := SectionText(body, "next")
	if !ok || got != "1. One.\n\n### Detail\nMore.\n\n```\n## Not a heading\n```" {
		t.Errorf("SectionText = %q, %v", got, ok)
	}
	if _, ok := SectionText(body, "Missing"); ok {
		t.Error("missing section: want ok=false")
	}
}
