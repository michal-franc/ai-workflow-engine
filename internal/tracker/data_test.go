package tracker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeIssueFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestSidecarPath(t *testing.T) {
	cases := map[string]string{
		"foo/bar/42.md": "foo/bar/42.data.json",
		"42.md":         "42.data.json",
		"42":            "42.data.json",
		"a/b.md.txt":    "a/b.md.txt.data.json",
	}
	for in, want := range cases {
		got := SidecarPath(in)
		if got != want {
			t.Errorf("SidecarPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadData_MissingFile(t *testing.T) {
	dir := t.TempDir()
	issuePath := filepath.Join(dir, "missing.md")
	store, err := LoadData(issuePath)
	if err != nil {
		t.Fatalf("LoadData missing: %v", err)
	}
	if len(store.Entries) != 0 || store.NextID != 0 {
		t.Errorf("expected empty store, got %+v", store)
	}
}

func TestAddEntry_MonotonicIDs(t *testing.T) {
	dir := t.TempDir()
	issuePath := writeIssueFile(t, dir, "x.md", "---\ntitle: x\n---\n")

	id1, err := AddEntry(issuePath, "first", "open")
	if err != nil {
		t.Fatalf("add 1: %v", err)
	}
	id2, err := AddEntry(issuePath, "second", "open")
	if err != nil {
		t.Fatalf("add 2: %v", err)
	}
	if id1 != 1 || id2 != 2 {
		t.Fatalf("expected ids 1,2 got %d,%d", id1, id2)
	}

	if err := RemoveEntry(issuePath, id1); err != nil {
		t.Fatalf("remove 1: %v", err)
	}
	id3, err := AddEntry(issuePath, "third", "open")
	if err != nil {
		t.Fatalf("add 3: %v", err)
	}
	if id3 != 3 {
		t.Errorf("after remove of 1, next id should be 3 (monotonic), got %d", id3)
	}

	store, _ := LoadData(issuePath)
	if len(store.Entries) != 2 {
		t.Errorf("expected 2 entries after remove, got %d", len(store.Entries))
	}
	for _, e := range store.Entries {
		if e.ID == 1 {
			t.Errorf("removed id 1 reappeared")
		}
	}
}

func TestSetEntryStatusAndComment(t *testing.T) {
	dir := t.TempDir()
	issuePath := writeIssueFile(t, dir, "x.md", "---\ntitle: x\n---\n")

	id, _ := AddEntry(issuePath, "thing", "open")
	if err := SetEntryStatus(issuePath, id, "🔥 must-fix"); err != nil {
		t.Fatalf("set-status: %v", err)
	}
	if err := SetEntryComment(issuePath, id, "looked at it"); err != nil {
		t.Fatalf("set-comment: %v", err)
	}

	store, _ := LoadData(issuePath)
	if len(store.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(store.Entries))
	}
	got := store.Entries[0]
	if got.Status != "🔥 must-fix" {
		t.Errorf("status = %q", got.Status)
	}
	if got.Comment != "looked at it" {
		t.Errorf("comment = %q", got.Comment)
	}
}

func TestSetEntryStatus_NotFound(t *testing.T) {
	dir := t.TempDir()
	issuePath := writeIssueFile(t, dir, "x.md", "---\ntitle: x\n---\n")
	if err := SetEntryStatus(issuePath, 99, "open"); err == nil {
		t.Errorf("expected error for missing entry, got nil")
	}
}

func TestRemoveEntry_NotFound(t *testing.T) {
	dir := t.TempDir()
	issuePath := writeIssueFile(t, dir, "x.md", "---\ntitle: x\n---\n")
	if err := RemoveEntry(issuePath, 99); err == nil {
		t.Errorf("expected error for missing entry, got nil")
	}
}

func TestSaveData_AtomicWriteWritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	issuePath := writeIssueFile(t, dir, "x.md", "---\ntitle: x\n---\n")
	store := DataStore{
		NextID: 3,
		Entries: []DataEntry{
			{ID: 1, Description: "a", Status: "open", Comment: ""},
			{ID: 2, Description: "b", Status: "✅ done", Comment: "ok"},
		},
	}
	if err := SaveData(issuePath, store); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(SidecarPath(issuePath))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	var got DataStore
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("sidecar is not valid JSON: %v", err)
	}
	if !reflect.DeepEqual(got, store) {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, store)
	}
}

