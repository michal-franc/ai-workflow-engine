package health

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// fn is one function a scan found: its name and length in lines (signature
// to closing brace).
type fn struct {
	name  string
	line  int
	lines int
}

// Parsers per language. Go is parsed exactly; brace languages go through a
// small scanner; Python and GDScript use indentation. A language missing here
// has no functions to measure (CSS, HTML, SQL, templates, ...), except those
// in unparsedLangs, which do but have no scanner yet.
var (
	braceLangs    = map[string]bool{"csharp": true, "js": true, "ts": true, "java": true, "kotlin": true, "c": true, "cpp": true, "rust": true, "swift": true, "php": true, "shell": true, "shader": true}
	indentLangs   = map[string]string{"python": "def ", "gdscript": "func "}
	unparsedLangs = map[string]bool{"ruby": true, "lua": true, "fsharp": true, "vb": true, "zig": true, "powershell": true}
)

// funcs returns the functions in data; ok is false when lang has functions
// but they could not be read (a Go syntax error, a language without a
// scanner). measured is false for languages without functions.
func funcs(lang string, data []byte) (out []fn, measured, ok bool) {
	switch {
	case lang == "go":
		out, ok = goFuncs(data)
		return out, true, ok
	case braceLangs[lang]:
		return braceFuncs(data, lang), true, true
	case indentLangs[lang] != "":
		return indentFuncs(data, indentLangs[lang]), true, true
	case unparsedLangs[lang]:
		return nil, true, false
	}
	return nil, false, true
}

// goFuncs reads every function and method with a body.
func goFuncs(data []byte) ([]fn, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", data, parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	var out []fn
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		name := fd.Name.Name
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			name = recvName(fd.Recv.List[0].Type) + "." + name
		}
		start, end := fset.Position(fd.Pos()).Line, fset.Position(fd.End()).Line
		out = append(out, fn{name, start, end - start + 1})
	}
	return out, true
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}

// controlWords start a block that is not a function.
var controlWords = map[string]bool{"if": true, "else": true, "for": true, "foreach": true, "while": true, "switch": true, "catch": true,
	"using": true, "lock": true, "fixed": true, "do": true, "try": true, "return": true, "new": true, "match": true, "when": true,
	"unsafe": true, "checked": true, "unchecked": true, "with": true, "loop": true, "select": true, "case": true, "elif": true}

// braceFuncs finds function bodies in a brace language: a block whose
// header (the text since the previous ; { or }) has a parameter list, a
// name before it, no control keyword in front and no assignment or lambda.
// Comments and strings are skipped. It is a heuristic: a lambda or an
// initializer is not counted, a constructor or a local function is.
func braceFuncs(data []byte, lang string) []fn {
	singleQuoted := lang == "js" || lang == "ts" || lang == "php" || lang == "shell"
	type open struct {
		isFn bool
		name string
		line int
	}
	var out []fn
	var stack []open
	var header strings.Builder
	headerLine := 1
	line := 1
	n := len(data)
	for i := 0; i < n; i++ {
		c := data[i]
		switch {
		case c == '\n':
			line++
			if header.Len() > 0 {
				header.WriteByte(' ')
			}
			continue
		case c == '/' && i+1 < n && data[i+1] == '/':
			for i < n && data[i] != '\n' {
				i++
			}
			i--
			continue
		case c == '#' && header.Len() == 0:
			// a preprocessor line (C, C#) or a shell comment
			for i < n && data[i] != '\n' {
				i++
			}
			i--
			continue
		case c == '/' && i+1 < n && data[i+1] == '*':
			i += 2
			for i+1 < n && !(data[i] == '*' && data[i+1] == '/') {
				if data[i] == '\n' {
					line++
				}
				i++
			}
			i++
			continue
		case c == '"' || c == '`' || c == '\'' && singleQuoted:
			i = skipString(data, i, c, &line)
			header.WriteString(`""`)
			continue
		case c == '\'' && i+2 < n && (data[i+2] == '\'' || data[i+1] == '\\' && i+3 < n && data[i+3] == '\''):
			// a char literal such as '{' or '\''
			if data[i+1] == '\\' {
				i += 3
			} else {
				i += 2
			}
			header.WriteString("''")
			continue
		case c == ';':
			header.Reset()
			continue
		case c == '{':
			name, isFn := funcHeader(header.String())
			stack = append(stack, open{isFn, name, headerLine})
			header.Reset()
			continue
		case c == '}':
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.isFn {
					out = append(out, fn{top.name, top.line, line - top.line + 1})
				}
			}
			header.Reset()
			continue
		}
		if header.Len() == 0 {
			if c == ' ' || c == '\t' || c == '\r' {
				continue
			}
			headerLine = line
		}
		header.WriteByte(c)
	}
	return out
}

// skipString moves past a string literal opened by q at i; returns the index
// of its closing quote.
func skipString(data []byte, i int, q byte, line *int) int {
	for i++; i < len(data); i++ {
		switch data[i] {
		case '\\':
			i++
		case '\n':
			*line++
			if q != '`' {
				return i // an unterminated or verbatim string: stop at the line end
			}
		case q:
			return i
		}
	}
	return i
}

// funcHeader decides whether the text before a { opens a function, and its
// name.
func funcHeader(h string) (string, bool) {
	h = strings.Join(strings.Fields(h), " ")
	open := strings.IndexByte(h, '(')
	closeAt := strings.LastIndexByte(h, ')')
	if open <= 0 || closeAt < open {
		return "", false
	}
	if strings.Contains(h, "=>") || strings.Contains(h, "->") && strings.Contains(h[closeAt:], ",") {
		return "", false
	}
	if eq := strings.IndexByte(h, '='); eq >= 0 && eq < open && !strings.Contains(h[:open], "operator") {
		return "", false // an assignment or initializer
	}
	if tail := h[closeAt+1:]; strings.ContainsAny(tail, "(,;") || len(tail) > 60 {
		return "", false
	}
	first := strings.Fields(h)[0]
	if controlWords[strings.TrimRight(first, "(")] {
		return "", false
	}
	before := strings.TrimSpace(h[:open])
	if i := strings.LastIndexAny(before, " .:*&"); i >= 0 {
		before = before[i+1:]
	}
	if before == "" || controlWords[before] || !isIdent(before) {
		return "", false
	}
	return before, true
}

func isIdent(s string) bool {
	for i, r := range s {
		ok := r == '_' || r == '$' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9' || r == '<' || r == '>' || r == '~' || r == '!'
		if !ok {
			return false
		}
	}
	return s != ""
}

// indentFuncs finds `def name(` (or `func name(`) blocks; a block ends at
// the next non-blank line indented no deeper than its keyword.
func indentFuncs(data []byte, keyword string) []fn {
	lines := strings.Split(string(data), "\n")
	var out []fn
	for i, l := range lines {
		trimmed := strings.TrimLeft(l, " \t")
		kw := keyword
		if strings.HasPrefix(trimmed, "async "+keyword) {
			kw = "async " + keyword
		}
		if !strings.HasPrefix(trimmed, kw) {
			continue
		}
		name, _, ok := strings.Cut(strings.TrimPrefix(trimmed, kw), "(")
		if !ok {
			continue
		}
		indent := len(l) - len(trimmed)
		end := i
		for j := i + 1; j < len(lines); j++ {
			t := strings.TrimLeft(lines[j], " \t")
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if len(lines[j])-len(t) <= indent {
				break
			}
			end = j
		}
		out = append(out, fn{strings.TrimSpace(name), i + 1, end - i + 1})
	}
	return out
}
