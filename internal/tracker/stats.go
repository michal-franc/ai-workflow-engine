package tracker

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tokens"
)

// TransitionStat is one recorded workflow transition with its estimated token
// cost. ActualTokens is reserved for a future hybrid pass that records the
// measured agent-run token count; nil means "not yet captured".
type TransitionStat struct {
	From          string    `json:"from"`
	To            string    `json:"to"`
	TS            time.Time `json:"ts"`
	StaticTokens  int       `json:"static_tokens"`
	DynamicTokens int       `json:"dynamic_tokens"`
	ActualTokens  *int      `json:"actual_tokens"`
}

// StatsStore is the per-issue workflow-stats sidecar persisted as
// <issue>.stats.json next to the markdown file.
type StatsStore struct {
	Transitions []TransitionStat `json:"transitions"`
}

// StatsSidecarPath returns the stats sidecar path for an issue markdown file.
// Mirrors the convention used by SidecarPath for the data-table sidecar:
// foo/42.md → foo/42.stats.json.
func StatsSidecarPath(issuePath string) string {
	if strings.HasSuffix(issuePath, ".md") {
		return strings.TrimSuffix(issuePath, ".md") + ".stats.json"
	}
	return issuePath + ".stats.json"
}

// LoadStats reads the stats sidecar for an issue. A missing file yields an
// empty store with no error — callers should treat "no records" and "no
// sidecar" the same.
func LoadStats(issuePath string) (StatsStore, error) {
	path := StatsSidecarPath(issuePath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StatsStore{}, nil
		}
		return StatsStore{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var store StatsStore
	if err := json.Unmarshal(data, &store); err != nil {
		return StatsStore{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return store, nil
}

// SaveStats writes the stats sidecar atomically (temp file + fsync + rename).
// Last-write-wins; the atomic rename guarantees no half-written file on disk.
func SaveStats(issuePath string, store StatsStore) error {
	path := StatsSidecarPath(issuePath)
	encoded, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding stats store: %w", err)
	}
	encoded = append(encoded, '\n')
	return writeFileAtomically(path, encoded, 0644)
}

// AppendTransitionStat loads, appends, and saves the stats sidecar in one shot.
// Errors from the load path propagate; missing-file is not an error.
func AppendTransitionStat(issuePath string, stat TransitionStat) error {
	store, err := LoadStats(issuePath)
	if err != nil {
		return err
	}
	store.Transitions = append(store.Transitions, stat)
	return SaveStats(issuePath, store)
}

// StaticTransitionCost returns the approximate token count of the workflow
// scaffolding the bot reads when transitioning from→to: every action body the
// transition runs (validate rules, append_section bodies, inject_prompt
// prompts), the legacy template appended on entry to the target status, and
// the target status's entry-guidance prompt.
//
// Pure function — no I/O, no issue context. Used both by the Stats tab's
// reference table and as the floor for the dynamic cost recorded at
// transition time.
func StaticTransitionCost(wf *WorkflowConfig, from, to string) int {
	if wf == nil {
		return 0
	}

	total := 0
	for _, action := range wf.transitionActions(from, to) {
		switch action.Type {
		case "validate":
			total += tokens.Estimate(action.Rule)
		case "append_section":
			total += tokens.Estimate(action.Title) + tokens.Estimate(action.Body)
		case "inject_prompt":
			total += tokens.Estimate(action.Prompt)
		case "require_human_approval":
			total += tokens.Estimate(action.Status)
		}
	}

	total += tokens.Estimate(wf.TemplateForStatus(to))
	total += tokens.Estimate(wf.StatusPrompt(to))

	return total
}

// DynamicTransitionCost returns the static cost plus the issue context the
// bot reads at transition time: the issue body and the joined comment bodies
// captured at the moment of transition.
func DynamicTransitionCost(wf *WorkflowConfig, from, to, issueBody string, comments []Comment) int {
	cost := StaticTransitionCost(wf, from, to)
	cost += tokens.Estimate(issueBody)
	for _, c := range comments {
		cost += tokens.Estimate(c.Text)
	}
	return cost
}
