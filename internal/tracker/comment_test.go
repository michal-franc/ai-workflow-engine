package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseComments_NoComments(t *testing.T) {
	body, comments := ParseComments("Just body text")
	if body != "Just body text" {
		t.Errorf("body = %q", body)
	}
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
}

func TestParseComments_WithComments(t *testing.T) {
	content := `Body text here

<!-- issue-viewer-comments
{"id":1,"block":0,"date":"2025-01-15","text":"first comment","status":"open","source":"cli"}
{"id":2,"block":0,"date":"2025-01-16","text":"second comment","status":"done","source":"web"}
-->`

	body, comments := ParseComments(content)
	if !strings.Contains(body, "Body text here") {
		t.Errorf("body = %q", body)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].ID != 1 || comments[0].Text != "first comment" || comments[0].Status != "open" {
		t.Errorf("comment[0] = %+v", comments[0])
	}
	if comments[1].ID != 2 || comments[1].Text != "second comment" || comments[1].Status != "done" {
		t.Errorf("comment[1] = %+v", comments[1])
	}
}

func TestParseComments_DefaultStatus(t *testing.T) {
	content := `Body

<!-- issue-viewer-comments
{"id":1,"block":0,"date":"2025-01-15","text":"no status field","source":"cli"}
-->`

	_, comments := ParseComments(content)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Status != "open" {
		t.Errorf("default status = %q, want %q", comments[0].Status, "open")
	}
}

func TestParseSerializeRoundtrip(t *testing.T) {
	original := []Comment{
		{ID: 1, Block: 0, Date: "2025-01-15", Text: "comment one", Status: "open", Source: "cli"},
		{ID: 2, Block: 1, Date: "2025-01-16", Text: "comment two", Status: "done", Source: "web"},
	}

	serialized := SerializeComments(original)
	_, parsed := ParseComments("body" + serialized)

	if len(parsed) != len(original) {
		t.Fatalf("roundtrip: got %d comments, want %d", len(parsed), len(original))
	}

	for i := range original {
		if parsed[i].ID != original[i].ID || parsed[i].Text != original[i].Text || parsed[i].Status != original[i].Status {
			t.Errorf("roundtrip mismatch at %d: got %+v, want %+v", i, parsed[i], original[i])
		}
	}
}

func TestSerializeComments_Empty(t *testing.T) {
	result := SerializeComments(nil)
	if result != "" {
		t.Errorf("expected empty string for nil comments, got %q", result)
	}

	result = SerializeComments([]Comment{})
	if result != "" {
		t.Errorf("expected empty string for empty comments, got %q", result)
	}
}

func TestAddComment(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.md")
	os.WriteFile(fp, []byte("---\ntitle: \"Test\"\n---\n\nBody text."), 0644)

	err := AddComment(fp, 0, "my new comment", "cli")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	comments, err := LoadComments(fp)
	if err != nil {
		t.Fatalf("unexpected error loading: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Text != "my new comment" {
		t.Errorf("text = %q", comments[0].Text)
	}
	if comments[0].ID != 1 {
		t.Errorf("id = %d, want 1", comments[0].ID)
	}
	if comments[0].Status != "open" {
		t.Errorf("status = %q, want %q", comments[0].Status, "open")
	}
}

func TestAddComment_Multiple(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.md")
	os.WriteFile(fp, []byte("---\ntitle: \"Test\"\n---\n\nBody."), 0644)

	AddComment(fp, 0, "first", "cli")
	AddComment(fp, 0, "second", "web")

	comments, _ := LoadComments(fp)
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].ID != 1 || comments[1].ID != 2 {
		t.Errorf("IDs = [%d, %d], want [1, 2]", comments[0].ID, comments[1].ID)
	}
}

func TestAddComment_ConcurrentPreservesAllComments(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.md")
	os.WriteFile(fp, []byte("---\ntitle: \"Test\"\n---\n\nBody."), 0644)

	const writers = 16
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := AddComment(fp, 0, "comment", "cli"); err != nil {
				t.Errorf("AddComment(%d): %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	comments, err := LoadComments(fp)
	if err != nil {
		t.Fatalf("unexpected error loading comments: %v", err)
	}
	if len(comments) != writers {
		t.Fatalf("expected %d comments, got %d", writers, len(comments))
	}
	for i, c := range comments {
		if c.ID != i+1 {
			t.Fatalf("comment[%d].ID = %d, want %d", i, c.ID, i+1)
		}
	}
}

func TestLoadComments(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.md")

	content := `---
title: "Test"
---

Body

<!-- issue-viewer-comments
{"id":1,"block":0,"date":"2025-01-15","text":"hello","status":"open","source":"cli"}
-->`
	os.WriteFile(fp, []byte(content), 0644)

	comments, err := LoadComments(fp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Text != "hello" {
		t.Errorf("text = %q", comments[0].Text)
	}
}

func TestLoadComments_NoFile(t *testing.T) {
	_, err := LoadComments("/nonexistent/file.md")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestToggleComment(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.md")
	os.WriteFile(fp, []byte("---\ntitle: \"Test\"\n---\n\nBody."), 0644)

	AddComment(fp, 0, "togglable", "cli")

	// Toggle to done
	err := ToggleComment(fp, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	comments, _ := LoadComments(fp)
	if comments[0].Status != "done" {
		t.Errorf("status after first toggle = %q, want %q", comments[0].Status, "done")
	}

	// Toggle back to open
	err = ToggleComment(fp, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	comments, _ = LoadComments(fp)
	if comments[0].Status != "open" {
		t.Errorf("status after second toggle = %q, want %q", comments[0].Status, "open")
	}
}

func TestDeleteComment(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.md")
	os.WriteFile(fp, []byte("---\ntitle: \"Test\"\n---\n\nBody."), 0644)

	AddComment(fp, 0, "first", "cli")
	AddComment(fp, 0, "second", "cli")

	err := DeleteComment(fp, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	comments, _ := LoadComments(fp)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment after delete, got %d", len(comments))
	}
	if comments[0].Text != "second" {
		t.Errorf("remaining comment = %q, want %q", comments[0].Text, "second")
	}
}

func TestNextCommentID(t *testing.T) {
	tests := []struct {
		name     string
		comments []Comment
		want     int
	}{
		{"empty", nil, 1},
		{"one comment", []Comment{{ID: 1}}, 2},
		{"multiple", []Comment{{ID: 1}, {ID: 5}, {ID: 3}}, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextCommentID(tt.comments)
			if got != tt.want {
				t.Errorf("NextCommentID() = %d, want %d", got, tt.want)
			}
		})
	}
}
