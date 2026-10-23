package health

import (
	"path"
	"strings"
)

// Kinds a text file is counted under. A code file that matches a test glob
// is a test file instead.
const (
	KindCode  = "code"
	KindTest  = "test"
	KindDocs  = "docs"
	KindData  = "data"
	KindOther = "other"
)

// kinds lists every kind, in the order output shows them.
var kinds = []string{KindCode, KindTest, KindDocs, KindData, KindOther}

type language struct {
	name string
	kind string
}

// languages maps a lower-case extension to its language and kind.
var languages = map[string]language{
	".go": {"go", KindCode}, ".cs": {"csharp", KindCode}, ".js": {"js", KindCode}, ".mjs": {"js", KindCode},
	".cjs": {"js", KindCode}, ".jsx": {"js", KindCode}, ".ts": {"ts", KindCode}, ".tsx": {"ts", KindCode},
	".py": {"python", KindCode}, ".rs": {"rust", KindCode}, ".java": {"java", KindCode}, ".kt": {"kotlin", KindCode},
	".c": {"c", KindCode}, ".h": {"c", KindCode}, ".cc": {"cpp", KindCode}, ".cpp": {"cpp", KindCode},
	".hpp": {"cpp", KindCode}, ".swift": {"swift", KindCode}, ".rb": {"ruby", KindCode}, ".php": {"php", KindCode},
	".lua": {"lua", KindCode}, ".sh": {"shell", KindCode}, ".bash": {"shell", KindCode}, ".zsh": {"shell", KindCode},
	".ps1": {"powershell", KindCode}, ".sql": {"sql", KindCode}, ".css": {"css", KindCode}, ".scss": {"css", KindCode},
	".html": {"html", KindCode}, ".htm": {"html", KindCode}, ".tmpl": {"template", KindCode}, ".gohtml": {"template", KindCode},
	".vue": {"vue", KindCode}, ".svelte": {"svelte", KindCode}, ".shader": {"shader", KindCode}, ".hlsl": {"shader", KindCode},
	".gd": {"gdscript", KindCode}, ".fs": {"fsharp", KindCode}, ".vb": {"vb", KindCode}, ".zig": {"zig", KindCode},

	".md": {"markdown", KindDocs}, ".markdown": {"markdown", KindDocs}, ".txt": {"text", KindDocs}, ".rst": {"rst", KindDocs},
	".adoc": {"asciidoc", KindDocs},

	".json": {"json", KindData}, ".yaml": {"yaml", KindData}, ".yml": {"yaml", KindData}, ".toml": {"toml", KindData},
	".xml": {"xml", KindData}, ".csv": {"csv", KindData}, ".tsv": {"csv", KindData}, ".svg": {"svg", KindData},
	".jsonl": {"json", KindData}, ".ini": {"ini", KindData},
}

// specialNames are files without a telling extension.
var specialNames = map[string]language{
	"makefile": {"make", KindCode}, "dockerfile": {"docker", KindCode},
	"go.mod": {"go-mod", KindData}, "go.sum": {"go-mod", KindData},
}

// classify returns the language and kind of a text file. Unknown
// extensions are all language "other", so the lang labels stay a short,
// stable set.
func classify(p string) language {
	base := strings.ToLower(path.Base(p))
	if l, ok := specialNames[base]; ok {
		return l
	}
	if l, ok := languages[path.Ext(base)]; ok {
		return l
	}
	return language{"other", KindOther}
}
