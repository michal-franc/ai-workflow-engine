package tracker

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

type DocPage struct {
	Title   string `yaml:"title"`
	Order   int    `yaml:"order"`
	Slug    string `yaml:"-"`
	Section string `yaml:"-"`

	BodyHTML string `yaml:"-"`
}

type DocSection struct {
	Name  string
	Pages []*DocPage
}

func ParseDocPage(relPath string, data []byte) (*DocPage, error) {
	page := &DocPage{}
	body, err := ParseFrontmatter(string(data), page)
	if err != nil {
		body = string(data)
	}

	page.Slug = strings.TrimSuffix(relPath, ".md")

	dir := filepath.Dir(relPath)
	if dir == "." {
		page.Section = ""
	} else {
		page.Section = dir
	}

	if page.Title == "" {
		base := strings.TrimSuffix(filepath.Base(relPath), ".md")
		page.Title = strings.ReplaceAll(base, "-", " ")
		page.Title = titleCase(page.Title)
	}

	md := goldmark.New(goldmark.WithExtensions(extension.TaskList, extension.Table))
	var buf bytes.Buffer
	if err := md.Convert([]byte(body), &buf); err != nil {
		return nil, fmt.Errorf("rendering markdown in %s: %w", relPath, err)
	}
	page.BodyHTML = buf.String()

	return page, nil
}

func LoadDocs(dir string) ([]*DocPage, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, nil
	}

	var pages []*DocPage

	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}

		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping doc %s: %v\n", path, err)
			return nil
		}

		relPath, _ := filepath.Rel(dir, path)

		page, err := ParseDocPage(relPath, data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping doc %s: %v\n", path, err)
			return nil
		}

		pages = append(pages, page)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking docs directory %s: %w", dir, err)
	}

	sort.Slice(pages, func(i, j int) bool {
		if pages[i].Section != pages[j].Section {
			if pages[i].Section == "" {
				return true
			}
			if pages[j].Section == "" {
				return false
			}
			return pages[i].Section < pages[j].Section
		}
		if pages[i].Order != pages[j].Order {
			return pages[i].Order < pages[j].Order
		}
		return pages[i].Title < pages[j].Title
	})

	return pages, nil
}

func GroupDocSections(pages []*DocPage) []DocSection {
	sectionMap := map[string][]*DocPage{}
	var sectionOrder []string
	seen := map[string]bool{}

	for _, p := range pages {
		s := p.Section
		if !seen[s] {
			sectionOrder = append(sectionOrder, s)
			seen[s] = true
		}
		sectionMap[s] = append(sectionMap[s], p)
	}

	var sections []DocSection
	for _, name := range sectionOrder {
		sections = append(sections, DocSection{Name: name, Pages: sectionMap[name]})
	}
	return sections
}

func titleCase(s string) string {
	prev := ' '
	return strings.Map(func(r rune) rune {
		if prev == ' ' || prev == '-' || prev == '_' {
			prev = r
			return unicode.ToUpper(r)
		}
		prev = r
		return r
	}, s)
}
