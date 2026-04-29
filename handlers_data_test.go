package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

func TestHandleDataAdd_CreatesSidecarAndReturnsID(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	body := `{"description":"first finding","status":"open"}`
	resp, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login/data", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var got struct{ ID int }
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 1 {
		t.Errorf("expected id 1, got %d", got.ID)
	}

	store, err := tracker.LoadData(filepath.Join(proj.IssueDir, "bug-in-login.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Entries) != 1 || store.Entries[0].Description != "first finding" || store.Entries[0].Status != "open" {
		t.Errorf("sidecar mismatch: %+v", store)
	}
}

func TestHandleDataAdd_RequiresDescription(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/p/test-project/issue/bug-in-login/data", "application/json", strings.NewReader(`{"description":"","status":"open"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleDataAdd_404ForUnknownIssue(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/p/test-project/issue/nope/data", "application/json", strings.NewReader(`{"description":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleDataSetStatus_PersistsChange(t *testing.T) {
	proj, _ := setupTestProject(t)
	issuePath := filepath.Join(proj.IssueDir, "bug-in-login.md")
	id, err := tracker.AddEntry(issuePath, "x", "open")
	if err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	url := ts.URL + "/p/test-project/issue/bug-in-login/data/" + strconv.Itoa(id) + "/status"
	resp, err := http.Post(url, "application/json", strings.NewReader(`{"status":"resolved"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	store, _ := tracker.LoadData(issuePath)
	if store.Entries[0].Status != "resolved" {
		t.Errorf("status not persisted: %+v", store)
	}
}

func TestHandleDataSetComment_PersistsChange(t *testing.T) {
	proj, _ := setupTestProject(t)
	issuePath := filepath.Join(proj.IssueDir, "bug-in-login.md")
	id, _ := tracker.AddEntry(issuePath, "x", "open")

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	url := ts.URL + "/p/test-project/issue/bug-in-login/data/" + strconv.Itoa(id) + "/comment"
	resp, err := http.Post(url, "application/json", strings.NewReader(`{"comment":"looked into it"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	store, _ := tracker.LoadData(issuePath)
	if store.Entries[0].Comment != "looked into it" {
		t.Errorf("comment not persisted: %+v", store)
	}
}

func TestHandleDataRemove_DeletesEntry(t *testing.T) {
	proj, _ := setupTestProject(t)
	issuePath := filepath.Join(proj.IssueDir, "bug-in-login.md")
	id, _ := tracker.AddEntry(issuePath, "x", "open")

	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	url := ts.URL + "/p/test-project/issue/bug-in-login/data/" + strconv.Itoa(id)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	store, _ := tracker.LoadData(issuePath)
	if len(store.Entries) != 0 {
		t.Errorf("expected empty entries after remove, got %+v", store)
	}
}

func TestHandleDataSetStatus_404ForMissingEntry(t *testing.T) {
	proj, _ := setupTestProject(t)
	ts := newTestServer(t, []tracker.Project{proj})
	defer ts.Close()

	url := ts.URL + "/p/test-project/issue/bug-in-login/data/99/status"
	resp, err := http.Post(url, "application/json", strings.NewReader(`{"status":"resolved"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}
