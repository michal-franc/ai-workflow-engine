package tracker

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"gopkg.in/yaml.v3"
)

// ExtraField holds a single unknown frontmatter field for display in the sidebar.
type ExtraField struct {
	Key    string
	Label  string
	Value  string
	Values []string
	IsURL  bool
	IsList bool
}

var knownFrontmatterFields = map[string]bool{
	"title": true, "status": true, "system": true, "version": true,
	"labels": true, "priority": true, "assignee": true,
	"human_approval": true, "approved_for": true,
	"started_at": true, "done_at": true, "created": true,
	"number": true, "repo": true,
}

func formatFieldLabel(key string) string {
	words := strings.Split(key, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func extractExtraFields(rawMap map[string]interface{}) []ExtraField {
	var keys []string
	for k := range rawMap {
		if !knownFrontmatterFields[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var fields []ExtraField
	for _, k := range keys {
		ef := ExtraField{Key: k, Label: formatFieldLabel(k)}
		switch val := rawMap[k].(type) {
		case []interface{}:
			ef.IsList = true
			for _, item := range val {
				ef.Values = append(ef.Values, fmt.Sprintf("%v", item))
			}
		default:
			ef.Value = fmt.Sprintf("%v", val)
			ef.IsURL = strings.HasPrefix(ef.Value, "http://") || strings.HasPrefix(ef.Value, "https://")
		}
		fields = append(fields, ef)
	}
	return fields
}

// StatusOrder defines the workflow lifecycle.
var StatusOrder = []string{
	"idea", "in design", "backlog", "in progress", "testing", "human-testing", "documentation", "shipping", "done",
}

var StatusDescriptions = map[string]string{
	"idea":          "Raw idea, needs exploration",
	"in design":     "Being designed and specced out",
	"backlog":       "Ready to work on",
	"in progress":   "Actively being implemented",
	"testing":       "Under verification",
	"human-testing": "Manual verification by humans",
	"documentation": "Being documented",
	"shipping":      "Committing and pushing changes",
	"done":          "Completed",
}

type Issue struct {
	Title          string   `yaml:"title"`
	Status         string   `yaml:"status"`
	System         string   `yaml:"system"`
	Version        string   `yaml:"version"`
	Labels         []string `yaml:"labels"`
	Priority       string   `yaml:"priority"`
	Assignee       string   `yaml:"assignee"`
	HumanApproval  string   `yaml:"human_approval"`
	LegacyApproval string   `yaml:"approved_for"`
	StartedAt      string   `yaml:"started_at"`
	DoneAt         string   `yaml:"done_at"`
	Created        string   `yaml:"created"`
	Number         int      `yaml:"number"`
	Repo           string   `yaml:"repo"`

	// Computed fields
	Slug        string       `yaml:"-"`
	FilePath    string       `yaml:"-"`
	ModTime     time.Time    `yaml:"-"`
	BodyHTML    string       `yaml:"-"`
	BodyRaw     string       `yaml:"-"`
	GithubURL   string       `yaml:"-"`
	ExtraFields []ExtraField `yaml:"-"`
}

func ParseIssue(filename string, data []byte) (*Issue, error) {
	issue := &Issue{}
	rawBody, err := ParseFrontmatter(string(data), issue)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	body, _ := ParseComments(rawBody)
	body = strings.TrimSpace(body)
	issue.BodyRaw = body

	md := goldmark.New(goldmark.WithExtensions(extension.TaskList, extension.Table))
	var buf bytes.Buffer
	if err := md.Convert([]byte(body), &buf); err != nil {
		return nil, fmt.Errorf("rendering markdown in %s: %w", filename, err)
	}
	issue.BodyHTML = buf.String()

	issue.Slug = Slugify(issue.Title)

	issue.Status = strings.ToLower(strings.TrimSpace(issue.Status))
	issue.Priority = strings.ToLower(strings.TrimSpace(issue.Priority))
	issue.System = strings.TrimSpace(issue.System)
	if issue.HumanApproval == "" {
		issue.HumanApproval = strings.TrimSpace(issue.LegacyApproval)
	}
	if issue.Repo != "" && issue.Number > 0 {
		issue.GithubURL = fmt.Sprintf("https://github.com/%s/issues/%d", issue.Repo, issue.Number)
	}

	// Extract unknown frontmatter fields for display
	if strings.HasPrefix(string(data), "---") {
		parts := strings.SplitN(string(data)[3:], "\n---", 2)
		if len(parts) >= 1 {
			var rawMap map[string]interface{}
			if yaml.Unmarshal([]byte(parts[0]), &rawMap) == nil {
				issue.ExtraFields = extractExtraFields(rawMap)
			}
		}
	}

	return issue, nil
}

const (
	issueLockRetryDelay = 25 * time.Millisecond
	issueLockTimeout    = 15 * time.Second
)

func issueLockPath(filePath string) string {
	return filePath + ".lock"
}

func withIssueLock(filePath string, fn func() error) error {
	lockPath := issueLockPath(filePath)
	deadline := time.Now().Add(issueLockTimeout)

	for {
		lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			fmt.Fprintf(lockFile, "pid=%d\ntime=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
			lockFile.Close()
			defer os.Remove(lockPath)
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("creating lock %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for issue lock %s", filePath)
		}
		time.Sleep(issueLockRetryDelay)
	}
}

func writeFileAtomically(filePath string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, filepath.Base(filePath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file for %s: %w", filePath, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("setting temp permissions for %s: %w", filePath, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp file for %s: %w", filePath, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing temp file for %s: %w", filePath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file for %s: %w", filePath, err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("replacing %s atomically: %w", filePath, err)
	}
	return nil
}

func LoadIssues(dir string) ([]*Issue, error) {
	var issues []*Issue

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", path, err)
			return nil
		}

		issue, err := ParseIssue(d.Name(), data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", path, err)
			return nil
		}

		issue.FilePath = path

		if info, err := d.Info(); err == nil {
			issue.ModTime = info.ModTime()
		}

		relDir := filepath.Dir(path)
		baseDir, _ := filepath.Rel(dir, relDir)
		if baseDir != "" && baseDir != "." {
			issue.Slug = strings.ToLower(baseDir) + "/" + issue.Slug
			// Auto-detect system from subdirectory if not set in frontmatter
			if issue.System == "" {
				issue.System = baseDir
			}
		}

		issues = append(issues, issue)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory %s: %w", dir, err)
	}

	// Handle slug collisions
	seen := map[string]int{}
	for _, issue := range issues {
		count := seen[issue.Slug]
		seen[issue.Slug]++
		if count > 0 {
			issue.Slug = fmt.Sprintf("%s-%d", issue.Slug, count+1)
		}
	}

	sort.Slice(issues, func(i, j int) bool {
		return issues[i].ModTime.After(issues[j].ModTime)
	})

	return issues, nil
}

type IssueUpdate struct {
	Title         *string           `json:"title,omitempty"`
	Status        *string           `json:"status,omitempty"`
	Priority      *string           `json:"priority,omitempty"`
	Version       *string           `json:"version,omitempty"`
	Assignee      *string           `json:"assignee,omitempty"`
	HumanApproval *string           `json:"human_approval,omitempty"`
	StartedAt     *string           `json:"started_at,omitempty"`
	DoneAt        *string           `json:"done_at,omitempty"`
	Labels        []string          `json:"labels,omitempty"`
	Body          *string           `json:"body,omitempty"`
	ExtraFields   map[string]string `json:"extra_fields,omitempty"`
}

func AppendIssueBody(body, text string) (string, bool, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return body, false, nil
	}

	existing := map[string]bool{}
	for _, match := range findAllHeadings(body) {
		existing[match.Key] = true
	}

	textHeadings := findAllHeadings(text)

	// Auto-route: if text begins with a heading that already exists in body
	// and the rest of the text only contains deeper subheadings, treat this
	// as an append into that existing section.
	if len(textHeadings) > 0 && textHeadings[0].StartLine == 0 && existing[textHeadings[0].Key] {
		lead := textHeadings[0]
		canAutoRoute := true
		for _, h := range textHeadings[1:] {
			if h.Level <= lead.Level {
				canAutoRoute = false
				break
			}
		}
		if canAutoRoute {
			lines := strings.Split(text, "\n")
			_, title, _ := parseHeadingLine(lines[0])
			remainder := strings.Join(lines[1:], "\n")
			return AppendIssueBodyToSection(body, title, remainder, false)
		}
	}

	var duplicates []string
	seen := map[string]bool{}
	for _, match := range textHeadings {
		if existing[match.Key] && !seen[match.Key] {
			duplicates = append(duplicates, strings.TrimSpace(match.Line))
			seen[match.Key] = true
		}
	}
	if len(duplicates) > 0 {
		return body, false, fmt.Errorf("append would introduce duplicate heading(s): %s\n\nUse --section to target an existing section instead", strings.Join(duplicates, ", "))
	}

	body = strings.TrimRight(body, "\n")
	if body == "" {
		return text + "\n", true, nil
	}
	return body + "\n" + text + "\n", true, nil
}

func AppendIssueBodyToSection(body, section, text string, force bool) (string, bool, error) {
	matches := findHeadingMatches(body, section)
	if len(matches) > 1 && !force {
		var labels []string
		for _, match := range matches {
			labels = append(labels, fmt.Sprintf("%s (line %d)", match.Line, match.StartLine+1))
		}
		return body, false, fmt.Errorf("multiple matching sections for %q: %s\n\nRerun with --force to merge into the first matching section", section, strings.Join(labels, ", "))
	}
	if len(matches) > 1 {
		nextBody, changed := appendContentToMatch(body, matches[0], text)
		return nextBody, changed, nil
	}
	if len(matches) == 1 {
		nextBody, changed := appendContentToMatch(body, matches[0], text)
		return nextBody, changed, nil
	}

	nextBody, changed := appendToSection(body, section, text)
	return nextBody, changed, nil
}

// ReplaceIssueBodySection replaces the content of an existing section with text.
// The heading line is preserved; everything between it and the next heading of
// equal or shallower depth is replaced. Errors if no matching section exists,
// or if multiple sections match and force is false.
func ReplaceIssueBodySection(body, section, text string, force bool) (string, bool, error) {
	matches := findHeadingMatches(body, section)
	if len(matches) == 0 {
		return body, false, fmt.Errorf("no section matching %q in issue body\n\nUse 'issue-cli append --section %q' to create a new section", section, section)
	}
	if len(matches) > 1 && !force {
		var labels []string
		for _, match := range matches {
			labels = append(labels, fmt.Sprintf("%s (line %d)", match.Line, match.StartLine+1))
		}
		return body, false, fmt.Errorf("multiple matching sections for %q: %s\n\nRerun with --force to replace the first matching section", section, strings.Join(labels, ", "))
	}
	nextBody, changed := replaceContentInMatch(body, matches[0], text)
	return nextBody, changed, nil
}

func findAllHeadings(body string) []headingMatch {
	lines := strings.Split(body, "\n")
	fenceFlags := computeFenceFlags(lines)
	var matches []headingMatch
	for i, line := range lines {
		if fenceFlags[i] {
			continue
		}
		level, title, ok := parseHeadingLine(line)
		if !ok {
			continue
		}
		matches = append(matches, headingMatch{
			StartLine: i,
			Level:     level,
			Line:      strings.TrimSpace(line),
			Key:       normalizeHeadingKey(title),
		})
	}
	return matches
}

// UpdateIssueBody applies a body transformation while holding the issue lock.
// It reloads the current body under the lock so callers do not overwrite newer edits
// based on a stale pre-lock snapshot.
func UpdateIssueBody(filePath string, update func(body string) (string, bool, error)) (string, bool, error) {
	var updatedBody string
	var changed bool

	err := withIssueLock(filePath, func() error {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", filePath, err)
		}

		content := string(data)
		if !strings.HasPrefix(content, "---") {
			return fmt.Errorf("no frontmatter in %s", filePath)
		}

		parts := strings.SplitN(content[3:], "\n---", 2)
		if len(parts) < 2 {
			return fmt.Errorf("invalid frontmatter in %s", filePath)
		}

		fmRaw := parts[0]
		bodyWithComments := parts[1]
		body, existingComments := ParseComments(bodyWithComments)
		body = strings.TrimSpace(body)

		nextBody, ok, err := update(body)
		if err != nil {
			return err
		}
		if !ok {
			updatedBody = body
			return nil
		}

		updatedBody = nextBody
		changed = true

		var out strings.Builder
		out.WriteString("---")
		out.WriteString(fmRaw)
		out.WriteString("\n---")
		out.WriteString("\n")
		out.WriteString(nextBody)
		out.WriteString("\n")
		out.WriteString(SerializeComments(existingComments))

		info, err := os.Stat(filePath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", filePath, err)
		}
		return writeFileAtomically(filePath, []byte(out.String()), info.Mode().Perm())
	})

	return updatedBody, changed, err
}

