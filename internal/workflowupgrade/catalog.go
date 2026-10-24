package workflowupgrade

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

type Entry struct {
	ID        string   `yaml:"id"`
	Version   string   `yaml:"version"`
	Category  string   `yaml:"category"`
	Propose   bool     `yaml:"propose"`
	Templates []string `yaml:"templates"`
	Title     string   `yaml:"title"`
	What      string   `yaml:"what"`
	Why       string   `yaml:"why"`
	Where     string   `yaml:"where"`
	Snippet   string   `yaml:"snippet"`
	Question  string   `yaml:"question"`
	Detect    []Probe  `yaml:"detect"`
	// Requires lists entry ids the project must have before upgrade
	// proposes this one (ci-test-results needs local-ci).
	Requires []string `yaml:"requires"`
	// Sources are the repo globs of the code behind the feature. `workflow
	// catalog-check` asks for a version bump when a branch changes them.
	Sources []string `yaml:"sources"`
	// Setup is the ordered steps beyond the snippet, for a feature that sets
	// things up outside workflow.yaml (repos, hooks). The Updates card lists
	// them and the install agent walks them; an owner step is the owner's to
	// do, never the agent's.
	Setup []SetupStep `yaml:"setup"`
}

// SetupStep is one setup step: who does it (owner or agent) and what.
type SetupStep struct {
	Who string `yaml:"who" json:"who"`
	Do  string `yaml:"do" json:"do"`
}

// SetupWho are the values a setup step's who may take.
var SetupWho = []string{"owner", "agent"}

// Probe matches values at a path in the parsed workflow.yaml. With no
// condition it matches any set value; otherwise the single condition given.
type Probe struct {
	Path     string   `yaml:"path"`
	Equals   any      `yaml:"equals"`
	OneOf    []string `yaml:"one_of"`
	RuleIn   []string `yaml:"rule_in"`
	Contains string   `yaml:"contains"`
}

// Categories is the catalog's category list, in the order the Updates tab
// and scan output group entries.
var Categories = []string{"lifecycle", "gates", "automation", "actions", "board", "workbench", "dispatch", "conventions"}

// appliesTo reports whether upgrade proposes the entry for a template. An
// entry without templates applies to all of them.
func (e Entry) appliesTo(template string) bool {
	if len(e.Templates) == 0 {
		return true
	}
	for _, t := range e.Templates {
		if t == template {
			return true
		}
	}
	return false
}

func Catalog() ([]Entry, error) {
	var doc struct {
		Entries []Entry `yaml:"entries"`
	}
	if err := yaml.Unmarshal(catalogYAML(), &doc); err != nil {
		return nil, fmt.Errorf("parsing bundled feature catalog: %w", err)
	}
	return doc.Entries, nil
}

func catalogIndex(entries []Entry) map[string]Entry {
	idx := make(map[string]Entry, len(entries))
	for _, e := range entries {
		idx[e.ID] = e
	}
	return idx
}

// ParseDoc decodes workflow.yaml generically for probing. A file that
// doesn't parse as a mapping yields nil, so probes find nothing rather than
// breaking the command (the hint must never fail).
func ParseDoc(raw []byte) map[string]any {
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// expandProbePath returns the concrete roots a probe path is searched in: a
// path into the base lifecycle also covers every type and system overlay.
func expandProbePath(path string) []string {
	if strings.HasPrefix(path, "statuses[]") || strings.HasPrefix(path, "transitions[]") {
		return []string{path, "types.*." + path, "systems.*." + path}
	}
	return []string{path}
}

// walkProbePath visits every value at path (see the catalog header for the
// syntax), passing the concrete location, e.g. transitions[2].actions[0].rule.
func walkProbePath(node any, path string, visit func(at string, v any)) {
	walkSegments(node, strings.Split(path, "."), "", visit)
}

func walkSegments(node any, segs []string, at string, visit func(string, any)) {
	if len(segs) == 0 {
		visit(at, node)
		return
	}
	seg, rest := segs[0], segs[1:]
	join := func(k string) string {
		if at == "" {
			return k
		}
		return at + "." + k
	}
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if seg == "*" {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkSegments(m[k], rest, join(k), visit)
		}
		return
	}
	key, isList := strings.CutSuffix(seg, "[]")
	child, ok := m[key]
	if !ok {
		return
	}
	if !isList {
		walkSegments(child, rest, join(key), visit)
		return
	}
	items, _ := child.([]any)
	for i, item := range items {
		walkSegments(item, rest, fmt.Sprintf("%s[%d]", join(key), i), visit)
	}
}

