package tracker

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// DataEntry is a single row in an issue's structured data store.
type DataEntry struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Comment     string `json:"comment"`
}

// DataStore is the shape persisted to <issue>.data.json next to the markdown.
type DataStore struct {
	NextID  int         `json:"next_id"`
	Entries []DataEntry `json:"entries"`
}

// DefaultDataStatuses is used when the marker omits an explicit status list.
var DefaultDataStatuses = []string{"open", "resolved"}

// dataMarkerRe captures the data marker. The first submatch (when present)
// holds the raw value of statuses=, e.g. "open,resolved,wontfix".
var dataMarkerRe = regexp.MustCompile(`<!--\s*data(?:\s+statuses=([^>]+?))?\s*-->`)

// SidecarPath returns the data sidecar path for an issue markdown file.
// foo/bar/42.md → foo/bar/42.data.json. A path that does not end in .md
// gets .data.json appended verbatim.
func SidecarPath(issuePath string) string {
	if strings.HasSuffix(issuePath, ".md") {
		return strings.TrimSuffix(issuePath, ".md") + ".data.json"
	}
	return issuePath + ".data.json"
}

// LoadData reads the sidecar for an issue. A missing file yields an empty
// store with no error — callers should treat "no entries" and "no sidecar"
// the same.
func LoadData(issuePath string) (DataStore, error) {
	path := SidecarPath(issuePath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DataStore{}, nil
		}
		return DataStore{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var store DataStore
	if err := json.Unmarshal(data, &store); err != nil {
		return DataStore{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return store, nil
}

// SaveData writes the sidecar atomically (temp file + fsync + rename).
// Last-write-wins: callers do not lock, so a racing writer can overwrite
// concurrent changes. The atomic rename guarantees the file on disk is
// never half-written.
func SaveData(issuePath string, store DataStore) error {
	path := SidecarPath(issuePath)
	encoded, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding data store: %w", err)
	}
	encoded = append(encoded, '\n')
	return writeFileAtomically(path, encoded, 0644)
}

// AddEntry appends a new entry and returns its assigned id. Ids start at 1
// and are monotonic — RemoveEntry does not reuse ids.
func AddEntry(issuePath, description, status string) (int, error) {
	store, err := LoadData(issuePath)
	if err != nil {
		return 0, err
	}
	if store.NextID < 1 {
		store.NextID = 1
		for _, e := range store.Entries {
			if e.ID >= store.NextID {
				store.NextID = e.ID + 1
			}
		}
	}
	id := store.NextID
	store.NextID++
	store.Entries = append(store.Entries, DataEntry{
		ID:          id,
		Description: description,
		Status:      status,
		Comment:     "",
	})
	if err := SaveData(issuePath, store); err != nil {
		return 0, err
	}
	return id, nil
}

// SetEntryStatus updates one entry's status. Returns an error if the id is
// not found.
func SetEntryStatus(issuePath string, id int, status string) error {
	return mutateEntry(issuePath, id, func(e *DataEntry) {
		e.Status = status
	})
}

// SetEntryComment overwrites one entry's comment.
func SetEntryComment(issuePath string, id int, comment string) error {
	return mutateEntry(issuePath, id, func(e *DataEntry) {
		e.Comment = comment
	})
}

// RemoveEntry deletes the entry with the given id. NextID is unchanged so
// removed ids are never reused.
func RemoveEntry(issuePath string, id int) error {
	store, err := LoadData(issuePath)
	if err != nil {
		return err
	}
	out := store.Entries[:0]
	found := false
	for _, e := range store.Entries {
		if e.ID == id {
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		return fmt.Errorf("entry %d not found", id)
	}
	store.Entries = out
	return SaveData(issuePath, store)
}

func mutateEntry(issuePath string, id int, fn func(*DataEntry)) error {
	store, err := LoadData(issuePath)
	if err != nil {
		return err
	}
	for i := range store.Entries {
		if store.Entries[i].ID == id {
			fn(&store.Entries[i])
			return SaveData(issuePath, store)
		}
	}
	return fmt.Errorf("entry %d not found", id)
}

// DataMarker describes the first <!-- data ... --> marker found in a body.
type DataMarker struct {
	Found    bool
	Raw      string   // exact substring to substitute (e.g. `<!-- data statuses=a,b -->`)
	Start    int      // byte offset of Raw in the source string
	Statuses []string // declared statuses, trimmed; empty if not declared
}

// ParseDataMarker returns the first data marker in s. Subsequent markers are
// left in place so they render literally.
func ParseDataMarker(s string) DataMarker {
	loc := dataMarkerRe.FindStringSubmatchIndex(s)
	if loc == nil {
		return DataMarker{}
	}
	m := DataMarker{
		Found: true,
		Raw:   s[loc[0]:loc[1]],
		Start: loc[0],
	}
	if loc[2] != -1 {
		raw := s[loc[2]:loc[3]]
		for _, tok := range strings.Split(raw, ",") {
			tok = strings.TrimSpace(tok)
			if tok != "" {
				m.Statuses = append(m.Statuses, tok)
			}
		}
	}
	return m
}

// ResolveDataStatuses returns the union of declared statuses and any extra
// statuses currently in use on entries (in declaration order, with extras
// appended). Falls back to DefaultDataStatuses when none are declared.
func ResolveDataStatuses(declared []string, entries []DataEntry) []string {
	statuses := append([]string(nil), declared...)
	if len(statuses) == 0 {
		statuses = append(statuses, DefaultDataStatuses...)
	}
	seen := map[string]bool{}
	for _, s := range statuses {
		seen[s] = true
	}
	for _, e := range entries {
		if e.Status == "" || seen[e.Status] {
			continue
		}
		statuses = append(statuses, e.Status)
		seen[e.Status] = true
	}
	return statuses
}
