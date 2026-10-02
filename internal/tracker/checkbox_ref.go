package tracker

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// A checkbox ref names one box in an issue body. Two forms are accepted:
//
//	short  D3, AC2, do1   section abbreviation + per-section index (case-insensitive)
//	long   Design#3       section name + "#" + per-section index (case-insensitive)
//
// Section abbreviations are the uppercased initials of the heading's words
// ("Acceptance Criteria" → "AC"). Sections claim abbreviations in the order
// they first hold a checkbox; a later section whose initials are already
// taken extends its first word ("Documentation" → "Do" when "Design" holds
// "D"). Workflow sections are appended, so an existing box's id is stable.

var (
	shortRefRe = regexp.MustCompile(`^([A-Za-z]+)(\d+)$`)
	longRefRe  = regexp.MustCompile(`^(.*\S)\s*#\s*(\d+)$`)
)

// RefStatus reports how a checkbox ref resolved against an issue's boxes.
type RefStatus int

const (
	// RefNotARef: the string is not shaped like a ref, or it names no
	// section that holds checkboxes. Callers may treat it as text.
	RefNotARef RefStatus = iota
	// RefNoSuchBox: the ref names a known section but no box has that index.
	RefNoSuchBox
	// RefFound: the ref resolved to a box.
	RefFound
)

// IsCheckboxRefShaped reports whether s has the syntax of a short or long
// checkbox ref, without checking it against any issue.
func IsCheckboxRefShaped(s string) bool {
	s = strings.TrimSpace(s)
	return shortRefRe.MatchString(s) || longRefRe.MatchString(s)
}

// ResolveCheckboxRef finds the box that ref names among items (as returned
// by ListCheckboxes).
func ResolveCheckboxRef(items []CheckboxItem, ref string) (CheckboxItem, RefStatus) {
	ref = strings.TrimSpace(ref)
	var match func(CheckboxItem) bool
	var index int
	if m := shortRefRe.FindStringSubmatch(ref); m != nil {
		index, _ = strconv.Atoi(m[2])
		match = func(it CheckboxItem) bool { return strings.EqualFold(idPrefix(it.ID), m[1]) }
	} else if m := longRefRe.FindStringSubmatch(ref); m != nil {
		index, _ = strconv.Atoi(m[2])
		match = func(it CheckboxItem) bool { return it.Section != "" && strings.EqualFold(it.Section, m[1]) }
	} else {
		return CheckboxItem{}, RefNotARef
	}
	known := false
	for _, it := range items {
		if it.ID == "" || !match(it) {
			continue
		}
		known = true
		if it.Index == index {
			return it, RefFound
		}
	}
	if known {
		return CheckboxItem{}, RefNoSuchBox
	}
	return CheckboxItem{}, RefNotARef
}

// CheckLines ticks the unchecked checkbox on each of the given 0-based body
// lines (CheckboxItem.Line) and returns the updated body. Lines that are not
// unchecked boxes are left alone.
func CheckLines(body string, lines []int) string {
	split := strings.Split(body, "\n")
	for _, n := range lines {
		if n >= 0 && n < len(split) {
			split[n] = strings.Replace(split[n], "- [ ]", "- [x]", 1)
		}
	}
	return strings.Join(split, "\n")
}

// idPrefix strips the trailing index from a short id ("AC12" → "AC").
func idPrefix(id string) string {
	return strings.TrimRightFunc(id, unicode.IsDigit)
}

// nextSectionAbbrev picks section's short-id prefix and records it in taken
// (keyed lowercase). Returns "" for the no-section bucket, a heading with no
// letter-led words, or when no unique abbreviation exists.
func nextSectionAbbrev(section string, taken map[string]bool) string {
	words := strings.FieldsFunc(section, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var initials []rune
	var first []rune
	for _, w := range words {
		r := []rune(w)
		if r[0] > unicode.MaxASCII || !unicode.IsLetter(r[0]) {
			continue
		}
		if first == nil {
			first = r
		}
		initials = append(initials, unicode.ToUpper(r[0]))
	}
	if len(initials) == 0 {
		return ""
	}
	rest := string(initials[1:])
	candidates := []string{string(initials)}
	// Extend the first word one letter at a time: "Documentation" → "Do", "Doc", …
	for k := 2; k <= len(first); k++ {
		c := first[k-1]
		if c > unicode.MaxASCII || !unicode.IsLetter(c) {
			break
		}
		candidates = append(candidates, string(unicode.ToUpper(first[0]))+strings.ToLower(string(first[1:k]))+rest)
	}
	for _, c := range candidates {
		if !taken[strings.ToLower(c)] {
			taken[strings.ToLower(c)] = true
			return c
		}
	}
	return ""
}
