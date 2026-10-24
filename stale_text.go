package main

import (
	"regexp"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

// StaleLine is a body line that still says Open, pending or TBD after the
// design, and the answer that likely settled it (D-2 of
// ui/evidence-tab-proof-shots-and-criteria).
type StaleLine struct {
	Section string
	Text    string
	// Answer names the best match ("D-1", "Clarifications: Version"); empty
	// when nothing shares enough words. AnswerText is its words, Anchor the
	// body anchor it links to.
	Answer     string
	AnswerText string
	Anchor     string
	// Quote is a decision's text, cut short, shown after its D-n.
	Quote string
}

var (
	staleHeadingRe  = regexp.MustCompile(`(?i)^open questions?\b`)
	staleLeadRe     = regexp.MustCompile(`(?i)^(\*\*)?open( question)?s?\s*(\*\*)?:`)
	stalePendingRe  = regexp.MustCompile(`(?i)(:\s*pending\b|\(pending\)|\bpending\)|\bTBD\b)`)
	staleCitesRe    = regexp.MustCompile(`\bD-\d+\b`)
	staleCodeSpanRe = regexp.MustCompile("`[^`]*`")
	staleWordRe     = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9_-]{3,}`)
	headingRe       = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*$`)
	boldLeadRe      = regexp.MustCompile(`^\*\*([^*]+?)[:.]?\*\*`)
	listMarkRe      = regexp.MustCompile(`^([-*+]|\d+[.)])\s+`)
	answeredRe      = regexp.MustCompile(`(?i)\banswered\b`)
)

// staleSkipSections are the level-2 sections whose lines are never stale:
// the decisions themselves, criteria and test lists, and the late statuses.
var staleSkipSections = map[string]bool{
	"decisions": true, "acceptance criteria": true, "test plan": true, "testing": true,
	"human testing": true, "documentation": true, "shipping": true, "comments": true,
}

// staleStopwords are words too common to tie a line to an answer.
var staleStopwords = map[string]bool{
	"that": true, "this": true, "with": true, "from": true, "have": true, "what": true, "when": true,
	"where": true, "which": true, "should": true, "would": true, "could": true, "does": true,
	"there": true, "their": true, "them": true, "then": true, "than": true, "into": true, "only": true,
	"also": true, "each": true, "every": true, "some": true, "more": true, "most": true, "over": true,
	"such": true, "will": true, "they": true, "were": true, "been": true, "being": true, "about": true,
	"open": true, "question": true, "questions": true, "pending": true, "issue": true, "owner": true,
	"option": true, "other": true, "same": true, "just": true, "like": true, "make": true, "need": true,
}

// staleMinShared is how many distinctive words a line and an answer must
// share to match: two matched loosely related bullets on real issues.
const staleMinShared = 3

// staleLines lists the body's stale lines. decisions and the Clarifications
// bullets are the answers; a match needs staleMinShared shared words.
func staleLines(body string, decisions []tracker.Decision) []StaleLine {
	type answer struct {
		name, text, anchor string
		words              map[string]bool
		order              int
	}
	var answers []answer
	for i, d := range decisions {
		text := d.Text
		if d.Quote != "" {
			text += " " + d.Quote
		}
		answers = append(answers, answer{name: d.ID, text: d.Text, anchor: "decisions", words: staleWords(text), order: i})
	}

	var out []StaleLine
	section, sub := "", ""
	inOpen, inClar, inFence := false, false, false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			title := strings.TrimSpace(m[2])
			if len(m[1]) <= 2 {
				section, sub = title, ""
			} else {
				sub = title
			}
			inOpen = staleHeadingRe.MatchString(title)
			inClar = strings.Contains(strings.ToLower(title), "clarification")
			continue
		}
		if line == "" || strings.HasPrefix(line, "<!--") {
			continue
		}
		item := strings.TrimSpace(listMarkRe.ReplaceAllString(line, ""))
		if inClar && item != "" {
			name := "Clarifications"
			if m := boldLeadRe.FindStringSubmatch(item); m != nil {
				name += ": " + strings.TrimSpace(m[1])
			}
			answers = append(answers, answer{name: name, text: item, anchor: tracker.Slugify(sectionOr(sub, section)), words: staleWords(item), order: len(answers)})
			continue
		}
		if staleSkipSections[strings.ToLower(section)] || isCheckboxLine(line) ||
			strings.Contains(line, "~~") || staleCitesRe.MatchString(line) || answeredRe.MatchString(line) {
			continue
		}
		plain := staleCodeSpanRe.ReplaceAllString(item, "")
		if !inOpen && !staleLeadRe.MatchString(plain) && !stalePendingRe.MatchString(plain) {
			continue
		}
		where := section
		if inOpen {
			where = sectionOr(sub, section)
		}
		out = append(out, StaleLine{Section: where, Text: item})
	}

	for i := range out {
		words := staleWords(out[i].Text)
		best, bestN := -1, staleMinShared-1
		for j, a := range answers {
			n := 0
			for w := range words {
				if a.words[w] {
					n++
				}
			}
			if n > bestN || (n == bestN && best >= 0 && a.order > answers[best].order) {
				best, bestN = j, n
			}
		}
		if best >= 0 {
			a := answers[best]
			out[i].Answer, out[i].AnswerText, out[i].Anchor = a.name, a.text, a.anchor
			if strings.HasPrefix(a.name, "D-") {
				out[i].Quote = truncate(a.text, 90)
			}
		}
	}
	return out
}

func sectionOr(sub, section string) string {
	if sub != "" {
		return sub
	}
	return section
}

func isCheckboxLine(line string) bool {
	l := strings.TrimLeft(line, "-*+ ")
	return strings.HasPrefix(l, "[ ]") || strings.HasPrefix(strings.ToLower(l), "[x]")
}

// staleWords are a text's distinctive words, lowercased and crudely stemmed.
func staleWords(text string) map[string]bool {
	words := map[string]bool{}
	for _, w := range staleWordRe.FindAllString(strings.ToLower(text), -1) {
		w = strings.Trim(w, "-_")
		if len(w) < 4 || staleStopwords[w] {
			continue
		}
		words[stem(w)] = true
	}
	return words
}

func stem(w string) string {
	for _, suf := range []string{"ing", "ed", "es", "s"} {
		if strings.HasSuffix(w, suf) && len(w)-len(suf) >= 4 {
			return w[:len(w)-len(suf)]
		}
	}
	return w
}
