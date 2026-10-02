package tracker

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Structured questions live in the issue body under `## Questions` so the
// issue file is the mailbox: an answer survives even when the asking agent is
// gone. Answering one turns it into a one-line pointer and records the choice
// as a `D-n` line under `## Decisions`.
//
//	## Questions
//
//	### Q-3 · open · 2026-10-02 14:05
//	Does unspent gold carry over between stages?
//
//	At PROBE_N=200 teams bank about 140 gold at a stage lock.
//
//	- **A** Reset to the stage's starting gold — Simplest.
//	- **B** (recommended) Carry over, capped at 300 — Rewards saving.
//
//	Links: docs/economy.md
//
// The decision line format is a contract for the generated decisions index:
//
//	- **D-7** · 2026-10-02 · answered in inbox (Q-3): <question> → **B. <label>**, "<note>" (also considered: A <label>; C <label>)

const (
	QuestionsSection = "Questions"
	DecisionsSection = "Decisions"

	QuestionOpen     = "open"
	QuestionAnswered = "answered"

	questionTimeLayout = "2006-01-02 15:04"
	optionSeparator    = " — "
)

// ErrQuestionNotOpen is returned when answering a question that does not exist
// or was already answered.
var ErrQuestionNotOpen = errors.New("question is not open")

// QuestionOption is one choice offered by a structured question.
type QuestionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Tradeoff    string `json:"tradeoff,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

// Question is a structured question parsed from an issue's `## Questions`.
type Question struct {
	ID       string           `json:"id"` // "Q-3"
	State    string           `json:"state"`
	AskedAt  time.Time        `json:"asked_at,omitempty"`
	Text     string           `json:"text"`
	Context  string           `json:"context,omitempty"`
	Options  []QuestionOption `json:"options,omitempty"`
	Links    []string         `json:"links,omitempty"`
	Answer   string           `json:"answer,omitempty"`   // option id, answered only
	Decision string           `json:"decision,omitempty"` // "D-7", answered only
}

// NewQuestion is the input for AddQuestion.
type NewQuestion struct {
	Text    string
	Context string
	Options []QuestionOption
	Links   []string
}

var (
	questionHeadingRe = regexp.MustCompile(`^###\s+(Q-(\d+))\s+·\s+(open|answered)\b(.*)$`)
	questionIDRe      = regexp.MustCompile(`(?m)^###\s+Q-(\d+)\b`)
	decisionIDRe      = regexp.MustCompile(`\*\*D-(\d+)\*\*`)
	optionLineRe      = regexp.MustCompile(`^- \*\*([A-Z])\*\*( \(recommended\))? (.*)$`)
	answeredTailRe    = regexp.MustCompile(`→\s+([A-Z])\s+·\s+(D-\d+)`)
)

// ParseOptionSpec parses an `--option` value of the form "A=Label|Tradeoff".
func ParseOptionSpec(spec string) (QuestionOption, error) {
	id, rest, ok := strings.Cut(spec, "=")
	id = strings.ToUpper(strings.TrimSpace(id))
	if !ok || len(id) != 1 || id[0] < 'A' || id[0] > 'Z' {
		return QuestionOption{}, fmt.Errorf("option %q must look like \"A=Label|Tradeoff\" (id is one letter A–Z)", spec)
	}
	label, tradeoff, _ := strings.Cut(rest, "|")
	label = oneLine(label)
	if label == "" {
		return QuestionOption{}, fmt.Errorf("option %s has an empty label", id)
	}
	return QuestionOption{ID: id, Label: label, Tradeoff: oneLine(tradeoff)}, nil
}

// ValidateNewQuestion checks a question before it is written.
func ValidateNewQuestion(q NewQuestion) error {
	if oneLine(q.Text) == "" {
		return fmt.Errorf("question text is required")
	}
	if len(q.Options) < 2 {
		return fmt.Errorf("a question needs at least 2 options, got %d", len(q.Options))
	}
	seen := map[string]bool{}
	recommended := 0
	for _, o := range q.Options {
		if seen[o.ID] {
			return fmt.Errorf("duplicate option id %s", o.ID)
		}
		seen[o.ID] = true
		if o.Recommended {
			recommended++
		}
	}
	if recommended > 1 {
		return fmt.Errorf("only one option can be recommended")
	}
	return nil
}