func updateIssueFrontmatterLocked(filePath string, update IssueUpdate) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filePath, err)
	}

	content := string(data)
	if !strings.HasPrefix(content, "---") {
		return fmt.Errorf("no frontmatter in %s", filePath)
	}

	parts := strings.SplitN(content[3:], "---", 2)
	if len(parts) < 2 {
		return fmt.Errorf("invalid frontmatter in %s", filePath)
	}

	fmRaw := parts[0]
	body := parts[1]

	setScalar := func(fm, key, value string) string {
		re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:.*$`)
		newLine := key + `: "` + value + `"`
		if re.MatchString(fm) {
			return re.ReplaceAllString(fm, newLine)
		}
		return strings.TrimRight(fm, "\n") + "\n" + newLine + "\n"
	}

	removeField := func(fm, key string) string {
		re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:.*\n?`)
		fm = re.ReplaceAllString(fm, "")
		reList := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*\n([ \t]+-[^\n]*\n?)*`)
		fm = reList.ReplaceAllString(fm, "")
		return fm
	}

	setLabels := func(fm string, labels []string) string {
		fm = removeField(fm, "labels")
		if len(labels) == 0 {
			return fm
		}
		var buf strings.Builder
		buf.WriteString("labels:\n")
		for _, l := range labels {
			buf.WriteString("  - " + l + "\n")
		}
		return strings.TrimRight(fm, "\n") + "\n" + buf.String()
	}

	if update.Title != nil {
		fmRaw = setScalar(fmRaw, "title", *update.Title)
	}
	if update.Status != nil {
		fmRaw = setScalar(fmRaw, "status", *update.Status)
	}
	if update.Priority != nil {
		if *update.Priority == "" {
			fmRaw = removeField(fmRaw, "priority")
		} else {
			fmRaw = setScalar(fmRaw, "priority", *update.Priority)
		}
	}
	if update.Version != nil {
		if *update.Version == "" {
			fmRaw = removeField(fmRaw, "version")
		} else {
			fmRaw = setScalar(fmRaw, "version", *update.Version)
		}
	}
	if update.Assignee != nil {
		if *update.Assignee == "" {
			fmRaw = removeField(fmRaw, "assignee")
		} else {
			fmRaw = setScalar(fmRaw, "assignee", *update.Assignee)
		}
	}
	if update.HumanApproval != nil {
		fmRaw = removeField(fmRaw, "approved_for")
		if *update.HumanApproval == "" {
			fmRaw = removeField(fmRaw, "human_approval")
		} else {
			fmRaw = setScalar(fmRaw, "human_approval", *update.HumanApproval)
		}
	}
	if update.StartedAt != nil {
		if *update.StartedAt == "" {
			fmRaw = removeField(fmRaw, "started_at")
		} else {
			fmRaw = setScalar(fmRaw, "started_at", *update.StartedAt)
		}
	}
	if update.DoneAt != nil {
		if *update.DoneAt == "" {
			fmRaw = removeField(fmRaw, "done_at")
		} else {
			fmRaw = setScalar(fmRaw, "done_at", *update.DoneAt)
		}
	}
	if update.Labels != nil {
		fmRaw = setLabels(fmRaw, update.Labels)
	}

	if len(update.ExtraFields) > 0 {
		keys := make([]string, 0, len(update.ExtraFields))
		for k := range update.ExtraFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if ProtectedFrontmatterFields[k] || !frontmatterKeyRe.MatchString(k) {
				continue
			}
			v := update.ExtraFields[k]
			if v == "" {
				fmRaw = removeFrontmatterKey(fmRaw, k)
			} else {
				fmRaw = setFrontmatterScalar(fmRaw, k, v)
			}
		}
	}

	if update.Body != nil {
		_, existingComments := ParseComments(body)
		body = "\n" + *update.Body + "\n" + SerializeComments(existingComments)
	}

	var out strings.Builder
	out.WriteString("---")
	out.WriteString(fmRaw)
	out.WriteString("\n---")
	out.WriteString(body)

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", filePath, err)
	}
	return writeFileAtomically(filePath, []byte(out.String()), info.Mode().Perm())
}

func UpdateIssueFrontmatter(filePath string, update IssueUpdate) error {
	return withIssueLock(filePath, func() error {
		return updateIssueFrontmatterLocked(filePath, update)
	})
}

// ProtectedFrontmatterFields lists keys that SetFrontmatterField refuses to touch
// because they are managed elsewhere (workflow transitions, claim/start, GitHub sync).
var ProtectedFrontmatterFields = map[string]bool{
	"title":          true,
	"status":         true,
	"human_approval": true,
	"approved_for":   true,
	"started_at":     true,
	"done_at":        true,
	"number":         true,
	"repo":           true,
	"created":        true,
	"labels":         true,
}

var frontmatterKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func escapeYAMLScalar(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func setFrontmatterScalar(fm, key, value string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:.*$`)
	newLine := key + `: "` + escapeYAMLScalar(value) + `"`
	if re.MatchString(fm) {
		return re.ReplaceAllString(fm, newLine)
	}
	return strings.TrimRight(fm, "\n") + "\n" + newLine + "\n"
}

