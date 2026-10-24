package lint

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// checkSchema walks workflow.yaml against the struct tags the parser reads
// (the ones process schema prints). The parser drops a key it doesn't
// know, so a typo silently turns a gate or a prompt off.
func checkSchema(env *Env) []Finding {
	if env.File == "" {
		return nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(env.Raw, &doc); err != nil {
		return []Finding{{Where: env.where(0), Message: "workflow.yaml does not parse: " + err.Error()}}
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return []Finding{{Where: env.where(0), Message: "workflow.yaml is not a mapping of keys"}}
	}
	root := doc.Content[0]
	var out []Finding
	walkSchema(env, root, reflect.TypeOf(tracker.WorkflowConfig{}), "", &out)
	top := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		top[root.Content[i].Value] = true
	}
	if !top["statuses"] {
		out = append(out, Finding{Where: env.where(0),
			Message: "no statuses: list; the project falls back to nothing it can walk",
			Fix:     "add statuses: (see issue-cli process schema)"})
	}
	if !top["transitions"] {
		out = append(out, Finding{Where: env.where(0),
			Message: "no transitions: list; only the legacy statuses[].validation gates run",
			Fix:     "add transitions: with the gates (see issue-cli process schema)"})
	}
	return out
}

type schemaField struct {
	typ        reflect.Type
	deprecated string
}

// schemaFields maps each YAML key a struct reads to its field, as yaml.v3
// reads it: the tag's name, the lowercased field name without one, inline
// structs merged in.
func schemaFields(t reflect.Type) map[string]schemaField {
	out := map[string]schemaField{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if strings.Contains(opts, "inline") {
			for k, v := range schemaFields(deref(f.Type)) {
				out[k] = v
			}
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		out[name] = schemaField{typ: f.Type, deprecated: f.Tag.Get("deprecated")}
	}
	return out
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

func walkSchema(env *Env, node *yaml.Node, t reflect.Type, path string, out *[]Finding) {
	t = deref(t)
	switch {
	case t.Kind() == reflect.Struct && node.Kind == yaml.MappingNode:
		fields := schemaFields(t)
		if len(fields) == 0 {
			return
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, val := node.Content[i], node.Content[i+1]
			f, ok := fields[key.Value]
			if !ok {
				*out = append(*out, unknownKey(env, key, path, fields))
				continue
			}
			if f.deprecated != "" && !emptyNode(val) {
				*out = append(*out, Finding{Where: env.where(key.Line),
					Message: fmt.Sprintf("%s is deprecated", join(path, key.Value)),
					Fix:     f.deprecated})
			}
			walkSchema(env, val, f.typ, join(path, key.Value), out)
		}
	case t.Kind() == reflect.Map && node.Kind == yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			walkSchema(env, node.Content[i+1], t.Elem(), join(path, node.Content[i].Value), out)
		}
	case t.Kind() == reflect.Slice && node.Kind == yaml.SequenceNode:
		for _, item := range node.Content {
			walkSchema(env, item, t.Elem(), path+"[]", out)
		}
	}
}

func unknownKey(env *Env, key *yaml.Node, path string, fields map[string]schemaField) Finding {
	in := "at the top level"
	if path != "" {
		in = "in " + path
	}
	f := Finding{Where: env.where(key.Line),
		Message: fmt.Sprintf("unknown key %q %s; the parser ignores it", key.Value, in),
		Fix:     "remove it (keys: issue-cli process schema)"}
	if near := nearest(key.Value, fields); near != "" {
		f.Fix = fmt.Sprintf("did you mean %q?", near)
	}
	return f
}

// nearest is the known key within two edits of name, "" when none is.
func nearest(name string, fields map[string]schemaField) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best, bestD := "", 3
	for _, k := range keys {
		if d := editDistance(name, k); d < bestD {
			best, bestD = k, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func emptyNode(n *yaml.Node) bool {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Tag == "!!null" || strings.TrimSpace(n.Value) == ""
	case yaml.SequenceNode, yaml.MappingNode:
		return len(n.Content) == 0
	}
	return false
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
