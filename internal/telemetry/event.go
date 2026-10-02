// Package telemetry records one structured, privacy-safe event per issue-cli
// invocation into a local append-only JSONL file and aggregates those events
// into a usage report. Nothing is ever sent off-machine.
//
// Events carry flag NAMES, never values, except for a tiny whitelist of
// structural values (see Safe). This is deliberately separate from the CLI's
// raw-args action log (logAction / clilog), which feeds the agent timeline.
package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// SchemaVersion is written into every event as "v". Readers skip events with
// a higher version they don't understand.
const SchemaVersion = 1

// Event is one issue-cli invocation.
type Event struct {
	V     int    `json:"v"`
	TS    string `json:"ts"`
	DurMS int64  `json:"dur_ms"`

	Cmd   string `json:"cmd"`
	Sub   string `json:"sub,omitempty"`
	Alias string `json:"alias,omitempty"`
	// Flags are flag names as typed (without leading dashes), global flags
	// included. Never values.
	Flags []string `json:"flags,omitempty"`

	Project  string `json:"project,omitempty"`
	Issue    string `json:"issue,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	Safe     *Safe  `json:"safe,omitempty"`

	Exit     int    `json:"exit"`
	ErrClass string `json:"err_class,omitempty"`
	// Unknown is the offending token for unknown command / subcommand /
	// topic / flag errors, truncated to MaxUnknownLen.
	Unknown string `json:"unknown,omitempty"`

	Caller string `json:"caller"`
	Agent  string `json:"agent,omitempty"`
	Tmux   bool   `json:"tmux,omitempty"`

	PID      int    `json:"pid"`
	PPID     int    `json:"ppid"`
	ArgsHash string `json:"args_hash"`
}

// Safe holds the only argument values telemetry is allowed to store.
type Safe struct {
	To      string `json:"to,omitempty"`
	Section string `json:"section,omitempty"`
}

// Empty reports whether no safe value is set.
func (s *Safe) Empty() bool { return s == nil || (s.To == "" && s.Section == "") }

const (
	MaxUnknownLen = 64
	MaxSafeLen    = 64
	// MaxEventBytes caps one serialized event so a single O_APPEND write
	// stays atomic between concurrent writers.
	MaxEventBytes = 4096
)

// Caller values.
const (
	CallerDispatched = "dispatched"
	CallerAgent      = "agent"
	CallerTTY        = "tty"
	CallerScript     = "script"
)

// Error classes.
const (
	ErrUnknownCommand    = "unknown_command"
	ErrUnknownSubcommand = "unknown_subcommand"
	ErrUnknownTopic      = "unknown_topic"
	ErrUnknownFlag       = "unknown_flag"
	ErrFlagParse         = "flag_parse"
	ErrUsage             = "usage"
	ErrProjectResolution = "project_resolution"
	ErrApprovalMissing   = "approval_missing"
	ErrIssueNotFound     = "issue_not_found"
	ErrInvalidTransition = "invalid_transition"
	ErrValidation        = "validation"
	ErrPanic             = "panic"
	ErrOther             = "other"
)

// HashArgs returns a short, stable fingerprint of the raw args so identical
// repeats can be detected without storing values.
func HashArgs(args []string) string {
	h := sha256.Sum256([]byte(strings.Join(args, "\x00")))
	return hex.EncodeToString(h[:8])
}

// Truncate shortens s to at most n bytes without splitting a UTF-8 rune.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// CommandKey is how the report groups events: "cmd" or "cmd sub".
func (e Event) CommandKey() string {
	if e.Sub != "" {
		return e.Cmd + " " + e.Sub
	}
	return e.Cmd
}
