package validations

// Shared helpers and table-driven scaffolding for the per-validator tests.

func sampleIssue() *IssueView {
	return &IssueView{
		Slug:     "sample",
		Title:    "Sample",
		Status:   "in progress",
		System:   "Workflow",
		Priority: "high",
		Number:   42,
		Repo:     "owner/repo",
		Labels:   []string{"bug", "enhancement"},
		BodyRaw:  "## Problem\n\nbody.\n\n## Design\n\nlong enough text to satisfy bounds.\n",
		Frontmatter: map[string]string{
			"title":    "Sample",
			"status":   "in progress",
			"system":   "Workflow",
			"priority": "high",
			"number":   "42",
			"repo":     "owner/repo",
			"pr":       "https://github.com/owner/repo/pull/123",
			"parent":   "workflow/parent",
		},
	}
}
