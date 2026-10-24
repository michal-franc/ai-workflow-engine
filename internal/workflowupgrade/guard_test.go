package workflowupgrade

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// nonFeatureKeys are workflow.yaml keys with no catalog entry, each with the
// reason. Everything else a project can set must be detected by an entry's
// probe or taught by an entry's snippet, so `workflow upgrade` can offer it.
var nonFeatureKeys = map[string]string{
	"template":                "the template stamp itself",
	"features":                "the feature record itself",
	"worktree_sparse_exclude": "tunes worktree (default issues/); not something to adopt on its own",
	"leads.epic":              "obsolete and ignored; lint warns when set",
	"ci.branch":               "local CI's integration branch; the default (main/master) fits every project so far",
	"ci.nice":                 "local CI's process priority; a tuning knob, not a feature",
	"health.off":              "turns snapshots off; the opposite of adopting a feature",
	"health.timeout":          "per-family time limit; a tuning knob",
	"health.exclude":          "codebase-count exclusions; a tuning knob",
	"health.big_file_lines":   "a tuning knob for big files",
	"health.long_func_lines":  "a tuning knob for long functions",
	"health.score":            "per-component score overrides; a tuning knob",
}

// blocks are the project-wide config blocks whose sub-keys the guard walks.
// Lifecycle structs (statuses, transitions, overlays) are covered by the
// lifecycle and gate entries and are not walked key by key.
var blocks = []string{"ci", "ci.run[]", "health", "leads", "workbench", "workbench.systems.*"}

// TestEveryWorkflowFeatureHasACatalogEntry fails when workflow.yaml gains a
// key, a validator rule or an action type that no catalog entry detects or
// teaches: such a feature would be invisible to `issue-cli workflow upgrade`.
func TestEveryWorkflowFeatureHasACatalogEntry(t *testing.T) {
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	const fix = "add an entry (or a probe or snippet line) to cmd/issue-cli/templates/workflow-features.yaml (docs/Workflow/overview.md, Feature catalog), or add it to nonFeatureKeys with a reason"

	keys := yamlKeys(reflect.TypeOf(tracker.WorkflowConfig{}), "")
	for _, b := range blocks {
		keys = append(keys, yamlKeys(schemaType(t, b), b)...)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
		_, allowed := nonFeatureKeys[k]
		switch covered := keyCovered(entries, k); {
		case covered && allowed:
			t.Errorf("%s is in nonFeatureKeys but an entry covers it now; drop the allowlist line", k)
		case !covered && !allowed:
			t.Errorf("workflow.yaml key %s has no catalog entry: %s", k, fix)
		}
	}
	for k := range nonFeatureKeys {
		if !seen[k] {
			t.Errorf("nonFeatureKeys has %s, which is no longer a workflow.yaml key; drop the line", k)
		}
	}

	for _, r := range tracker.WorkflowValidationRules {
		name := strings.TrimSpace(strings.SplitN(r.Name, ":", 2)[0])
		if !ruleCovered(entries, name) {
			t.Errorf("validator rule %s has no catalog entry with rule_in: [%s]: %s", name, name, fix)
		}
	}
	for _, a := range tracker.WorkflowActionTypes {
		if !actionCovered(entries, a.Name) {
			t.Errorf("action type %s has no catalog entry probing transitions[].actions[].type: %s", a.Name, fix)
		}
	}
}

// yamlKeys lists the yaml keys of a struct type, prefixed with at.
func yamlKeys(typ reflect.Type, at string) []string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	var keys []string
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		if at != "" {
			name = at + "." + name
		}
		keys = append(keys, name)
	}
	return keys
}

// schemaType resolves a block path (as in blocks) to its struct type.
func schemaType(t *testing.T, path string) reflect.Type {
	t.Helper()
	typ := reflect.TypeOf(tracker.WorkflowConfig{})
	for _, seg := range strings.Split(path, ".") {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if seg == "*" {
			typ = typ.Elem()
			continue
		}
		key, isList := strings.CutSuffix(seg, "[]")
		var next reflect.Type
		for i := 0; i < typ.NumField(); i++ {
			if name, _, _ := strings.Cut(typ.Field(i).Tag.Get("yaml"), ","); name == key {
				next = typ.Field(i).Type
			}
		}
		if next == nil {
			t.Fatalf("block %s: no key %s", path, seg)
		}
		for next.Kind() == reflect.Pointer {
			next = next.Elem()
		}
		if isList {
			next = next.Elem()
		}
		typ = next
	}
	return typ
}

