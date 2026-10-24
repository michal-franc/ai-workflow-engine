package tracker

import (
	"fmt"
	"strconv"
	"strings"
)

const approvalPrefix = "approval: "

// ApprovalWords is the owner's words saved with an approval: what they said,
// how it reached the issue (viewer, in the lead chat) and the statuses it
// approved. Statuses is empty for comments written before the → suffix.
type ApprovalWords struct {
	Words    string
	How      string
	Date     string
	Statuses []string
}

// ApprovalComment is the comment text that saves the owner's words with an
// approval, shared by the viewer's approve button and issue-cli approve:
//
//	approval: "<words>" (<how>, <date>) → <status>[, <status>]
func ApprovalComment(words, how, date string, statuses ...string) string {
	s := fmt.Sprintf("%s%q (%s, %s)", approvalPrefix, words, how, date)
	if len(statuses) > 0 {
		s += " → " + strings.Join(statuses, ", ")
	}
	return s
}

// DispatchComment is the comment issue-cli dispatch leaves with the human's
// words that started (or re-prompted) the issue's worker, the same shape as
// an approval:
//
//	dispatch: "<words>" (<how>, <date>)
func DispatchComment(words, how, date string) string {
	return fmt.Sprintf("dispatch: %q (%s, %s)", words, how, date)
}

// ParseApprovalComment reads a comment written by ApprovalComment. It also
// takes the older form without "→ <status>" and a trailing
// " — recorded by <name>"; ok is false for any other comment.
func ParseApprovalComment(text string) (ApprovalWords, bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(text), approvalPrefix)
	if !found {
		return ApprovalWords{}, false
	}
	quoted, err := strconv.QuotedPrefix(rest)
	if err != nil {
		return ApprovalWords{}, false
	}
	words, err := strconv.Unquote(quoted)
	if err != nil {
		return ApprovalWords{}, false
	}
	a := ApprovalWords{Words: words}
	rest = strings.TrimSpace(rest[len(quoted):])
	rest, _, _ = strings.Cut(rest, " — recorded by ")
	if inner, after, ok := strings.Cut(strings.TrimPrefix(rest, "("), ")"); ok && strings.HasPrefix(rest, "(") {
		if i := strings.LastIndex(inner, ", "); i >= 0 {
			a.How, a.Date = inner[:i], inner[i+2:]
		} else {
			a.How = inner
		}
		rest = strings.TrimSpace(after)
	}
	if to, ok := strings.CutPrefix(rest, "→"); ok {
		for _, s := range strings.Split(to, ",") {
			if s = strings.TrimSpace(s); s != "" {
				a.Statuses = append(a.Statuses, s)
			}
		}
	}
	return a, true
}

// Approves reports whether these words were for an approval of status. Words
// that name no status (the older form) match any.
func (a ApprovalWords) Approves(status string) bool {
	if len(a.Statuses) == 0 {
		return true
	}
	for _, s := range a.Statuses {
		if strings.EqualFold(s, status) {
			return true
		}
	}
	return false
}
