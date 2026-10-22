package tracker

import (
	"fmt"
	"regexp"
	"strings"
)

type headingMatch struct {
	StartLine int
	EndLine   int
	Level     int
	Line      string
	Key       string
}

func normalizeHeading(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if strings.HasPrefix(title, "#") {
		return title
	}
	return "## " + title
}

func parseHeadingLine(line string) (level int, title string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i >= len(trimmed) || trimmed[i] != ' ' {
		return 0, "", false
	}
	title = strings.TrimSpace(trimmed[i+1:])
	title = strings.TrimSpace(strings.TrimRight(title, "#"))
	if title == "" {
		return 0, "", false
	}
	return i, title, true
}

func normalizeHeadingKey(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if _, parsed, ok := parseHeadingLine(title); ok {
		title = parsed
	}
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

// computeFenceFlags returns, for each line index, whether the line sits
// inside a fenced code block (or is the fence opener/closer itself). Fence
// detection follows CommonMark rules loosely: a line is a fence if, after
// stripping leading whitespace, it starts with ``` or ~~~, and a closing
// fence must use the same marker that opened the block.
func computeFenceFlags(lines []string) []bool {
	flags := make([]bool, len(lines))
	var fence string
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if fence == "" {
			switch {
			case strings.HasPrefix(trimmed, "```"):
				fence = "```"
				flags[i] = true
			case strings.HasPrefix(trimmed, "~~~"):
				fence = "~~~"
				flags[i] = true
			}
			continue
		}
		flags[i] = true
		if strings.HasPrefix(trimmed, fence) {
			fence = ""
		}
	}
	return flags
}

func findHeadingMatches(body, title string) []headingMatch {
	lines := strings.Split(body, "\n")
	key := normalizeHeadingKey(title)
	if key == "" {
		return nil
	}

	fenceFlags := computeFenceFlags(lines)

	var matches []headingMatch
	for i, line := range lines {
		if fenceFlags[i] {
			continue
		}
		level, parsedTitle, ok := parseHeadingLine(line)
		if !ok || normalizeHeadingKey(parsedTitle) != key {
			continue
		}

		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if fenceFlags[j] {
				continue
			}
			nextLevel, _, ok := parseHeadingLine(lines[j])
			if ok && nextLevel <= level {
				end = j
				break
			}
		}

		matches = append(matches, headingMatch{
			StartLine: i,
			EndLine:   end,
			Level:     level,
			Line:      strings.TrimSpace(line),
			Key:       key,
		})
	}
	return matches
}

func findAllHeadings(body string) []headingMatch {
	lines := strings.Split(body, "\n")
	fenceFlags := computeFenceFlags(lines)
	var matches []headingMatch
	for i, line := range lines {
		if fenceFlags[i] {
			continue
		}
		level, title, ok := parseHeadingLine(line)
		if !ok {
			continue
		}
		matches = append(matches, headingMatch{
			StartLine: i,
			Level:     level,
			Line:      strings.TrimSpace(line),
			Key:       normalizeHeadingKey(title),
		})
	}
	return matches
}

// ReplaceSectionLine replaces one line of a section's body with text. The
// line is chosen by its 1-based number counted from the line after the
// heading, blank lines included (line > 0), or as the one line re matches
// (re != nil). It returns the new body and the line it replaced. When the
// line can't be chosen (out of range, no match, several matches) the error
// lists the section's lines with their numbers so the caller can retry.
func ReplaceSectionLine(body, section string, line int, re *regexp.Regexp, text string) (string, string, error) {
	if strings.Contains(text, "\n") {
		return body, "", fmt.Errorf("the replacement must be a single line (use replace --section without --line/--match for more)")
	}
	matches := findHeadingMatches(body, section)
	if len(matches) == 0 {
		return body, "", fmt.Errorf("no section matching %q in issue body", section)
	}
	if len(matches) > 1 {
		return body, "", fmt.Errorf("multiple matching sections for %q: rename one so the line is unambiguous", section)
	}
	m := matches[0]
	lines := strings.Split(body, "\n")
	content := lines[m.StartLine+1 : m.EndLine]
	idx := -1
	if re != nil {
		var hits []int
		for i, l := range content {
			if re.MatchString(l) {
				hits = append(hits, i)
			}
		}
		switch len(hits) {
		case 1:
			idx = hits[0]
		case 0:
			return body, "", fmt.Errorf("--match %q matches no line in section %q:\n%s", re.String(), section, numberLines(content, nil))
		default:
			return body, "", fmt.Errorf("--match %q matches %d lines in section %q; narrow it or pass --line:\n%s", re.String(), len(hits), section, numberLines(content, hits))
		}
	} else {
		if line < 1 || line > len(content) {
			return body, "", fmt.Errorf("section %q has %d lines, no line %d:\n%s", section, len(content), line, numberLines(content, nil))
		}
		idx = line - 1
	}
	old := content[idx]
	lines[m.StartLine+1+idx] = text
	return strings.Join(lines, "\n"), old, nil
}