// keyCovered: a probe path starts at the key, or an entry's snippet sets it.
func keyCovered(entries []Entry, key string) bool {
	for _, e := range entries {
		for _, p := range e.Detect {
			if p.Path == key || strings.HasPrefix(p.Path, key+".") || strings.HasPrefix(p.Path, key+"[]") {
				return true
			}
		}
		found := false
		walkProbePath(ParseDoc([]byte(e.Snippet)), key, func(string, any) { found = true })
		if found {
			return true
		}
	}
	return false
}

func ruleCovered(entries []Entry, rule string) bool {
	for _, e := range entries {
		for _, p := range e.Detect {
			if containsString(p.RuleIn, rule) {
				return true
			}
		}
	}
	return false
}

func actionCovered(entries []Entry, action string) bool {
	for _, e := range entries {
		for _, p := range e.Detect {
			if p.Path == "transitions[].actions[].type" && (containsString(p.OneOf, action) || p.Equals == action) {
				return true
			}
			// Only a validate action has a rule, so a rule probe covers it.
			if action == "validate" && p.Path == "transitions[].actions[].rule" {
				return true
			}
		}
	}
	return false
}

func TestGuardCatchesANewKey(t *testing.T) {
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ci.run", "health.limits", "workbench.pages", "leads.project", "ci.run[].results"} {
		if !keyCovered(entries, k) {
			t.Errorf("%s should be covered", k)
		}
	}
	if keyCovered(entries, "workbench.not_a_feature_yet") || ruleCovered(entries, "not_a_rule_yet") || actionCovered(entries, "not_an_action_yet") {
		t.Error("an unknown key, rule or action counts as covered")
	}
}

// Every entry names the code behind it, and every source exists, so
// catalog-check can ask for a version bump when that code changes.
func TestEveryEntryHasSourcesThatExist(t *testing.T) {
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	root := repoRoot(t)
	files := trackedFiles(t, root)
	for _, e := range entries {
		if len(e.Sources) == 0 {
			t.Errorf("%s: no sources: (the code behind the feature, or %q for a prompt-only convention)", e.ID, PromptSource)
		}
		for _, s := range e.Sources {
			if s == PromptSource {
				if len(e.Sources) > 1 {
					t.Errorf("%s: %s is the only source of a prompt-only convention", e.ID, PromptSource)
				}
				continue
			}
			glob, sym, hasSym := strings.Cut(s, "#")
			var matched []string
			for _, f := range files {
				if MatchSource(glob, f) {
					matched = append(matched, f)
				}
			}
			if len(matched) == 0 {
				t.Errorf("%s: source %s matches no tracked file", e.ID, s)
				continue
			}
			if hasSym {
				if len(matched) != 1 || filepath.Ext(matched[0]) != ".go" {
					t.Errorf("%s: %s: a #symbol source names one Go file, matched %v", e.ID, s, matched)
					continue
				}
				src, err := os.ReadFile(filepath.Join(root, matched[0]))
				if err != nil {
					t.Fatal(err)
				}
				if len(SymbolDecls(src, sym)) == 0 {
					t.Errorf("%s: %s has no top-level %s", e.ID, matched[0], sym)
				}
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, CatalogPath)); err != nil {
		t.Fatalf("repo root %s: %v", root, err)
	}
	return root
}

func trackedFiles(t *testing.T, root string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		t.Skipf("git ls-files: %v", err)
	}
	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	sort.Strings(files)
	return files
}

func TestSetupStepsSayWhoDoesThem(t *testing.T) {
	entries, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		for i, st := range e.Setup {
			if st.Who != "owner" && st.Who != "agent" {
				t.Errorf("%s: setup[%d].who %q is not one of %v", e.ID, i, st.Who, SetupWho)
			}
			if strings.TrimSpace(st.Do) == "" {
				t.Errorf("%s: setup[%d].do is empty", e.ID, i)
			}
		}
	}
}