func removeFrontmatterKey(fm, key string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:.*\n?`)
	fm = re.ReplaceAllString(fm, "")
	reList := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*\n([ \t]+-[^\n]*\n?)*`)
	fm = reList.ReplaceAllString(fm, "")
	return fm
}

// SetFrontmatterField writes or clears a single scalar frontmatter key.
// Protected keys (see ProtectedFrontmatterFields) are refused. Pass clear=true
// to remove the field; value is ignored in that case.
func SetFrontmatterField(filePath, key, value string, clear bool) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if !frontmatterKeyRe.MatchString(key) {
		return fmt.Errorf("invalid key %q: must start with a lowercase letter and contain only lowercase letters, digits, or underscores", key)
	}
	if ProtectedFrontmatterFields[key] {
		return fmt.Errorf("field %q is managed by the workflow and cannot be set via set-meta", key)
	}

	return withIssueLock(filePath, func() error {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", filePath, err)
		}

		content := string(data)
		if !strings.HasPrefix(content, "---") {
			return fmt.Errorf("no frontmatter in %s", filePath)
		}

		parts := strings.SplitN(content[3:], "---", 2)
		if len(parts) < 2 {
			return fmt.Errorf("invalid frontmatter in %s", filePath)
		}

		fmRaw := parts[0]
		body := parts[1]

		if clear {
			fmRaw = removeFrontmatterKey(fmRaw, key)
		} else {
			fmRaw = setFrontmatterScalar(fmRaw, key, value)
		}

		var out strings.Builder
		out.WriteString("---")
		out.WriteString(fmRaw)
		out.WriteString("\n---")
		out.WriteString(body)

		info, err := os.Stat(filePath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", filePath, err)
		}
		return writeFileAtomically(filePath, []byte(out.String()), info.Mode().Perm())
	})
}

