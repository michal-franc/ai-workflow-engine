package validations

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"
)

// fail builds a "<problem> — <hint>" error, applying templating + the
// optional per-action hint override.
func fail(action Action, issue *IssueView, problem, defaultHint string) error {
	hint := defaultHint
	if h := strings.TrimSpace(action.Hint); h != "" {
		if rendered, err := renderTemplate(h, issue); err == nil {
			hint = rendered
		} else {
			hint = h
		}
	}
	if hint == "" {
		return fmt.Errorf("%s", problem)
	}
	return fmt.Errorf("%s — %s", problem, hint)
}

// renderTemplate applies text/template substitution with {{slug}}, {{number}},
// {{repo}}, {{system}} as zero-arg template funcs (so the no-dot syntax works).
func renderTemplate(s string, issue *IssueView) (string, error) {
	funcs := template.FuncMap{
		"slug":   func() string { return issue.Slug },
		"number": func() int { return issue.Number },
		"repo":   func() string { return issue.Repo },
		"system": func() string { return issue.System },
	}
	tmpl, err := template.New("v").Funcs(funcs).Parse(s)
	if err != nil {
		return s, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		return s, err
	}
	return buf.String(), nil
}

// findSectionContent returns the body of a "## <name>" section
// (case-insensitive title match), exclusive of the heading line, terminating
// at the next "## " heading or end of body. Returns ok=false when no such
// heading exists.
func findSectionContent(body, name string) (string, bool) {
	want := strings.TrimSpace(strings.ToLower(name))
	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "## ") && strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(t, "##")), want) {
			start = i
			break
		}
	}
	if start == -1 {
		return "", false
	}
	end := len(lines)
	for j := start + 1; j < len(lines); j++ {
		if strings.HasPrefix(strings.TrimSpace(lines[j]), "## ") {
			end = j
			break
		}
	}
	return strings.Join(lines[start+1:end], "\n"), true
}

// scrubbedEnv returns the env allowlist used by command_succeeds.
func scrubbedEnv() []string {
	allow := []string{"PATH", "HOME", "GH_TOKEN"}
	out := make([]string, 0, len(allow))
	for _, k := range allow {
		if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
