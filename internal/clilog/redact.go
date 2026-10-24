package clilog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

// keptValueFlags are the flags whose values are structural (statuses,
// section names, ids, paths, numbers) and stay readable in the clilog. The
// value of any other flag is free text, or unknown, and is redacted.
var keptValueFlags = map[string]bool{
	"agent": true, "assignee": true, "body-file": true, "category": true,
	"ci": true, "config": true, "declined": true, "forget": true,
	"how": true, "index": true, "installed": true, "interval": true,
	"issue": true, "key": true, "line": true, "metric": true, "pass": true,
	"priority": true, "project": true, "replaces": true, "section": true,
	"since": true, "sort": true, "state": true, "status": true,
	"system": true, "template": true, "tier": true, "to": true, "top": true,
	"type": true, "version": true, "workflow": true,
}

// boolFlags are issue-cli's flags that take no value, so the token after one
// is a positional. TestBoolFlagsCoverCLI keeps the list in step with
// cmd/issue-cli. A missing entry only over-redacts the next positional.
var boolFlags = map[string]bool{
	"all": true, "attach": true, "clear": true, "debug": true,
	"deferred": true, "design": true, "detach": true, "dry-run": true,
	"force": true, "global": true, "headless": true, "help": true, "h": true,
	"if-untested": true, "import": true, "json": true, "lib": true, "list": true,
	"first": true, "md": true, "once": true, "quiet": true, "rename": true, "reprompt": true, "schema": true,
	"send": true, "spawn": true, "yes": true,
}

// maxKeptPositional is the longest positional kept verbatim: slugs, box ids
// and status names fit, prose does not.
const maxKeptPositional = 100

// Redact returns args with free text replaced by Placeholder: values of
// flags outside keptValueFlags (for --field, the part after "="), and
// positionals with whitespace or longer than maxKeptPositional. Commands,
// flag names, slugs, box ids and structural values stay as typed.
func Redact(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out = append(out, a)
			for _, rest := range args[i+1:] {
				out = append(out, redactPositional(rest))
			}
			break
		}
		name, ok := flagName(a)
		if !ok {
			out = append(out, redactPositional(a))
			continue
		}
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			out = append(out, a[:eq+1]+redactValue(name, a[eq+1:]))
			continue
		}
		out = append(out, a)
		// Like the flag package, a non-bool flag takes the next token as its
		// value even when it starts with "-".
		if boolFlags[name] || i+1 >= len(args) {
			continue
		}
		i++
		out = append(out, redactValue(name, args[i]))
	}
	return out
}

// Placeholder stands in for redacted text: its length in characters and the
// first 6 hex digits of its SHA-256, so identical calls still compare equal
// and a reader can match a call to the text saved in the issue.
func Placeholder(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("‹text n=%d h=%s›", utf8.RuneCountInString(s), hex.EncodeToString(sum[:3]))
}

// IsPlaceholder reports whether s is a Placeholder.
func IsPlaceholder(s string) bool {
	return strings.HasPrefix(s, "‹text n=") && strings.HasSuffix(s, "›")
}

func redactValue(flag, v string) string {
	if keptValueFlags[flag] || boolFlags[flag] || IsPlaceholder(v) {
		return v
	}
	if flag == "field" {
		if eq := strings.IndexByte(v, '='); eq > 0 {
			if IsPlaceholder(v[eq+1:]) {
				return v
			}
			return v[:eq+1] + Placeholder(v[eq+1:])
		}
	}
	return Placeholder(v)
}

func redactPositional(a string) string {
	if IsPlaceholder(a) {
		return a
	}
	if utf8.RuneCountInString(a) > maxKeptPositional || strings.ContainsAny(a, " \t\n\r") {
		return Placeholder(a)
	}
	return a
}

// flagName is a's flag name without dashes and value; ok is false for
// positionals, including "-", "-1" and text such as "- [ ] item".
func flagName(a string) (string, bool) {
	rest := strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-")
	if rest == a || rest == "" {
		return "", false
	}
	name := rest
	if eq := strings.IndexByte(name, '='); eq >= 0 {
		name = name[:eq]
	}
	if name == "" || !isLetter(name[0]) {
		return "", false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !isLetter(c) && !(c >= '0' && c <= '9') && c != '-' && c != '_' {
			return "", false
		}
	}
	return name, true
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
