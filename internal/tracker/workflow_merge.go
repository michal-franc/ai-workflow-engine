package tracker

import "strings"

func cloneBoardConfig(b WorkflowBoardConfig) WorkflowBoardConfig {
	return WorkflowBoardConfig{
		CardFields: append([]string(nil), b.CardFields...),
		Columns:    append([]string(nil), b.Columns...),
	}
}

func cloneScoringConfig(s ScoringConfig) ScoringConfig {
	out := ScoringConfig{
		Enabled:     s.Enabled,
		DefaultSort: s.DefaultSort,
		Formula: ScoringFormula{
			DueDate: s.Formula.DueDate,
			Age:     s.Formula.Age,
		},
	}
	if s.Formula.Priority != nil {
		out.Formula.Priority = make(ScoringPriority, len(s.Formula.Priority))
		for k, v := range s.Formula.Priority {
			out.Formula.Priority[k] = v
		}
	}
	if s.Formula.Labels != nil {
		out.Formula.Labels = make(ScoringLabels, len(s.Formula.Labels))
		for k, v := range s.Formula.Labels {
			out.Formula.Labels[k] = v
		}
	}
	return out
}

func (w *WorkflowConfig) Clone() *WorkflowConfig {
	if w == nil {
		return nil
	}

	clone := &WorkflowConfig{
		Statuses:    append([]WorkflowStatus(nil), w.Statuses...),
		Transitions: make([]WorkflowTransition, len(w.Transitions)),
		Systems:     make(map[string]WorkflowOverlay, len(w.Systems)),
		Board:       cloneBoardConfig(w.Board),
		Scoring:     cloneScoringConfig(w.Scoring),
		AllowShell:  w.AllowShell,
		LookupIssue: w.LookupIssue,
		IssuesRoot:  w.IssuesRoot,
	}
	for i := range w.Transitions {
		clone.Transitions[i] = WorkflowTransition{
			From:     w.Transitions[i].From,
			To:       w.Transitions[i].To,
			Actions:  append([]WorkflowAction(nil), w.Transitions[i].Actions...),
			Fields:   append([]WorkflowField(nil), w.Transitions[i].Fields...),
			CTALabel: w.Transitions[i].CTALabel,
		}
	}
	for name, overlay := range w.Systems {
		clonedOverlay := WorkflowOverlay{
			Statuses:    append([]WorkflowStatus(nil), overlay.Statuses...),
			Transitions: make([]WorkflowTransition, len(overlay.Transitions)),
		}
		for i := range overlay.Transitions {
			clonedOverlay.Transitions[i] = WorkflowTransition{
				From:     overlay.Transitions[i].From,
				To:       overlay.Transitions[i].To,
				Actions:  append([]WorkflowAction(nil), overlay.Transitions[i].Actions...),
				Fields:   append([]WorkflowField(nil), overlay.Transitions[i].Fields...),
				CTALabel: overlay.Transitions[i].CTALabel,
			}
		}
		clone.Systems[name] = clonedOverlay
	}
	return clone
}

func (w *WorkflowConfig) ForSystem(system string) *WorkflowConfig {
	if w == nil {
		return nil
	}
	if strings.TrimSpace(system) == "" || len(w.Systems) == 0 {
		return w.Clone()
	}

	overlay, ok := w.Systems[system]
	if !ok {
		return w.Clone()
	}

	merged := w.Clone()
	merged.Merge(&WorkflowConfig{
		Statuses:    overlay.Statuses,
		Transitions: overlay.Transitions,
	})
	return merged
}

// Merge overlays custom workflow config onto this one.
// For matching statuses: appends validation and side_effects (deduped),
// overrides description and template if set in custom.
// New statuses from custom are appended.
func (w *WorkflowConfig) Merge(custom *WorkflowConfig) {
	for _, cs := range custom.Statuses {
		base := w.GetStatus(cs.Name)
		if base == nil {
			w.Statuses = append(w.Statuses, cs)
			continue
		}
		if cs.Description != "" {
			base.Description = cs.Description
		}
		if cs.Prompt != "" {
			base.Prompt = cs.Prompt
		}
		if cs.Template != "" {
			base.Template = cs.Template
		}
		if cs.Optional {
			base.Optional = true
		}
		if cs.Global {
			base.Global = true
		}
		base.Validation = appendUnique(base.Validation, cs.Validation)
		base.SideEffects = appendUnique(base.SideEffects, cs.SideEffects)
	}

	for _, ct := range custom.Transitions {
		base := w.GetTransition(ct.From, ct.To)
		if base == nil {
			w.Transitions = append(w.Transitions, WorkflowTransition{
				From:     ct.From,
				To:       ct.To,
				Actions:  append([]WorkflowAction(nil), ct.Actions...),
				CTALabel: ct.CTALabel,
			})
			continue
		}
		base.Actions = appendUniqueActions(base.Actions, ct.Actions)
		if ct.CTALabel != "" {
			base.CTALabel = ct.CTALabel
		}
	}

	if len(custom.Systems) > 0 {
		if w.Systems == nil {
			w.Systems = make(map[string]WorkflowOverlay, len(custom.Systems))
		}
		for name, overlay := range custom.Systems {
			existing := w.Systems[name]
			cfg := &WorkflowConfig{
				Statuses:    append([]WorkflowStatus(nil), existing.Statuses...),
				Transitions: append([]WorkflowTransition(nil), existing.Transitions...),
			}
			cfg.Merge(&WorkflowConfig{
				Statuses:    overlay.Statuses,
				Transitions: overlay.Transitions,
			})
			w.Systems[name] = WorkflowOverlay{
				Statuses:    cfg.Statuses,
				Transitions: cfg.Transitions,
			}
		}
	}
}

func appendUnique(base, extra []string) []string {
	seen := make(map[string]bool, len(base))
	for _, v := range base {
		seen[v] = true
	}
	for _, v := range extra {
		if !seen[v] {
			base = append(base, v)
			seen[v] = true
		}
	}
	return base
}

func appendUniqueActions(base, extra []WorkflowAction) []WorkflowAction {
	seen := make(map[string]bool, len(base))
	keyFor := func(action WorkflowAction) string {
		return strings.Join([]string{
			action.Type,
			action.Rule,
			action.Status,
			action.Title,
			action.Body,
			action.Prompt,
			action.Field,
			action.Value,
			strings.Join(action.Values, ","),
			action.Pattern,
			action.Section,
			action.Command,
			action.RefKey,
			action.LinkedStatus,
			action.Hint,
		}, "\x00")
	}
	for _, action := range base {
		seen[keyFor(action)] = true
	}
	for _, action := range extra {
		key := keyFor(action)
		if !seen[key] {
			base = append(base, action)
			seen[key] = true
		}
	}
	return base
}
