package tracker

import "strings"

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
