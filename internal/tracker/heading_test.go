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

func TestAppendGateSection(t *testing.T) {
	gate := "- [ ] Tests passing\n- [ ] Results logged"
	cases := []struct {
		name    string
		body    string
		heading string
		content string
		want    string
		added   SectionAppend
	}{
		{
			name:    "creates the section when missing",
			body:    "## Design\n- [x] done",
			heading: "Testing", content: gate,
			want:  "## Design\n- [x] done\n\n## Testing\n- [ ] Tests passing\n- [ ] Results logged\n",
			added: SectionAppend{Title: "Testing", Created: true, BoxesAdded: 2},
		},
		{
			name:    "a deeper heading with the same title does not count",
			body:    "## Design\n### Testing\nUnit tests only.\n\n## Notes\nx",
			heading: "Testing", content: gate,
			want:  "## Design\n### Testing\nUnit tests only.\n\n## Notes\nx\n\n## Testing\n- [ ] Tests passing\n- [ ] Results logged\n",
			added: SectionAppend{Title: "Testing", Created: true, BoxesAdded: 2},
		},
		{
			name:    "a heading inside a code fence does not count",
			body:    "## Design\n```\n## Testing\n```",
			heading: "Testing", content: gate,
			want:  "## Design\n```\n## Testing\n```\n\n## Testing\n- [ ] Tests passing\n- [ ] Results logged\n",
			added: SectionAppend{Title: "Testing", Created: true, BoxesAdded: 2},
		},
		{
			name:    "an existing section gets only the missing boxes",
			body:    "## Testing\n- [x] tests passing\n- [ ] Ran it by hand\n\n## Notes\nx",
			heading: "Testing", content: gate,
			want:  "## Testing\n- [x] tests passing\n- [ ] Ran it by hand\n- [ ] Results logged\n\n## Notes\nx\n",
			added: SectionAppend{Title: "Testing", BoxesAdded: 1},
		},
		{
			name:    "an existing section with prose gets the boxes after a blank line",
			body:    "## Testing\nWritten by hand.",
			heading: "Testing", content: gate,
			want:  "## Testing\nWritten by hand.\n\n- [ ] Tests passing\n- [ ] Results logged\n",
			added: SectionAppend{Title: "Testing", BoxesAdded: 2},
		},
		{
			name:    "an existing section with every box is left alone",
			body:    "## Testing\n- [x] Tests passing\n- [x] Results logged",
			heading: "Testing", content: gate,
			want:  "## Testing\n- [x] Tests passing\n- [x] Results logged",
			added: SectionAppend{Title: "Testing"},
		},
		{
			name:    "a box in a second same-name section counts as present",
			body:    "## Testing\n- [ ] Tests passing\n\n## Testing\n- [x] Results logged",
			heading: "Testing", content: gate,
			want:  "## Testing\n- [ ] Tests passing\n\n## Testing\n- [x] Results logged",
			added: SectionAppend{Title: "Testing"},
		},
		{
			name:    "lead boxes go before the section's sub-headings",
			body:    "## Testing\n- [x] Tests passing\n\n### Log\nran it",
			heading: "Testing", content: gate,
			want:  "## Testing\n- [x] Tests passing\n- [ ] Results logged\n\n### Log\nran it\n",
			added: SectionAppend{Title: "Testing", BoxesAdded: 1},
		},
		{
			name:    "sub-heading boxes go under the matching sub-heading",
			body:    "## Test Plan\n### Automated\n- go test ./...\n\n### Manual\n- [ ] Manual verification steps listed if needed\n\n## Notes\nx",
			heading: "Test Plan", content: "### Automated\n- [ ] Automated verification recorded\n\n### Manual\n- [ ] Manual verification steps listed if needed",
			want:  "## Test Plan\n### Automated\n- go test ./...\n- [ ] Automated verification recorded\n\n### Manual\n- [ ] Manual verification steps listed if needed\n\n## Notes\nx\n",
			added: SectionAppend{Title: "Test Plan", BoxesAdded: 1},
		},
		{
			name:    "a missing sub-heading is appended with its boxes",
			body:    "## Test Plan\nSee below.",
			heading: "Test Plan", content: "### Automated\n- [ ] Automated verification recorded",
			want:  "## Test Plan\nSee below.\n\n### Automated\n- [ ] Automated verification recorded\n",
			added: SectionAppend{Title: "Test Plan", BoxesAdded: 1},
		},
		{
			name:    "an explicit heading line sets the level",
			body:    "### Checks\n- [ ] Tests passing",
			heading: "### Checks", content: gate,
			want:  "### Checks\n- [ ] Tests passing\n- [ ] Results logged\n",
			added: SectionAppend{Title: "Checks", BoxesAdded: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, added := appendGateSection(tc.body, tc.heading, tc.content)
			if got != tc.want {
				t.Errorf("body =\n%q\nwant\n%q", got, tc.want)
			}
			if added != tc.added {
				t.Errorf("added = %+v, want %+v", added, tc.added)
			}
		})
	}
}