// matches reports whether one value satisfies the probe's condition.
func (p Probe) matches(v any) bool {
	s, isString := v.(string)
	switch {
	case p.Equals != nil:
		return v != nil && fmt.Sprint(v) == fmt.Sprint(p.Equals)
	case len(p.OneOf) > 0:
		return isString && containsString(p.OneOf, strings.TrimSpace(s))
	case len(p.RuleIn) > 0:
		name, _, _ := strings.Cut(s, ":")
		return isString && containsString(p.RuleIn, strings.TrimSpace(name))
	case p.Contains != "":
		return isString && strings.Contains(strings.ToLower(s), strings.ToLower(p.Contains))
	default:
		return isSetValue(v)
	}
}

// isSetValue is the no-condition test: anything but null, false, 0, "" and
// empty lists or maps.
func isSetValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return strings.TrimSpace(x) != ""
	case int:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// describe renders the condition for evidence lines.
func (p Probe) describe(v any) string {
	switch {
	case p.Contains != "":
		return fmt.Sprintf("contains %q", p.Contains)
	case p.Equals != nil, len(p.OneOf) > 0, len(p.RuleIn) > 0:
		return "= " + shortValue(v)
	}
	switch x := v.(type) {
	case []any:
		return fmt.Sprintf("(%d items)", len(x))
	case map[string]any:
		return fmt.Sprintf("(%d entries)", len(x))
	}
	return "= " + shortValue(v)
}

func shortValue(v any) string {
	s := fmt.Sprint(v)
	if str, ok := v.(string); ok {
		s = strings.TrimSpace(str)
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[:i] + " …"
		}
		if len(s) > 50 {
			s = s[:47] + "..."
		}
		return fmt.Sprintf("%q", s)
	}
	return s
}

// Detection is the result of probing one catalog entry.
type Detection struct {
	Matches  int
	Evidence string // first match: concrete path and condition
}

// Detect runs an entry's probes over the parsed workflow.yaml.
func Detect(e Entry, doc map[string]any) Detection {
	var d Detection
	if doc == nil {
		return d
	}
	for _, p := range e.Detect {
		for _, root := range expandProbePath(p.Path) {
			walkProbePath(doc, root, func(at string, v any) {
				if !p.matches(v) {
					return
				}
				if d.Matches == 0 {
					d.Evidence = at + " " + p.describe(v)
				}
				d.Matches++
			})
		}
	}
	return d
}

// featureRef splits a record entry "id@version" ("id" alone for declines).
func featureRef(s string) (id, version string) {
	id, version, _ = strings.Cut(strings.TrimSpace(s), "@")
	return strings.TrimSpace(id), strings.TrimSpace(version)
}

