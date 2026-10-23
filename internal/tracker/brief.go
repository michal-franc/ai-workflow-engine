package tracker

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// BriefMax is the longest brief, in runes, before it is cut at a sentence end.
const BriefMax = 280

// briefLabelRe matches the "**In short:**" label that opens a brief paragraph.
var briefLabelRe = regexp.MustCompile(`(?i)^\*\*in short:?\*\*:?\s*`)

// Brief is the one-paragraph summary the issue page's Issue tab leads with
// and `issue-cli show` prints, as markdown. It reads, in order: a paragraph
// opening with "**In short:**", the first line of "## Bug", the body's
// first prose paragraph before any "## " heading, then the first prose
// paragraph of the first section. "" when the body has none of these.
func Brief(body string) string {
	paras := proseParagraphs(body)
	for _, p := range paras {
		if briefLabelRe.MatchString(p.text) {
			return cutBrief(capitalize(briefLabelRe.ReplaceAllString(p.text, "")))
		}
	}
	if bug, ok := SectionText(body, "Bug"); ok {
		for _, line := range strings.Split(bug, "\n") {
			if line = strings.TrimSpace(line); line != "" && isProse(line) {
				return cutBrief(line)
			}
		}
	}
	for _, p := range paras {
		if p.section == "" {
			return cutBrief(p.text)
		}
	}
	if len(paras) > 0 {
		return cutBrief(paras[0].text)
	}
	return ""
}

// Lead is the first prose paragraph of markdown as plain text, cut to max
// runes the way Brief is: the Issue tab's folded section rows show it.
func Lead(markdown string, max int) string {
	for _, p := range proseParagraphs(markdown) {
		return cutText(capitalize(plainInline(briefLabelRe.ReplaceAllString(p.text, ""))), max)
	}
	return ""
}

var (
	inlineLinkRe   = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	inlineMarkerRe = regexp.MustCompile("\\*\\*|__|`")
)

// plainInline drops inline markdown: emphasis markers, code ticks, and link
// targets (the link text stays).
func plainInline(s string) string {
	s = inlineLinkRe.ReplaceAllString(s, "$1")
	return inlineMarkerRe.ReplaceAllString(s, "")
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 || !unicode.IsLower(r) {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

type briefPara struct {
	section string // the "## " heading above it, "" before the first
	text    string
}

// proseParagraphs returns the body's prose paragraphs, lines joined with a
// space, skipping headings, fenced code, tables, lists, quotes, images and
// HTML comments.
func proseParagraphs(body string) []briefPara {
	lines := strings.Split(body, "\n")
	fence := computeFenceFlags(lines)
	var out []briefPara
	var cur []string
	section := ""
	flush := func() {
		if len(cur) > 0 {
			out = append(out, briefPara{section: section, text: strings.Join(cur, " ")})
			cur = nil
		}
	}
	inComment := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if inComment {
			if strings.Contains(t, "-->") {
				inComment = false
			}
			continue
		}
		if fence[i] || t == "" {
			flush()
			continue
		}
		if strings.HasPrefix(t, "<!--") {
			flush()
			inComment = !strings.Contains(t, "-->")
			continue
		}
		if level, title, ok := parseHeadingLine(t); ok {
			flush()
			if level <= 2 {
				section = title
			}
			continue
		}
		if !isProse(t) {
			flush()
			continue
		}
		cur = append(cur, t)
	}
	flush()
	return out
}

// isProse reports whether a trimmed line reads as running text rather than
// markdown structure.
func isProse(t string) bool {
	for _, p := range []string{"|", "#", ">", "<", "![", "- ", "* ", "+ ", "---", "***", "```", "~~~"} {
		if strings.HasPrefix(t, p) {
			return false
		}
	}
	if i := strings.IndexAny(t, ".)"); i > 0 && i <= 3 && strings.Trim(t[:i], "0123456789") == "" {
		return false // "1. item"
	}
	return true
}

func cutBrief(text string) string { return cutText(text, BriefMax) }

// cutText keeps text up to max runes: past that it ends at the last
// sentence end inside the limit, else at a word with "…".
func cutText(text string, max int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	r := []rune(text)
	head := string(r[:max])
	best := -1
	for _, end := range []string{". ", "! ", "? "} {
		if i := strings.LastIndex(head, end); i > best {
			best = i
		}
	}
	if best >= max/4 {
		return strings.TrimSpace(head[:best+1])
	}
	if i := strings.LastIndex(head, " "); i > 0 {
		head = head[:i]
	}
	return strings.TrimRight(head, " ,;") + "…"
}