// numberLines renders lines as "  N | text" for an error message; when only
// is non-nil, just those indexes are shown.
func numberLines(lines []string, only []int) string {
	var b strings.Builder
	show := func(i int) { fmt.Fprintf(&b, "  %3d | %s\n", i+1, lines[i]) }
	if only != nil {
		for _, i := range only {
			show(i)
		}
	} else {
		for i := range lines {
			show(i)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func appendContentToMatch(body string, match headingMatch, content string) (string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return body, false
	}

	lines := strings.Split(body, "\n")
	sectionLines := append([]string(nil), lines[match.StartLine:match.EndLine]...)
	sectionBody := strings.TrimSpace(strings.Join(sectionLines[1:], "\n"))
	if sectionBody != "" {
		if strings.Contains(sectionBody, content) {
			return body, false
		}
		sectionBody = strings.TrimRight(sectionBody, "\n") + "\n\n" + content
	} else {
		sectionBody = content
	}

	replacement := []string{strings.TrimRight(sectionLines[0], "\n"), sectionBody}
	newLines := append([]string(nil), lines[:match.StartLine]...)
	newLines = append(newLines, replacement...)
	newLines = append(newLines, lines[match.EndLine:]...)
	return strings.TrimRight(strings.Join(newLines, "\n"), "\n") + "\n", true
}

func replaceContentInMatch(body string, match headingMatch, content string) (string, bool) {
	lines := strings.Split(body, "\n")
	sectionLines := lines[match.StartLine:match.EndLine]
	heading := sectionLines[0]
	contentLines := sectionLines[1:]

	trailing := 0
	for trailing < len(contentLines) && strings.TrimSpace(contentLines[len(contentLines)-1-trailing]) == "" {
		trailing++
	}

	currentBody := strings.TrimSpace(strings.Join(contentLines, "\n"))
	newBody := strings.TrimSpace(content)
	if newBody == currentBody {
		return body, false
	}

	replacement := []string{heading}
	if newBody != "" {
		replacement = append(replacement, strings.Split(newBody, "\n")...)
	}
	for i := 0; i < trailing; i++ {
		replacement = append(replacement, "")
	}

	newLines := append([]string(nil), lines[:match.StartLine]...)
	newLines = append(newLines, replacement...)
	newLines = append(newLines, lines[match.EndLine:]...)
	return strings.TrimRight(strings.Join(newLines, "\n"), "\n") + "\n", true
}

func appendToSection(body, title, content string) (string, bool) {
	heading := normalizeHeading(title)
	content = strings.TrimSpace(content)
	if heading == "" || content == "" {
		return body, false
	}

	matches := findHeadingMatches(body, title)
	switch len(matches) {
	case 0:
		body = strings.TrimRight(body, "\n")
		if body != "" {
			body += "\n\n"
		}
		body += heading + "\n" + content + "\n"
		return body, true
	default:
		return body, false
	}
}

// SectionText returns the content under the first heading named title (a bare
// title is a "## " heading), without the heading line, up to the next heading
// of the same or a higher level. ok is false when there is no such heading.
func SectionText(body, title string) (text string, ok bool) {
	matches := findHeadingMatches(body, normalizeHeading(title))
	if len(matches) == 0 {
		return "", false
	}
	lines := strings.Split(body, "\n")
	m := matches[0]
	return strings.TrimSpace(strings.Join(lines[m.StartLine+1:m.EndLine], "\n")), true
}

// MarkdownSection is one level-2 section of a markdown document.
type MarkdownSection struct {
	Title string // heading text, without the "## "
	Text  string // content up to the next level-1 or level-2 heading
}

// SplitSections splits body on its "## " headings, ignoring fenced code.
// Text before the first one is not returned.
func SplitSections(body string) []MarkdownSection {
	lines := strings.Split(body, "\n")
	fence := computeFenceFlags(lines)
	var out []MarkdownSection
	cur := -1
	start := 0
	flush := func(end int) {
		if cur >= 0 {
			out[cur].Text = strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		}
	}
	for i, line := range lines {
		if fence[i] {
			continue
		}
		level, title, ok := parseHeadingLine(line)
		if !ok || level > 2 {
			continue
		}
		flush(i)
		cur = -1
		if level == 2 {
			out = append(out, MarkdownSection{Title: title})
			cur = len(out) - 1
			start = i + 1
		}
	}
	flush(len(lines))
	return out
}
