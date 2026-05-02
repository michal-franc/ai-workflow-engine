package tracker

import (
	"reflect"
	"strings"
)

// SchemaFieldDoc describes one YAML field in a workflow struct.
type SchemaFieldDoc struct {
	Name        string
	Type        string
	Description string
	Optional    bool
}

// SchemaSection groups fields for a single workflow type.
type SchemaSection struct {
	Path   string // e.g. "statuses[]"
	Title  string // struct name, e.g. "WorkflowStatus"
	Fields []SchemaFieldDoc
}

// SchemaNamedDoc describes a named value (action type or validation rule) that
// lives in a switch statement rather than a struct field.
type SchemaNamedDoc struct {
	Name        string
	Description string
}

// WorkflowActionTypes lists every action.type value honored by
// transitionActions / BuildTransitionPreview. Kept next to the switch so a new
// type must be documented here or reviewers will notice the drift.
var WorkflowActionTypes = []SchemaNamedDoc{
	{Name: "validate", Description: "Run a validation rule (see 'Validation rules'); blocks the transition on failure"},
	{Name: "require_human_approval", Description: "Require the issue to be human-approved for the target status before transitioning"},
	{Name: "append_section", Description: "Append a '## <title>' section with the given body if it does not already exist"},
	{Name: "inject_prompt", Description: "Inject prompt text into the agent's entry guidance on this transition"},
	{Name: "set_fields", Description: "Set or clear a frontmatter field (field=assignee|priority|status|human_approval, value=\"\" to clear)"},
}

// WorkflowValidationRules lists every validation rule recognized by checkRule
// or the validations sub-package. The first block is the legacy colon-string
// shorthand; the second block is the structured-action form (one validator
// per file under internal/tracker/validations/), which uses companion
// fields on transitions[].actions[] (field, values, pattern, section, etc.).
var WorkflowValidationRules = []SchemaNamedDoc{
	{Name: "body_not_empty", Description: "Issue body must contain non-whitespace content"},
	{Name: "has_checkboxes", Description: "Body must contain at least one '- [ ]' or '- [x]' checkbox anywhere"},
	{Name: "section_has_checkboxes: <Title>", Description: "Named '## <Title>' section must contain at least one checkbox"},
	{Name: "has_assignee", Description: "Issue must have a non-empty assignee"},
	{Name: "all_checkboxes_checked", Description: "Every checkbox in the body must be checked"},
	{Name: "section_checkboxes_checked: <Title>", Description: "Every checkbox inside '## <Title>' must be checked (silent pass if the section is missing)"},
	{Name: "has_test_plan", Description: "Body must contain '## Test Plan' with '### Automated' and '### Manual' subsections"},
	{Name: "has_comment_prefix: <prefix>", Description: "At least one comment must start with the given prefix (e.g. 'tests:', 'docs:')"},
	{Name: "approved_for: <status>", Description: "Issue must be human-approved for the given status"},
	{Name: "human_approval: <status>", Description: "Alias for 'approved_for: <status>'"},

	// Structured validators — populate companion fields on the action.
	{Name: "field_present", Description: "Frontmatter key (action.field) exists on the issue"},
	{Name: "field_not_empty", Description: "Frontmatter key (action.field) exists and has a non-blank value"},
	{Name: "field_in", Description: "Frontmatter key (action.field) value is one of action.values"},
	{Name: "field_matches", Description: "Frontmatter key (action.field) value matches Go RE2 regex action.pattern"},
	{Name: "has_label", Description: "Issue labels contain the name in action.field"},
	{Name: "has_any_label", Description: "Issue has at least one label"},
	{Name: "has_pr_url", Description: "Frontmatter \"pr\" is a github pull request URL"},
	{Name: "linked_issue_in_status", Description: "Issue referenced by action.ref_key (frontmatter key holding a slug) has status action.linked_status"},
	{Name: "has_section", Description: "Body contains a '## <action.section>' heading"},
	{Name: "section_min_length", Description: "Section '## <action.section>' has at least action.min non-whitespace chars"},
	{Name: "section_max_length", Description: "Section '## <action.section>' has at most action.max non-whitespace chars (missing section passes)"},
	{Name: "no_todo_markers", Description: "Body contains no whole-word TODO or FIXME markers"},
	{Name: "command_succeeds", Description: "Shell command in action.command exits 0; templated with {{slug}}/{{number}}/{{repo}}/{{system}}; requires top-level allow_shell=true"},
}

// WorkflowSchemaSections returns the YAML schema for workflow.yaml, derived by
// reflecting the struct tags on the workflow types. Adding a new `yaml:"..."`
// field to any of these structs surfaces it here automatically, so the CLI
// docs cannot drift from the parser.
func WorkflowSchemaSections() []SchemaSection {
	return []SchemaSection{
		{Path: "(top level)", Title: "WorkflowConfig", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowConfig{}))},
		{Path: "statuses[]", Title: "WorkflowStatus", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowStatus{}))},
		{Path: "transitions[]", Title: "WorkflowTransition", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowTransition{}))},
		{Path: "transitions[].actions[]", Title: "WorkflowAction", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowAction{}))},
		{Path: "transitions[].fields[]", Title: "WorkflowField", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowField{}))},
		{Path: "board", Title: "WorkflowBoardConfig", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowBoardConfig{}))},
		{Path: "systems[<name>]", Title: "WorkflowOverlay", Fields: schemaFieldsOf(reflect.TypeOf(WorkflowOverlay{}))},
		{Path: "scoring", Title: "ScoringConfig", Fields: schemaFieldsOf(reflect.TypeOf(ScoringConfig{}))},
		{Path: "scoring.formula", Title: "ScoringFormula", Fields: schemaFieldsOf(reflect.TypeOf(ScoringFormula{}))},
		{Path: "scoring.formula.due_date", Title: "ScoringDueDate", Fields: schemaFieldsOf(reflect.TypeOf(ScoringDueDate{}))},
		{Path: "scoring.formula.age", Title: "ScoringAge", Fields: schemaFieldsOf(reflect.TypeOf(ScoringAge{}))},
	}
}

func schemaFieldsOf(t reflect.Type) []SchemaFieldDoc {
	var out []SchemaFieldDoc
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		yamlTag := f.Tag.Get("yaml")
		if yamlTag == "" || yamlTag == "-" {
			continue
		}
		parts := strings.Split(yamlTag, ",")
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		optional := false
		for _, p := range parts[1:] {
			if strings.TrimSpace(p) == "omitempty" {
				optional = true
			}
		}
		out = append(out, SchemaFieldDoc{
			Name:        name,
			Type:        schemaTypeName(f.Type),
			Description: f.Tag.Get("desc"),
			Optional:    optional,
		})
	}
	return out
}

func schemaTypeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Ptr:
		return schemaTypeName(t.Elem())
	case reflect.Slice, reflect.Array:
		return "list<" + schemaTypeName(t.Elem()) + ">"
	case reflect.Map:
		return "map<" + schemaTypeName(t.Key()) + "," + schemaTypeName(t.Elem()) + ">"
	case reflect.Struct:
		return t.Name()
	case reflect.Bool:
		return "bool"
	case reflect.String:
		return "string"
	default:
		if t.Kind() >= reflect.Int && t.Kind() <= reflect.Int64 {
			return "int"
		}
		if t.Kind() >= reflect.Uint && t.Kind() <= reflect.Uint64 {
			return "int"
		}
		if t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64 {
			return "float"
		}
		return t.Kind().String()
	}
}