// DeleteIssue removes the issue markdown file and its comment sidecar if present.
func DeleteIssue(filePath string) error {
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("deleting %s: %w", filePath, err)
	}
	// Also remove comment sidecar if it exists
	commentFile := strings.TrimSuffix(filePath, ".md") + ".comments.yaml"
	os.Remove(commentFile) // ignore error — may not exist
	return nil
}

// CreateIssueFile creates a new issue markdown file and returns the file path and slug.
type CreateIssueOpts struct {
	Title    string
	Status   string
	System   string
	Version  string
	Priority string
	Labels   []string
	Body     string
}

func CreateIssueFile(issueDir, title, status, system, version string) (filePath, slug string, err error) {
	return CreateIssueFileOpts(issueDir, CreateIssueOpts{
		Title: title, Status: status, System: system, Version: version,
	})
}

func CreateIssueFileOpts(issueDir string, opts CreateIssueOpts) (filePath, slug string, err error) {
	if opts.Title == "" {
		return "", "", fmt.Errorf("title is required")
	}
	if opts.Status == "" {
		opts.Status = "idea"
	}

	dir := issueDir
	if opts.System != "" {
		dir = filepath.Join(dir, opts.System)
		os.MkdirAll(dir, 0755)
	}

	slug = Slugify(opts.Title)
	filename := filepath.Join(dir, slug+".md")

	var content strings.Builder
	content.WriteString("---\n")
	content.WriteString(fmt.Sprintf("title: \"%s\"\n", strings.ReplaceAll(opts.Title, "\"", "\\\"")))
	content.WriteString(fmt.Sprintf("status: \"%s\"\n", opts.Status))
	if opts.System != "" {
		content.WriteString(fmt.Sprintf("system: \"%s\"\n", opts.System))
	}
	if opts.Version != "" {
		content.WriteString(fmt.Sprintf("version: \"%s\"\n", opts.Version))
	}
	if opts.Priority != "" {
		content.WriteString(fmt.Sprintf("priority: \"%s\"\n", opts.Priority))
	}
	if len(opts.Labels) > 0 {
		content.WriteString("labels:\n")
		for _, l := range opts.Labels {
			content.WriteString(fmt.Sprintf("  - %s\n", l))
		}
	}
	content.WriteString("---\n")
	if opts.Body != "" {
		content.WriteString("\n" + opts.Body + "\n")
	} else {
		content.WriteString("\n")
	}

	if err := writeFileAtomically(filename, []byte(content.String()), 0644); err != nil {
		return "", "", fmt.Errorf("creating issue: %w", err)
	}

	if opts.System != "" {
		slug = strings.ToLower(opts.System) + "/" + slug
	}

	return filename, slug, nil
}

