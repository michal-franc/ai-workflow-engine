package tracker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDocPage_WithFrontmatter(t *testing.T) {
	data := []byte(`---
title: "Getting Started"
order: 1
---

Welcome to the docs.
`)

	page, err := ParseDocPage("guide.md", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if page.Title != "Getting Started" {
		t.Errorf("title = %q, want %q", page.Title, "Getting Started")
	}
	if page.Order != 1 {
		t.Errorf("order = %d, want 1", page.Order)
	}
	if page.Slug != "guide" {
		t.Errorf("slug = %q, want %q", page.Slug, "guide")
	}
	if page.Section != "" {
		t.Errorf("section = %q, want empty", page.Section)
	}
}

func TestParseDocPage_WithoutFrontmatter(t *testing.T) {
	data := []byte("Just plain markdown content.")

	page, err := ParseDocPage("my-page.md", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if page.Title != "My Page" {
		t.Errorf("title = %q, want %q", page.Title, "My Page")
	}
	if page.Order != 0 {
		t.Errorf("order = %d, want 0", page.Order)
	}
}

func TestParseDocPage_SubdirSection(t *testing.T) {
	data := []byte("---\ntitle: \"Sub Page\"\n---\n\nContent.")

	page, err := ParseDocPage("guides/sub-page.md", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if page.Section != "guides" {
		t.Errorf("section = %q, want %q", page.Section, "guides")
	}
	if page.Slug != "guides/sub-page" {
		t.Errorf("slug = %q, want %q", page.Slug, "guides/sub-page")
	}
}

func TestParseDocPage_OrderField(t *testing.T) {
	data := []byte("---\ntitle: \"Ordered\"\norder: 5\n---\n\nContent.")

	page, err := ParseDocPage("ordered.md", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if page.Order != 5 {
		t.Errorf("order = %d, want 5", page.Order)
	}
}

func TestLoadDocs(t *testing.T) {
	dir := t.TempDir()

	// Root doc
	os.WriteFile(filepath.Join(dir, "index.md"), []byte("---\ntitle: \"Index\"\norder: 1\n---\n\nHome."), 0644)

	// Subdirectory doc
	subDir := filepath.Join(dir, "guides")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "setup.md"), []byte("---\ntitle: \"Setup\"\norder: 1\n---\n\nSetup guide."), 0644)

	// Another root doc
	os.WriteFile(filepath.Join(dir, "faq.md"), []byte("---\ntitle: \"FAQ\"\norder: 2\n---\n\nQuestions."), 0644)

	pages, err := LoadDocs(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pages) != 3 {
		t.Fatalf("got %d pages, want 3", len(pages))
	}

	// Check sorting: root pages first (by order), then subdirectory pages
	if pages[0].Title != "Index" {
		t.Errorf("first page = %q, want %q", pages[0].Title, "Index")
	}
	if pages[1].Title != "FAQ" {
		t.Errorf("second page = %q, want %q", pages[1].Title, "FAQ")
	}
	if pages[2].Section != "guides" {
		t.Errorf("third page section = %q, want %q", pages[2].Section, "guides")
	}
}

func TestLoadDocs_NonexistentDir(t *testing.T) {
	pages, err := LoadDocs("/nonexistent/docs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pages != nil {
		t.Errorf("expected nil for nonexistent dir, got %v", pages)
	}
}

func TestLoadDocs_SkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "visible.md"), []byte("---\ntitle: \"Visible\"\n---\n\nContent."), 0644)

	hiddenDir := filepath.Join(dir, ".hidden")
	os.MkdirAll(hiddenDir, 0755)
	os.WriteFile(filepath.Join(hiddenDir, "secret.md"), []byte("---\ntitle: \"Secret\"\n---\n\nHidden."), 0644)

	pages, err := LoadDocs(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1 (hidden dir should be skipped)", len(pages))
	}
}

func TestGroupDocSections(t *testing.T) {
	pages := []*DocPage{
		{Title: "Index", Section: ""},
		{Title: "FAQ", Section: ""},
		{Title: "Setup", Section: "guides"},
		{Title: "Deploy", Section: "guides"},
		{Title: "API", Section: "reference"},
	}

	sections := GroupDocSections(pages)

	if len(sections) != 3 {
		t.Fatalf("got %d sections, want 3", len(sections))
	}

	// Preserves insertion order
	if sections[0].Name != "" || len(sections[0].Pages) != 2 {
		t.Errorf("section[0] = {Name: %q, Pages: %d}", sections[0].Name, len(sections[0].Pages))
	}
	if sections[1].Name != "guides" || len(sections[1].Pages) != 2 {
		t.Errorf("section[1] = {Name: %q, Pages: %d}", sections[1].Name, len(sections[1].Pages))
	}
	if sections[2].Name != "reference" || len(sections[2].Pages) != 1 {
		t.Errorf("section[2] = {Name: %q, Pages: %d}", sections[2].Name, len(sections[2].Pages))
	}
}

func TestGroupDocSections_Empty(t *testing.T) {
	sections := GroupDocSections(nil)
	if len(sections) != 0 {
		t.Errorf("expected 0 sections, got %d", len(sections))
	}
}
