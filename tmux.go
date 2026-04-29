package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var listTmuxSessions = defaultListTmuxSessions
var tmuxSendKeys = defaultTmuxSendKeys

func tmuxSessionName(slug string) string {
	r := strings.NewReplacer("/", "-", ".", "-", " ", "-")
	return "agent-" + r.Replace(slug)
}

func defaultTmuxSendKeys(target string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	content := strings.Join(lines, "\n")
	tmp, err := os.CreateTemp("", "issue-approval-*.txt")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := exec.Command("tmux", "load-buffer", tmpPath).Run(); err != nil {
		return err
	}
	if err := exec.Command("tmux", "paste-buffer", "-t", target).Run(); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return exec.Command("tmux", "send-keys", "-t", target, "Enter").Run()
}

func defaultListTmuxSessions() []AgentSession {
	out, err := exec.Command("tmux", "ls").Output()
	if err != nil {
		return nil
	}

	lineRE := regexp.MustCompile(`^([^:]+):.*\(created ([^)]+)\)`)
	var sessions []AgentSession
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		matches := lineRE.FindStringSubmatch(line)
		if len(matches) < 3 {
			continue
		}

		session := AgentSession{Name: strings.TrimSpace(matches[1])}
		if created, err := time.Parse("Mon Jan _2 15:04:05 2006", strings.TrimSpace(matches[2])); err == nil {
			session.StartTime = created.Format("2006-01-02 15:04:05")
		} else {
			session.StartTime = strings.TrimSpace(matches[2])
		}
		sessions = append(sessions, session)
	}
	return sessions
}

func sessionsByIssueSlug(issues []*tracker.Issue) (map[string][]AgentSession, int) {
	sessions := listTmuxSessions()
	result := make(map[string][]AgentSession, len(issues))
	matchedSessionNames := map[string]bool{}
	for _, issue := range issues {
		for _, session := range sessions {
			if sessionMatchesIssue(session.Name, issue.Slug) {
				result[issue.Slug] = append(result[issue.Slug], session)
				matchedSessionNames[session.Name] = true
			}
		}
	}
	return result, len(matchedSessionNames)
}

func sessionMatchesIssue(sessionName string, slug string) bool {
	sessionName = strings.ToLower(strings.TrimSpace(sessionName))
	slug = strings.ToLower(strings.TrimSpace(slug))
	if sessionName == "" || slug == "" {
		return false
	}

	candidates := []string{
		slug,
		strings.ReplaceAll(slug, "/", "-"),
		tmuxSessionName(slug),
	}
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(sessionName, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}