// CollectSubdirSystems returns the names of immediate subdirectories in dir.
// This captures systems that exist as folders even if they contain no issues.
func CollectSubdirSystems(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var systems []string
	for _, e := range entries {
		if e.IsDir() {
			systems = append(systems, e.Name())
		}
	}
	return systems
}

func CollectFilterValues(issues []*Issue) (statuses, systems, priorities, labels, assignees []string) {
	statusSet := map[string]bool{}
	systemSet := map[string]bool{}
	prioritySet := map[string]bool{}
	labelSet := map[string]bool{}
	assigneeSet := map[string]bool{}

	for _, issue := range issues {
		if issue.Status != "" {
			statusSet[issue.Status] = true
		}
		if issue.System != "" {
			systemSet[issue.System] = true
		}
		if issue.Priority != "" {
			prioritySet[issue.Priority] = true
		}
		if issue.Assignee != "" {
			assigneeSet[issue.Assignee] = true
		}
		for _, l := range issue.Labels {
			labelSet[l] = true
		}
	}

	for k := range statusSet {
		statuses = append(statuses, k)
	}
	for k := range systemSet {
		systems = append(systems, k)
	}
	for k := range prioritySet {
		priorities = append(priorities, k)
	}
	for k := range labelSet {
		labels = append(labels, k)
	}
	for k := range assigneeSet {
		assignees = append(assignees, k)
	}

	sort.Strings(statuses)
	sort.Strings(systems)
	sort.Strings(priorities)
	sort.Strings(labels)
	sort.Strings(assignees)

	return
}

