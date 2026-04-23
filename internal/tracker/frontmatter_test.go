package tracker

import (
	"testing"
)

func TestParseFrontmatter_Valid(t *testing.T) {
	content := `---
title: "Hello"
status: "idea"
---

Body content here.`

	var dest struct {
		Title  string `yaml:"title"`
		Status string `yaml:"status"`
	}

	body, err := ParseFrontmatter(content, &dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dest.Title != "Hello" {
		t.Errorf("title = %q, want %q", dest.Title, "Hello")
	}
	if dest.Status != "idea" {
		t.Errorf("status = %q, want %q", dest.Status, "idea")
	}
	if body != "Body content here." {
		t.Errorf("body = %q, want %q", body, "Body content here.")
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	content := "Just plain text"
	var dest struct{}

	_, err := ParseFrontmatter(content, &dest)
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestParseFrontmatter_InvalidYAML(t *testing.T) {
	content := "---\n: invalid: yaml: [broken\n---\nBody"
	var dest struct{}

	_, err := ParseFrontmatter(content, &dest)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseFrontmatter_MissingClosingDelimiter(t *testing.T) {
	content := "---\ntitle: \"Hello\"\n"
	var dest struct{}

	_, err := ParseFrontmatter(content, &dest)
	if err == nil {
		t.Fatal("expected error for missing closing delimiter")
	}
}

func TestParseFrontmatter_EmptyBody(t *testing.T) {
	content := "---\ntitle: \"Test\"\n---\n"
	var dest struct {
		Title string `yaml:"title"`
	}

	body, err := ParseFrontmatter(content, &dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != "" {
		t.Errorf("body = %q, want empty", body)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"spaces", "Hello World", "hello-world"},
		{"special chars", "Hello @World! #123", "hello-world-123"},
		{"uppercase", "ALL CAPS TITLE", "all-caps-title"},
		{"underscores", "snake_case_title", "snake-case-title"},
		{"dots", "version.1.0", "version-1-0"},
		{"unicode", "caf\u00e9 latte", "caf-latte"},
		{"multiple spaces", "too   many   spaces", "too-many-spaces"},
		{"hyphens preserved", "already-slugified", "already-slugified"},
		{"numbers", "issue 42", "issue-42"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slugify(tt.input)
			if got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