// ReadRecord returns the file's features: record and whether the key
// is present at all.
func ReadRecord(raw []byte) (tracker.FeatureRecord, bool) {
	var doc struct {
		Features *tracker.FeatureRecord `yaml:"features"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil || doc.Features == nil {
		return tracker.FeatureRecord{}, false
	}
	return *doc.Features, true
}

// RecordIndex maps installed ids to versions and lists declined ids.
func RecordIndex(rec tracker.FeatureRecord) (installed map[string]string, declined map[string]bool) {
	installed = map[string]string{}
	declined = map[string]bool{}
	for _, s := range rec.Installed {
		id, v := featureRef(s)
		if id != "" {
			installed[id] = v
		}
	}
	for _, s := range rec.Declined {
		if id, _ := featureRef(s); id != "" {
			declined[id] = true
		}
	}
	return installed, declined
}

const featureRecordComment = "# Feature record: catalog features this file has (<id>@<version>) and the\n# ones the human declined. Written by `issue-cli workflow scan`, read by\n# `issue-cli workflow upgrade`."

// RenderRecord is the features: block scan writes.
func RenderRecord(rec tracker.FeatureRecord) string {
	var b strings.Builder
	b.WriteString("features:\n")
	if len(rec.Installed) == 0 && len(rec.Declined) == 0 {
		b.WriteString("  installed: []\n")
		return b.String()
	}
	for _, part := range []struct {
		key  string
		list []string
	}{{"installed", rec.Installed}, {"declined", rec.Declined}} {
		if len(part.list) == 0 {
			continue
		}
		b.WriteString("  " + part.key + ":\n")
		for _, s := range part.list {
			b.WriteString("    - " + s + "\n")
		}
	}
	return b.String()
}

// WriteRecord replaces the top-level features: block in raw, or
// inserts one after the template: line (else before the first top-level
// key). It edits text so comments and layout elsewhere survive, then checks
// that nothing but features: changed.
func WriteRecord(raw []byte, rec tracker.FeatureRecord) ([]byte, error) {
	block := RenderRecord(rec)
	lines := strings.SplitAfter(string(raw), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	isTopLevel := func(l string) bool {
		t := strings.TrimRight(l, "\r\n")
		return t != "" && t[0] != ' ' && t[0] != '\t' && t[0] != '#' && t != "---"
	}

	start, end := -1, -1
	for i, l := range lines {
		if isTopLevel(l) && (strings.HasPrefix(l, "features:") || strings.HasPrefix(l, "\"features\":")) {
			start = i
			end = i + 1
			for end < len(lines) && !isTopLevel(lines[end]) && !strings.HasPrefix(lines[end], "#") {
				end++
			}
			for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
				end--
			}
			break
		}
	}
	var out []string
	if start >= 0 {
		out = append(append(append(out, lines[:start]...), block), lines[end:]...)
	} else {
		at := -1
		for i, l := range lines {
			if isTopLevel(l) && strings.HasPrefix(l, "template:") {
				at = i + 1
				break
			}
		}
		insert := "\n" + featureRecordComment + "\n" + block
		if at < 0 {
			for i, l := range lines {
				if isTopLevel(l) {
					at = i
					break
				}
			}
			insert = featureRecordComment + "\n" + block + "\n"
		}
		if at < 0 {
			at = len(lines)
		}
		if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
			lines[at-1] += "\n"
		}
		out = append(append(append(out, lines[:at]...), insert), lines[at:]...)
	}
	result := []byte(strings.Join(out, ""))

	before, after := ParseDoc(raw), ParseDoc(result)
	if before == nil || after == nil {
		return nil, fmt.Errorf("workflow.yaml does not parse as a YAML mapping")
	}
	delete(before, "features")
	delete(after, "features")
	got, _ := ReadRecord(result)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(normalizeRecord(got), normalizeRecord(rec)) {
		return nil, fmt.Errorf("could not safely rewrite the features: block; replace it by hand with:\n\n%s", block)
	}
	return result, nil
}

func normalizeRecord(r tracker.FeatureRecord) tracker.FeatureRecord {
	if len(r.Installed) == 0 {
		r.Installed = nil
	}
	if len(r.Declined) == 0 {
		r.Declined = nil
	}
	return r
}

// ScanFeature is one catalog entry's line in the scan report.
type ScanFeature struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Category string `json:"category"`
	Title    string `json:"title"`
	Detected bool   `json:"detected"`
	Matches  int    `json:"matches"`
	Evidence string `json:"evidence,omitempty"`
	// Recorded is the entry's state in the record after the scan:
	// "installed@<version>", "declined" or "".
	Recorded string `json:"recorded"`
}

type ScanResult struct {
	WorkflowFile string                `json:"workflow_file"`
	DryRun       bool                  `json:"dry_run"`
	Written      bool                  `json:"written"`
	HadRecord    bool                  `json:"had_record"`
	Features     []ScanFeature         `json:"features"`
	Record       tracker.FeatureRecord `json:"record"`
	Block        string                `json:"block"`
	Changes      []string              `json:"changes"`
	Notes        []string              `json:"notes"`
}

type ScanOptions struct {
	Installed, Declined, Forget []string
}

// Scan detects catalog features in raw and merges them with
// the existing record and the explicit flags:
//   - installed = detected ∪ already recorded ∪ --installed, minus --forget
//     for undetected ones. A recorded version is kept; new and --installed
//     entries get the catalog version.
//   - declined = recorded ∪ --declined, minus --forget and minus anything
//     installed (a present feature can't be declined).
//
// Ids in the record that the catalog doesn't know are kept, after the known
// ones.
func Scan(path string, raw []byte, opts ScanOptions) (*ScanResult, error) {
	entries, err := Catalog()
	if err != nil {
		return nil, err
	}
	idx := catalogIndex(entries)
	for _, list := range [][]string{opts.Installed, opts.Declined, opts.Forget} {
		for _, id := range list {
			if _, ok := idx[id]; !ok {
				return nil, fmt.Errorf("unknown feature id %q (known: %s)", id, strings.Join(IDs(entries), ", "))
			}
		}
	}
	doc := ParseDoc(raw)
	if doc == nil {
		return nil, fmt.Errorf("%s does not parse as a YAML mapping", path)
	}

	old, hadRecord := ReadRecord(raw)
	oldInstalled, oldDeclined := RecordIndex(old)
	explicitInstall := setOf(opts.Installed)
	explicitDecline := setOf(opts.Declined)
	forget := setOf(opts.Forget)

	res := &ScanResult{WorkflowFile: path, HadRecord: hadRecord, Features: []ScanFeature{}, Changes: []string{}, Notes: []string{}}
	var rec tracker.FeatureRecord
	installedNow := map[string]bool{}
	for _, e := range entries {
		d := Detect(e, doc)
		f := ScanFeature{ID: e.ID, Version: e.Version, Category: e.Category, Title: e.Title, Detected: d.Matches > 0, Matches: d.Matches, Evidence: d.Evidence}
		oldVersion, wasInstalled := oldInstalled[e.ID]
		if f.Detected && explicitDecline[e.ID] {
			return nil, fmt.Errorf("%s is present in %s (%s); remove it from the file before declining it", e.ID, path, d.Evidence)
		}
		if explicitInstall[e.ID] && (explicitDecline[e.ID] || forget[e.ID]) {
			return nil, fmt.Errorf("%s is in --installed and in --declined/--forget; pick one", e.ID)
		}
		install := f.Detected || explicitInstall[e.ID] || (wasInstalled && !forget[e.ID] && !explicitDecline[e.ID])
		if install {
			version := e.Version
			if wasInstalled && oldVersion != "" && !explicitInstall[e.ID] {
				version = oldVersion
			}
			ref := e.ID + "@" + version
			rec.Installed = append(rec.Installed, ref)
			installedNow[e.ID] = true
			f.Recorded = "installed@" + version
			switch {
			case !wasInstalled:
				res.Changes = append(res.Changes, "+ installed "+ref)
			case oldVersion != version:
				res.Changes = append(res.Changes, fmt.Sprintf("~ installed %s (was @%s)", ref, oldVersion))
			}
			if wasInstalled && !f.Detected && !explicitInstall[e.ID] {
				res.Notes = append(res.Notes, fmt.Sprintf("%s is recorded as installed but not detected; kept. If it was removed, run `issue-cli workflow scan --forget %s`.", e.ID, e.ID))
			}
			if f.Detected && forget[e.ID] {
				res.Notes = append(res.Notes, fmt.Sprintf("%s is still present in the file (%s), so it stays installed.", e.ID, d.Evidence))
			}
			if oldDeclined[e.ID] {
				res.Changes = append(res.Changes, "- declined "+e.ID+" (now installed)")
			}
		} else if wasInstalled {
			res.Changes = append(res.Changes, "- installed "+e.ID+"@"+oldVersion)
		}
		res.Features = append(res.Features, f)
	}
	for _, s := range old.Installed {
		if id, _ := featureRef(s); id != "" && idx[id].ID == "" && !forget[id] {
			rec.Installed = append(rec.Installed, strings.TrimSpace(s))
			res.Notes = append(res.Notes, fmt.Sprintf("%s is not in this issue-cli's catalog (newer binary?); kept.", id))
		}
	}

	declinedNow := map[string]bool{}
	for _, e := range entries {
		switch {
		case !oldDeclined[e.ID] && !explicitDecline[e.ID]:
			continue
		case forget[e.ID]:
			if oldDeclined[e.ID] {
				res.Changes = append(res.Changes, "- declined "+e.ID+" (re-opened)")
			}
			continue
		case installedNow[e.ID]:
			continue
		}
		rec.Declined = append(rec.Declined, e.ID)
		declinedNow[e.ID] = true
		if !oldDeclined[e.ID] {
			res.Changes = append(res.Changes, "+ declined "+e.ID)
		}
	}
	for _, s := range old.Declined {
		if id, _ := featureRef(s); id != "" && idx[id].ID == "" && !forget[id] {
			rec.Declined = append(rec.Declined, id)
		}
	}
	for i := range res.Features {
		if declinedNow[res.Features[i].ID] {
			res.Features[i].Recorded = "declined"
		}
	}
	res.Record = rec
	res.Block = RenderRecord(rec)
	return res, nil
}

func setOf(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, s := range list {
		m[s] = true
	}
	return m
}

func IDs(entries []Entry) []string {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	return ids
}