func TestParseDataMarker(t *testing.T) {
	type want struct {
		found    bool
		statuses []string
		tiers    []string
	}
	cases := []struct {
		name string
		body string
		want want
	}{
		{"no marker", "just text", want{false, nil, nil}},
		{"plain marker", "before\n<!-- data -->\nafter", want{true, nil, nil}},
		{"declared statuses", "<!-- data statuses=open,resolved,wontfix -->", want{true, []string{"open", "resolved", "wontfix"}, nil}},
		{"spaces and emojis", "<!-- data statuses=✅ done, 🔥 must-fix , open -->", want{true, []string{"✅ done", "🔥 must-fix", "open"}, nil}},
		{"first marker wins", "<!-- data statuses=a,b --> <!-- data statuses=c -->", want{true, []string{"a", "b"}, nil}},
		{"declared tiers only", "<!-- data tiers=🔴 critical,🟢 nice -->", want{true, nil, []string{"🔴 critical", "🟢 nice"}}},
		{"both axes", "<!-- data statuses=open,resolved tiers=S1,S2,S3 -->", want{true, []string{"open", "resolved"}, []string{"S1", "S2", "S3"}}},
		{"tiers before statuses", "<!-- data tiers=high,low statuses=open,resolved -->", want{true, []string{"open", "resolved"}, []string{"high", "low"}}},
		{"unknown attr ignored", "<!-- data foo=bar tiers=a,b -->", want{true, nil, []string{"a", "b"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseDataMarker(tc.body)
			if got.Found != tc.want.found {
				t.Fatalf("Found = %v, want %v", got.Found, tc.want.found)
			}
			if !reflect.DeepEqual(got.Statuses, tc.want.statuses) {
				t.Errorf("Statuses = %v, want %v", got.Statuses, tc.want.statuses)
			}
			if !reflect.DeepEqual(got.Tiers, tc.want.tiers) {
				t.Errorf("Tiers = %v, want %v", got.Tiers, tc.want.tiers)
			}
		})
	}
}

func TestResolveDataStatuses(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		got := ResolveDataStatuses(nil, nil)
		want := []string{"open", "resolved"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v want %v", got, want)
		}
	})
	t.Run("declared used as-is", func(t *testing.T) {
		got := ResolveDataStatuses([]string{"a", "b"}, nil)
		if !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Errorf("got %v", got)
		}
	})
	t.Run("entry status not in declared list is appended", func(t *testing.T) {
		got := ResolveDataStatuses([]string{"open"}, []DataEntry{{Status: "wontfix"}, {Status: "open"}})
		if !reflect.DeepEqual(got, []string{"open", "wontfix"}) {
			t.Errorf("got %v", got)
		}
	})
}

func TestResolveDataTiers(t *testing.T) {
	t.Run("no declared returns nil (tier UI hidden)", func(t *testing.T) {
		if got := ResolveDataTiers(nil, []DataEntry{{Tier: "ignored"}}); got != nil {
			t.Errorf("got %v want nil", got)
		}
	})
	t.Run("declared used as-is", func(t *testing.T) {
		got := ResolveDataTiers([]string{"S1", "S2"}, nil)
		if !reflect.DeepEqual(got, []string{"S1", "S2"}) {
			t.Errorf("got %v", got)
		}
	})
	t.Run("undeclared in-use tier appended", func(t *testing.T) {
		got := ResolveDataTiers([]string{"S1"}, []DataEntry{{Tier: "S3"}, {Tier: "S1"}})
		if !reflect.DeepEqual(got, []string{"S1", "S3"}) {
			t.Errorf("got %v", got)
		}
	})
}

func TestAddEntryWithTierAndSetEntryTier(t *testing.T) {
	dir := t.TempDir()
	issuePath := writeIssueFile(t, dir, "x.md", "---\ntitle: x\n---\n")

	id, err := AddEntryWithTier(issuePath, "thing", "open", "🔴 critical")
	if err != nil {
		t.Fatalf("AddEntryWithTier: %v", err)
	}
	store, _ := LoadData(issuePath)
	if got := store.Entries[0].Tier; got != "🔴 critical" {
		t.Errorf("tier after add = %q, want \"🔴 critical\"", got)
	}

	if err := SetEntryTier(issuePath, id, "🟢 nice"); err != nil {
		t.Fatalf("SetEntryTier: %v", err)
	}
	store, _ = LoadData(issuePath)
	if got := store.Entries[0].Tier; got != "🟢 nice" {
		t.Errorf("tier after set = %q, want \"🟢 nice\"", got)
	}

	if err := SetEntryTier(issuePath, id, ""); err != nil {
		t.Fatalf("clear tier: %v", err)
	}
	store, _ = LoadData(issuePath)
	if got := store.Entries[0].Tier; got != "" {
		t.Errorf("tier after clear = %q, want empty", got)
	}
}

func TestAddEntry_NoTierOmittedFromJSON(t *testing.T) {
	dir := t.TempDir()
	issuePath := writeIssueFile(t, dir, "x.md", "---\ntitle: x\n---\n")
	if _, err := AddEntry(issuePath, "no tier", "open"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(SidecarPath(issuePath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\"tier\"") {
		t.Errorf("expected tier field omitted when empty, got: %s", raw)
	}
}
