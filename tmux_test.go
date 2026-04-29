package main

import (
	"testing"
)

func TestSessionMatchesIssue(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
		slug        string
		want        bool
	}{
		{name: "normalized dispatch session", sessionName: "agent-api-integrate-with-claude-session-names-to-show-active-agent-work", slug: "api/integrate-with-claude-session-names-to-show-active-agent-work", want: true},
		{name: "plain slug fragment", sessionName: "claude-watch-bug-in-login", slug: "bug-in-login", want: true},
		{name: "different issue", sessionName: "agent-something-else", slug: "bug-in-login", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionMatchesIssue(tt.sessionName, tt.slug); got != tt.want {
				t.Fatalf("sessionMatchesIssue(%q, %q) = %v, want %v", tt.sessionName, tt.slug, got, tt.want)
			}
		})
	}
}