// StatusIndex returns the position of a status in the workflow, or -1.
func StatusIndex(status string) int {
	for i, s := range StatusOrder {
		if s == status {
			return i
		}
	}
	return -1
}

// ValidTransition checks if moving from one status to the next is allowed.
func ValidTransition(from, to string) bool {
	fi := StatusIndex(from)
	ti := StatusIndex(to)
	if fi == -1 || ti == -1 {
		return false
	}
	return ti == fi+1
}

// CountCheckboxes returns total and checked checkbox counts in markdown.
func CountCheckboxes(body string) (total, checked int) {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [x]") || strings.HasPrefix(trimmed, "- [X]") {
			total++
			checked++
		} else if strings.HasPrefix(trimmed, "- [ ]") {
			total++
		}
	}
	return
}

// CountCheckboxesInSection counts checkboxes only under a specific ## heading.
// The section ends at the next ## heading or end of body.
func CountCheckboxesInSection(body, section string) (total, checked int) {
	inSection := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			heading := strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))
			inSection = strings.EqualFold(heading, section)
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(trimmed, "- [x]") || strings.HasPrefix(trimmed, "- [X]") {
			total++
			checked++
		} else if strings.HasPrefix(trimmed, "- [ ]") {
			total++
		}
	}
	return
}

// CheckCheckbox finds an unchecked checkbox whose text contains the query and checks it off.
// Returns the updated body and whether a match was found. Checkboxes inside
// fenced code blocks are skipped — they are illustrative quotes, not workflow
// state.
func CheckCheckbox(body, query string) (string, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	lines := strings.Split(body, "\n")
	fenceFlags := computeFenceFlags(lines)
	for i, line := range lines {
		if fenceFlags[i] {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ]") {
			text := strings.ToLower(strings.TrimSpace(trimmed[5:]))
			if strings.Contains(text, query) {
				lines[i] = strings.Replace(line, "- [ ]", "- [x]", 1)
				return strings.Join(lines, "\n"), true
			}
		}
	}
	return body, false
}

// HasTestPlan checks if the body has ## Test Plan with ### Automated and ### Manual.
func HasTestPlan(body string) (hasAutomated, hasManual bool) {
	inTestPlan := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, "## Test Plan") {
			inTestPlan = true
			continue
		}
		if inTestPlan && strings.HasPrefix(trimmed, "## ") {
			break
		}
		if inTestPlan {
			if strings.EqualFold(trimmed, "### Automated") {
				hasAutomated = true
			}
			if strings.EqualFold(trimmed, "### Manual") {
				hasManual = true
			}
		}
	}
	return
}

// HasCommentWithPrefix checks if any comment starts with the given prefix.
func HasCommentWithPrefix(comments []Comment, prefix string) bool {
	for _, c := range comments {
		if strings.HasPrefix(strings.ToLower(c.Text), strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}
