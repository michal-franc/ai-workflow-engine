package tracker

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// TaskBox is one task-list checkbox in an issue body as goldmark parses it:
// a `[ ]` that opens a list item, never one in a code span, a code block or
// prose. Line is 1-based in the body (Issue.BodyRaw); Offset is the byte
// offset of its `[`.
type TaskBox struct {
	Line    int
	Offset  int
	Checked bool
}

// TaskBoxes lists the body's task-list checkboxes in document order: the
// same boxes, in the same order, as the rendered page shows.
func TaskBoxes(body string) []TaskBox {
	src := []byte(body)
	doc := newIssueMarkdown().Parser().Parse(text.NewReader(src))
	var boxes []TaskBox
	_ = gast.Walk(doc, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		if cb, ok := n.(*extast.TaskCheckBox); ok {
			if off, ok := taskBoxOffset(cb, src); ok {
				boxes = append(boxes, TaskBox{Line: lineAt(src, off), Offset: off, Checked: cb.IsChecked})
			}
		}
		return gast.WalkContinue, nil
	})
	return boxes
}

// taskBoxOffset finds the `[` of a parsed checkbox: the task-list parser
// only matches at the start of its list item's first line.
func taskBoxOffset(cb *extast.TaskCheckBox, src []byte) (int, bool) {
	parent := cb.Parent()
	if parent == nil || parent.Lines().Len() == 0 {
		return 0, false
	}
	seg := parent.Lines().At(0)
	off := seg.Start
	for off < seg.Stop && (src[off] == ' ' || src[off] == '\t') {
		off++
	}
	if off+2 >= len(src) || src[off] != '[' || src[off+2] != ']' {
		return 0, false
	}
	return off, true
}

func lineAt(src []byte, off int) int {
	return bytes.Count(src[:off], []byte("\n")) + 1
}

var (
	// ErrNoTaskBox marks a toggle of a line that holds no task-list checkbox
	// (a `[ ]` in code or prose, or no `[ ]` at all).
	ErrNoTaskBox = errors.New("no task-list checkbox on that line")
	// ErrTaskBoxState marks a toggle whose box is already in the asked state:
	// the page showed something other than the file.
	ErrTaskBoxState = errors.New("checkbox is already in that state")
)

// ToggleTaskBox sets the task-list checkbox on the body's line to checked
// and changes nothing else in the body.
func ToggleTaskBox(body string, line int, checked bool) (string, error) {
	for _, b := range TaskBoxes(body) {
		if b.Line != line {
			continue
		}
		if b.Checked == checked {
			return "", ErrTaskBoxState
		}
		mark := " "
		if checked {
			mark = "x"
		}
		return body[:b.Offset+1] + mark + body[b.Offset+2:], nil
	}
	return "", fmt.Errorf("line %d: %w", line, ErrNoTaskBox)
}

// ToggleIssueTaskBox is ToggleTaskBox on the issue file, under the issue
// lock, refused with ErrStaleBody when the body no longer hashes to
// baseHash. It returns the body as written.
func ToggleIssueTaskBox(filePath, baseHash string, line int, checked bool) (string, error) {
	body, _, err := UpdateIssueBody(filePath, func(body string) (string, bool, error) {
		if BodyHash(body) != baseHash {
			return "", false, ErrStaleBody
		}
		next, err := ToggleTaskBox(body, line, checked)
		if err != nil {
			return "", false, err
		}
		return next, true, nil
	})
	if err != nil {
		return "", err
	}
	return body, nil
}

// newIssueMarkdown is the issue body renderer. Each task-list checkbox
// carries data-line, its line in the body, so the page can ask the server
// to flip that line (ToggleIssueTaskBox) instead of counting `[ ]`.
func newIssueMarkdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.TaskList, extension.Table),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
			renderer.WithNodeRenderers(util.Prioritized(taskBoxLineRenderer{}, 100)),
		),
	)
}

type taskBoxLineRenderer struct{}

func (taskBoxLineRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(extast.KindTaskCheckBox, renderTaskBoxWithLine)
}

func renderTaskBoxWithLine(w util.BufWriter, src []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	if !entering {
		return gast.WalkContinue, nil
	}
	n := node.(*extast.TaskCheckBox)
	if n.IsChecked {
		_, _ = w.WriteString(`<input checked="" disabled="" type="checkbox"`)
	} else {
		_, _ = w.WriteString(`<input disabled="" type="checkbox"`)
	}
	if off, ok := taskBoxOffset(n, src); ok {
		_, _ = w.WriteString(` data-line="` + strconv.Itoa(lineAt(src, off)) + `"`)
	}
	_, _ = w.WriteString("> ")
	return gast.WalkContinue, nil
}
