package tracker

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFrontmatter splits a markdown file into YAML frontmatter and body.
func ParseFrontmatter(content string, dest interface{}) (body string, err error) {
	if !strings.HasPrefix(content, "---") {
		return "", fmt.Errorf("missing frontmatter delimiter")
	}

	parts := strings.SplitN(content[3:], "\n---", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("missing closing frontmatter delimiter")
	}

	if err := yaml.Unmarshal([]byte(parts[0]), dest); err != nil {
		return "", fmt.Errorf("parsing frontmatter YAML: %w", err)
	}

	return strings.TrimSpace(parts[1]), nil
}

var multiHyphen = regexp.MustCompile(`-{2,}`)

// Slugify converts a string to a URL-safe slug.
func Slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		if r == ' ' || r == '_' || r == '.' {
			return '-'
		}
		return -1
	}, s)
	s = multiHyphen.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
