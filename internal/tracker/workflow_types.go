package tracker

import (
	"fmt"
	"sort"
	"strings"
)

// HasTypes reports whether the workflow defines types of work.
func (w *WorkflowConfig) HasTypes() bool {
	return w != nil && len(w.Types) > 0
}

// TypeNames returns the defined type names: default_type first, the rest
// alphabetically. Empty when the project has no types.
func (w *WorkflowConfig) TypeNames() []string {
	if !w.HasTypes() {
		return nil
	}
	names := make([]string, 0, len(w.Types))
	for name := range w.Types {
		if name != w.DefaultType {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if _, ok := w.Types[w.DefaultType]; ok {
		names = append([]string{w.DefaultType}, names...)
	}
	return names
}

// ResolveType maps an issue's type: value to the type that governs it.
// Empty or unknown values fall back to default_type; known reports whether
// name itself was a defined type (always true for an empty name). Projects
// without types resolve everything to "".
func (w *WorkflowConfig) ResolveType(name string) (resolved string, known bool) {
	if !w.HasTypes() {
		return "", true
	}
	name = strings.TrimSpace(name)
	if _, ok := w.Types[name]; ok {
		return name, true
	}
	fallback := ""
	if _, ok := w.Types[w.DefaultType]; ok {
		fallback = w.DefaultType
	}
	return fallback, name == ""
}

// UnknownTypeWarning returns a one-line warning when name is set but is not a
// defined type, or "" when there is nothing to warn about.
func (w *WorkflowConfig) UnknownTypeWarning(slug, name string) string {
	if !w.HasTypes() {
		return ""
	}
	resolved, known := w.ResolveType(name)
	if known {
		return ""
	}
	using := "the full base path"
	if resolved != "" {
		using = fmt.Sprintf("default %q", resolved)
	}
	return fmt.Sprintf("type %q is not defined (types: %s); using %s. Fix: issue-cli set-type %s <type>",
		name, strings.Join(w.TypeNames(), ", "), using, slug)
}

// ForType returns the workflow scoped to one type of work: statuses filtered
// to the type's path (base order kept), base edges kept only when both ends
// are on the path, then the type's overlay merged (replace: true overwrites a
// base edge). Without types, or for a name that resolves to no type, it
// returns a plain clone.
func (w *WorkflowConfig) ForType(name string) *WorkflowConfig {
	if w == nil {
		return nil
	}
	clone := w.Clone()
	resolved, _ := w.ResolveType(name)
	if resolved == "" {
		return clone
	}
	t := w.Types[resolved]
	clone.ActiveType = resolved

	statuses, transitions := t.Statuses, t.Transitions
	if len(t.Path) > 0 {
		on := make(map[string]bool, len(t.Path))
		for _, s := range t.Path {
			if w.GetStatus(s) != nil {
				on[s] = true
			}
		}
		kept := clone.Statuses[:0]
		for _, s := range clone.Statuses {
			if on[s.Name] {
				kept = append(kept, s)
			}
		}
		clone.Statuses = kept
		edges := clone.Transitions[:0]
		for _, tr := range clone.Transitions {
			if (tr.From == "*" || on[tr.From]) && on[tr.To] {
				edges = append(edges, tr)
			}
		}
		clone.Transitions = edges
		clone.typePath = on
		statuses, transitions = restrictToPath(statuses, transitions, on)
	}
	clone.Merge(&WorkflowConfig{Statuses: statuses, Transitions: transitions})
	return clone
}

// ForIssue resolves the workflow that governs one issue: base → type →
// system overlay. When the issue sits on a status its type's path does not
// include (hand edit, or a retype without a status pick), that status is kept
// at its base position as a global, optional exit so the issue can move back
// onto its path; OffPathStatus records it so callers can warn.
func (w *WorkflowConfig) ForIssue(issue *Issue) *WorkflowConfig {
	if w == nil {
		return nil
	}
	if issue == nil {
		return w.Clone()
	}
	scoped := w.ForType(issue.Type).ForSystem(issue.System)
	if scoped.typePath == nil || issue.Status == "" || scoped.GetStatusIndex(issue.Status) != -1 {
		return scoped
	}
	base := w.GetStatus(issue.Status)
	if base == nil {
		return scoped
	}
	rescued := *base
	rescued.Global = true
	rescued.Optional = true
	baseIdx := w.GetStatusIndex(issue.Status)
	at := len(scoped.Statuses)
	for i, s := range scoped.Statuses {
		if w.GetStatusIndex(s.Name) > baseIdx {
			at = i
			break
		}
	}
	scoped.Statuses = append(scoped.Statuses[:at], append([]WorkflowStatus{rescued}, scoped.Statuses[at:]...)...)
	scoped.typePath[issue.Status] = true
	scoped.OffPathStatus = issue.Status
	return scoped
}

// PathLine renders the scoped lifecycle as "a → b → c", leaving out optional
// statuses (side states such as obsolete or parked).
func (w *WorkflowConfig) PathLine() string {
	var names []string
	for _, s := range w.Statuses {
		if !s.Optional {
			names = append(names, s.Name)
		}
	}
	return strings.Join(names, " → ")
}

// OffPathWarning returns a warning when ForIssue had to rescue the issue's
// status, or "".
func (w *WorkflowConfig) OffPathWarning() string {
	if w == nil || w.OffPathStatus == "" {
		return ""
	}
	return fmt.Sprintf("status %q is not on type %q's path (%s); move it onto the path with issue-cli transition",
		w.OffPathStatus, w.ActiveType, w.PathLine())
}

// restrictToPath drops overlay statuses and edges that leave the path.
func restrictToPath(statuses []WorkflowStatus, transitions []WorkflowTransition, on map[string]bool) ([]WorkflowStatus, []WorkflowTransition) {
	var ss []WorkflowStatus
	for _, s := range statuses {
		if on[s.Name] {
			ss = append(ss, s)
		}
	}
	var ts []WorkflowTransition
	for _, t := range transitions {
		if (t.From == "*" || on[t.From]) && on[t.To] {
			ts = append(ts, t)
		}
	}
	return ss, ts
}

// Lint reports problems in the types: configuration. Projects without types
// get no warnings. See docs/Workflow/types.md "Lint".
func (w *WorkflowConfig) Lint() []string {
	if !w.HasTypes() {
		return nil
	}
	var out []string
	names := w.TypeNames()
	if strings.TrimSpace(w.DefaultType) == "" {
		out = append(out, fmt.Sprintf("default_type is not set: issues without a type: walk the full base path (set default_type to one of %s)", strings.Join(names, ", ")))
	} else if _, ok := w.Types[w.DefaultType]; !ok {
		out = append(out, fmt.Sprintf("default_type %q is not a defined type (types: %s)", w.DefaultType, strings.Join(names, ", ")))
	}

	// Sections appended anywhere in the base workflow; a type that checks one
	// of these but drops every edge that appends it has a gap.
	baseAppends := appendedSections(w)

	for _, name := range names {
		t := w.Types[name]
		last := -1
		on := map[string]bool{}
		for _, s := range t.Path {
			idx := w.GetStatusIndex(s)
			if idx == -1 {
				out = append(out, fmt.Sprintf("type %s: path entry %q is not a base status (ignored)", name, s))
				continue
			}
			if idx < last {
				out = append(out, fmt.Sprintf("type %s: path lists %q out of base order (base order is used)", name, s))
			}
			if idx > last {
				last = idx
			}
			on[s] = true
		}
		if len(t.Path) > 0 {
			for _, s := range t.Statuses {
				if !on[s.Name] {
					out = append(out, fmt.Sprintf("type %s: status override %q is not on the path (ignored)", name, s.Name))
				}
			}
			for _, tr := range t.Transitions {
				if (tr.From != "*" && !on[tr.From]) || !on[tr.To] {
					out = append(out, fmt.Sprintf("type %s: transition %s → %s leaves the path (ignored)", name, tr.From, tr.To))
				}
			}
		}

		scoped := w.ForType(name)
		appended := appendedSections(scoped)
		for _, tr := range scoped.Transitions {
			for _, a := range tr.Actions {
				section := checkedSection(a)
				if section == "" || appended[section] || !baseAppends[section] {
					continue
				}
				out = append(out, fmt.Sprintf("type %s: %s → %s checks section %q, but nothing on %s's path appends it — add an append_section to an earlier edge",
					name, tr.From, tr.To, section, name))
			}
		}
		for _, s := range t.Statuses {
			if s.Prompt == "" {
				continue
			}
			for _, sys := range sortedSystemNames(w) {
				for _, os := range w.Systems[sys].Statuses {
					if os.Name == s.Name && os.Prompt != "" {
						out = append(out, fmt.Sprintf("type %s and system %s both set the %q prompt; the system's replaces the type's (use inject_prompt on an edge in the system instead)", name, sys, s.Name))
					}
				}
			}
		}
	}
	return out
}

func sortedSystemNames(w *WorkflowConfig) []string {
	names := make([]string, 0, len(w.Systems))
	for n := range w.Systems {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// appendedSections collects every section title an append_section action or
// legacy status template adds.
func appendedSections(w *WorkflowConfig) map[string]bool {
	out := map[string]bool{}
	for _, t := range w.Transitions {
		for _, a := range t.Actions {
			if a.Type == "append_section" && strings.TrimSpace(a.Title) != "" {
				out[strings.TrimSpace(a.Title)] = true
			}
		}
	}
	for _, s := range w.Statuses {
		for _, line := range strings.Split(s.Template, "\n") {
			if strings.HasPrefix(line, "## ") {
				out[strings.TrimSpace(strings.TrimPrefix(line, "## "))] = true
			}
		}
	}
	return out
}

// checkedSection returns the section a validate action requires, or "".
func checkedSection(a WorkflowAction) string {
	if a.Type != "validate" {
		return ""
	}
	if a.Section != "" {
		switch a.Rule {
		case "has_section", "section_min_length":
			return strings.TrimSpace(a.Section)
		}
	}
	for _, prefix := range []string{"section_checkboxes_checked:", "section_has_checkboxes:"} {
		if strings.HasPrefix(a.Rule, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(a.Rule, prefix))
		}
	}
	return ""
}