// AddQuestion appends a new open question to the issue's `## Questions`
// section and returns its id.
func AddQuestion(filePath string, q NewQuestion, now time.Time) (string, error) {
	if err := ValidateNewQuestion(q); err != nil {
		return "", err
	}
	var id string
	_, _, err := UpdateIssueBody(filePath, func(body string) (string, bool, error) {
		id = fmt.Sprintf("Q-%d", nextNumber(questionIDRe, body))
		return appendToSectionTight(body, QuestionsSection, renderQuestion(id, q, now), true), true, nil
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func renderQuestion(id string, q NewQuestion, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s · %s · %s\n", id, QuestionOpen, now.Format(questionTimeLayout))
	b.WriteString(oneLine(q.Text) + "\n")
	if ctx := strings.TrimSpace(q.Context); ctx != "" {
		b.WriteString("\n" + ctx + "\n")
	}
	b.WriteString("\n")
	for _, o := range q.Options {
		rec := ""
		if o.Recommended {
			rec = " (recommended)"
		}
		line := fmt.Sprintf("- **%s**%s %s", o.ID, rec, o.Label)
		if o.Tradeoff != "" {
			line += optionSeparator + o.Tradeoff
		}
		b.WriteString(line + "\n")
	}
	if len(q.Links) > 0 {
		b.WriteString("\nLinks: " + strings.Join(q.Links, ", ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ParseQuestions returns every question under `## Questions`, open and
// answered, in file order.
func ParseQuestions(body string) []Question {
	sec, ok := questionsSection(body)
	if !ok {
		return nil
	}
	all := strings.Split(body, "\n")
	fence := computeFenceFlags(all)
	var out []Question
	var cur *Question
	var block []string
	flush := func() {
		if cur != nil {
			parseQuestionBlock(cur, block)
			out = append(out, *cur)
		}
	}
	for i := sec.StartLine + 1; i < sec.EndLine; i++ {
		line := all[i]
		if m := questionHeadingRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil && !fence[i] {
			flush()
			cur = &Question{ID: m[1], State: m[3]}
			parseQuestionTail(cur, m[4])
			block = nil
			continue
		}
		if cur != nil {
			block = append(block, line)
		}
	}
	flush()
	return out
}

// OpenQuestions returns only the questions still waiting for an answer.
func OpenQuestions(body string) []Question {
	var open []Question
	for _, q := range ParseQuestions(body) {
		if q.State == QuestionOpen {
			open = append(open, q)
		}
	}
	return open
}

// FindQuestion returns the question with id (case-insensitive), or nil.
func FindQuestion(body, id string) *Question {
	for _, q := range ParseQuestions(body) {
		if strings.EqualFold(q.ID, id) {
			q := q
			return &q
		}
	}
	return nil
}

func parseQuestionTail(q *Question, tail string) {
	tail = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tail), "·"))
	if q.State == QuestionAnswered {
		if m := answeredTailRe.FindStringSubmatch(tail); m != nil {
			q.Answer, q.Decision = m[1], m[2]
		}
		tail, _, _ = strings.Cut(tail, "→")
	}
	if t, err := time.ParseInLocation(questionTimeLayout, strings.TrimSpace(tail), time.Local); err == nil {
		q.AskedAt = t
	}
}

func parseQuestionBlock(q *Question, lines []string) {
	var context []string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			if q.Text != "" && len(q.Options) == 0 {
				context = append(context, "")
			}
		case q.Text == "":
			q.Text = line
		case optionLineRe.MatchString(line):
			m := optionLineRe.FindStringSubmatch(line)
			label, tradeoff, _ := strings.Cut(m[3], optionSeparator)
			q.Options = append(q.Options, QuestionOption{
				ID: m[1], Label: strings.TrimSpace(label), Tradeoff: strings.TrimSpace(tradeoff), Recommended: m[2] != "",
			})
		case strings.HasPrefix(line, "Links:"):
			for _, l := range strings.Split(strings.TrimPrefix(line, "Links:"), ",") {
				if l = strings.TrimSpace(l); l != "" {
					q.Links = append(q.Links, l)
				}
			}
		case len(q.Options) == 0:
			context = append(context, raw)
		}
	}
	q.Context = strings.TrimSpace(strings.Join(context, "\n"))
}

// AnswerResult describes a recorded answer.
type AnswerResult struct {
	QuestionID   string `json:"question_id"`
	DecisionID   string `json:"decision_id"`
	DecisionLine string `json:"decision_line"`
}

// AnswerQuestion records the answer to an open question in one locked file
// write: the question collapses to a one-line pointer and a `D-n` line is
// appended to `## Decisions`. Returns ErrQuestionNotOpen when the question is
// missing or already answered, and clears any pending question wait.
func AnswerQuestion(filePath, questionID, optionID, note string, now time.Time) (*AnswerResult, error) {
	var res *AnswerResult
	_, _, err := UpdateIssueBody(filePath, func(body string) (string, bool, error) {
		q := FindQuestion(body, questionID)
		if q == nil || q.State != QuestionOpen {
			return body, false, fmt.Errorf("%s: %w", questionID, ErrQuestionNotOpen)
		}
		var chosen *QuestionOption
		var others []string
		for i := range q.Options {
			o := q.Options[i]
			if strings.EqualFold(o.ID, optionID) {
				chosen = &q.Options[i]
				continue
			}
			others = append(others, o.ID+" "+o.Label)
		}
		if chosen == nil {
			return body, false, fmt.Errorf("%s has no option %q", q.ID, optionID)
		}

		decisionID := fmt.Sprintf("D-%d", nextNumber(decisionIDRe, body))
		line := fmt.Sprintf("- **%s** · %s · answered in inbox (%s): %s → **%s. %s**",
			decisionID, now.Format("2006-01-02"), q.ID, q.Text, chosen.ID, chosen.Label)
		if n := oneLine(note); n != "" {
			line += fmt.Sprintf(", %q", n)
		}
		if len(others) > 0 {
			line += " (also considered: " + strings.Join(others, "; ") + ")"
		}

		pointer := fmt.Sprintf("### %s · %s %s → %s · %s\n%s",
			q.ID, QuestionAnswered, now.Format(questionTimeLayout), chosen.ID, decisionID, q.Text)
		next, ok := replaceQuestionBlock(body, q.ID, pointer)
		if !ok {
			return body, false, fmt.Errorf("%s: %w", questionID, ErrQuestionNotOpen)
		}
		next = appendToSectionTight(next, DecisionsSection, line, false)
		res = &AnswerResult{QuestionID: q.ID, DecisionID: decisionID, DecisionLine: line}
		return next, true, nil
	})
	if err != nil {
		return nil, err
	}
	if err := EndQuestionWait(filePath, res.QuestionID); err != nil {
		return res, fmt.Errorf("answer saved, clearing wait: %w", err)
	}
	return res, nil
}

// replaceQuestionBlock swaps the `### Q-n` block (heading through the line
// before the next heading of level ≤ 3) for replacement.
func replaceQuestionBlock(body, id, replacement string) (string, bool) {
	sec, ok := questionsSection(body)
	if !ok {
		return body, false
	}
	lines := strings.Split(body, "\n")
	fence := computeFenceFlags(lines)
	start := -1
	for i := sec.StartLine + 1; i < sec.EndLine; i++ {
		if fence[i] {
			continue
		}
		if m := questionHeadingRe.FindStringSubmatch(strings.TrimSpace(lines[i])); m != nil && strings.EqualFold(m[1], id) {
			start = i
			break
		}
	}
	if start == -1 {
		return body, false
	}
	end := sec.EndLine
	for j := start + 1; j < sec.EndLine; j++ {
		if fence[j] {
			continue
		}
		if level, _, ok := parseHeadingLine(lines[j]); ok && level <= 3 {
			end = j
			break
		}
	}
	out := append([]string(nil), lines[:start]...)
	out = append(out, strings.Split(replacement, "\n")...)
	if end < len(lines) {
		out = append(out, "")
	}
	out = append(out, lines[end:]...)
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n", true
}

// questionsSection is the first level-2 `## Questions` heading.
func questionsSection(body string) (headingMatch, bool) {
	for _, m := range findHeadingMatches(body, QuestionsSection) {
		if m.Level == 2 {
			return m, true
		}
	}
	return headingMatch{}, false
}

// appendToSectionTight appends content at the end of the first level-2
// section titled title, creating it at the end of the body when missing.
// Blocks (questions) are separated by a blank line; list items (decisions)
// are kept as a tight list.
func appendToSectionTight(body, title, content string, blankLine bool) string {
	var sec *headingMatch
	for _, m := range findHeadingMatches(body, title) {
		if m.Level == 2 {
			m := m
			sec = &m
			break
		}
	}
	if sec == nil {
		body = strings.TrimRight(body, "\n")
		if body != "" {
			body += "\n\n"
		}
		return body + "## " + title + "\n\n" + content + "\n"
	}
	lines := strings.Split(body, "\n")
	end := sec.EndLine
	for end > sec.StartLine+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	sep := []string{}
	if end == sec.StartLine+1 || blankLine {
		sep = []string{""}
	}
	out := append([]string(nil), lines[:end]...)
	out = append(out, sep...)
	out = append(out, strings.Split(content, "\n")...)
	if sec.EndLine < len(lines) {
		out = append(out, "")
	}
	out = append(out, lines[sec.EndLine:]...)
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

func nextNumber(re *regexp.Regexp, body string) int {
	max := 0
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > max {
			max = n
		}
	}
	return max + 1
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
