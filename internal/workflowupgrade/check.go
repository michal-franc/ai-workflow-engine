package workflowupgrade

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Paths of the files catalog-check reads, relative to the repository root.
const (
	CatalogPath   = "cmd/issue-cli/templates/workflow-features.yaml"
	TemplatesPath = "cmd/issue-cli/templates/workflow"
	ChangelogPath = "cmd/issue-cli/CHANGELOG.md"
)

// PromptSource is the sources: value of a feature that is prompt text only
// (a template or snippet convention): catalog-check doesn't watch it, the
// documentation box does.
const PromptSource = "@prompt"

// Branch is what catalog-check knows about a work branch: the files it
// changes since the merge-base, their content on each side, and the
// `Catalog: n/a <id>[,<id>] <why>` trailers on its own commits.
type Branch struct {
	Changed []string
	// Base and Head return a file's content; ok is false when it doesn't
	// exist on that side.
	Base, Head func(path string) (data []byte, ok bool)
	Trailers   []string
}

// CheckEntry is one catalog entry whose sources the branch changes.
type CheckEntry struct {
	ID      string   `json:"id"`
	Version string   `json:"version"`
	Files   []string `json:"files"`
	// State is "bumped" (version newer than the last release), "n/a" (a
	// Catalog: n/a trailer names it) or "stale".
	State string `json:"state"`
	Why   string `json:"why,omitempty"`
}

// CheckTemplate is a bundled template the branch changes.
type CheckTemplate struct {
	Name  string `json:"name"`
	Stamp string `json:"stamp"`
	State string `json:"state"` // "bumped" or "stale"
}

// CheckReport is the result of CheckBranch.
type CheckReport struct {
	LastRelease string          `json:"last_release"`
	NextRelease string          `json:"next_release"`
	Entries     []CheckEntry    `json:"entries"`
	Templates   []CheckTemplate `json:"templates"`
}

// OK reports whether every touched entry and template is bumped or marked n/a.
func (r *CheckReport) OK() bool {
	for _, e := range r.Entries {
		if e.State == "stale" {
			return false
		}
	}
	for _, t := range r.Templates {
		if t.State == "stale" {
			return false
		}
	}
	return true
}

var releaseHeading = regexp.MustCompile(`(?m)^## (v\d+\.\d+\.\d+)\b`)

// LastRelease is the newest `## vX.Y.Z` heading in a changelog.
func LastRelease(changelog []byte) (string, bool) {
	m := releaseHeading.FindSubmatch(changelog)
	if m == nil {
		return "", false
	}
	return string(m[1]), true
}

// NextRelease is the release after last: its minor version bumped.
func NextRelease(last string) string {
	v := ParseVersion(last)
	if v == nil {
		return ""
	}
	return fmt.Sprintf("v%d.%d.0", v[0], v[1]+1)
}

// CheckBranch asks for a version bump on every catalog entry whose sources
// the branch changes, and a stamp bump on every bundled template it changes.
// The catalog and templates are read from the branch head, so the check
// sees the bumps the branch itself makes.
func CheckBranch(b Branch, lastRelease string) (*CheckReport, error) {
	rep := &CheckReport{LastRelease: lastRelease, NextRelease: NextRelease(lastRelease), Entries: []CheckEntry{}, Templates: []CheckTemplate{}}
	raw, ok := b.Head(CatalogPath)
	if !ok {
		return nil, fmt.Errorf("%s not found: catalog-check runs in the issue-viewer repository", CatalogPath)
	}
	var doc struct {
		Entries []Entry `yaml:"entries"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", CatalogPath, err)
	}
	na := naTrailers(b.Trailers)
	newer := func(v string) bool { return CompareVersions(v, lastRelease) > 0 }

	for _, e := range doc.Entries {
		var files []string
		for _, f := range b.Changed {
			if sourceChanged(e.Sources, f, b) {
				files = append(files, f)
			}
		}
		if len(files) == 0 {
			continue
		}
		ce := CheckEntry{ID: e.ID, Version: e.Version, Files: files, State: "stale"}
		switch why, marked := na[e.ID]; {
		case newer(e.Version):
			ce.State = "bumped"
		case marked:
			ce.State, ce.Why = "n/a", why
		}
		rep.Entries = append(rep.Entries, ce)
	}

	for _, f := range b.Changed {
		if path.Dir(f) != TemplatesPath || path.Ext(f) != ".yaml" {
			continue
		}
		data, ok := b.Head(f)
		if !ok {
			continue
		}
		name := strings.TrimSuffix(path.Base(f), ".yaml")
		ct := CheckTemplate{Name: name, Stamp: ReadStamp(data), State: "stale"}
		if n, v, ok := ParseStamp(ct.Stamp); ok && n == name && newer(v) {
			ct.State = "bumped"
		}
		rep.Templates = append(rep.Templates, ct)
	}
	return rep, nil
}

// naTrailers parses `Catalog: n/a <id>[,<id>...] <why>` trailer values into
// id → why.
func naTrailers(values []string) map[string]string {
	out := map[string]string{}
	for _, v := range values {
		rest, ok := strings.CutPrefix(strings.TrimSpace(v), "n/a ")
		if !ok {
			continue
		}
		ids, why, _ := strings.Cut(strings.TrimSpace(rest), " ")
		for _, id := range strings.Split(ids, ",") {
			if id = strings.TrimSpace(id); id != "" {
				out[id] = strings.TrimSpace(why)
			}
		}
	}
	return out
}

// sourceChanged reports whether the changed file f is one of the sources
// and, for a `file.go#Symbol` source, whether that symbol's code differs.
func sourceChanged(sources []string, f string, b Branch) bool {
	for _, s := range sources {
		if s == PromptSource {
			continue
		}
		glob, sym, hasSym := strings.Cut(s, "#")
		if !MatchSource(glob, f) {
			continue
		}
		if !hasSym {
			return true
		}
		before, _ := b.Base(f)
		after, _ := b.Head(f)
		if symbolSource(before, sym) != symbolSource(after, sym) {
			return true
		}
	}
	return false
}

// MatchSource reports whether the repo-relative slash path p matches a
// sources: glob. A `**` segment matches any number of segments; other
// segments use path.Match. Globs are anchored at the repository root.
func MatchSource(glob, p string) bool {
	return matchSegments(strings.Split(strings.Trim(glob, "/"), "/"), strings.Split(p, "/"))
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(segs); i++ {
				if matchSegments(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// symbolSource is the code of the top-level Go declarations named sym (a
// func or method name, a type, a const or a var), doc comments left out.
// "" when the file doesn't parse or has no such symbol.
func symbolSource(src []byte, sym string) string {
	decls := SymbolDecls(src, sym)
	var b strings.Builder
	for _, d := range decls {
		b.WriteString(d)
		b.WriteByte('\n')
	}
	return b.String()
}

// SymbolDecls returns the source of each top-level declaration named sym.
func SymbolDecls(src []byte, sym string) []string {
	if len(src) == 0 {
		return nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	text := func(n ast.Node) string {
		return string(bytes.TrimSpace(src[fset.Position(n.Pos()).Offset:fset.Position(n.End()).Offset]))
	}
	var out []string
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Name.Name == sym {
				out = append(out, text(d))
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.Name == sym {
						out = append(out, text(s))
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name == sym {
							out = append(out, text(s))
						}
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}
