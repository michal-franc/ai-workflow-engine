// Package testids is the single list of data-testid ids the board's templates
// carry. Playtest sessions (playtest/sessions/*.yaml) point at these ids;
// `make playtest-check` rejects a session target that matches no entry, and
// TestTemplateTestIDs rejects a template id that is not listed here (or an
// entry no template uses).
//
// Ids are `page.area.element`, lower snake case, dots between levels. In an
// entry, `*` is any one segment (a status, a slug) and `#` is an index.
package testids

import (
	"regexp"
	"strings"
)

// IDs lists every test id and pattern. Keep it grouped by page.
var IDs = []string{
	// Project sidebar (templates/_sidebar.html), on every project page.
	"nav.sidebar",
	"nav.project",
	"nav.create",
	"nav.tab.*",
	"nav.more",
	"nav.agents_running",
	"nav.panel",
	"nav.panel.system.*",
	"nav.panel.row.*",
	"nav.actions",
	"nav.theme.*",
	"header.count",

	// Board (/p/<project>/board).
	"board.filters",
	"board.search",
	"board.filter.version",
	"board.filter.system",
	"board.filter.type",
	"board.filter.assignee",
	"board.clear",
	"board.hide_empty",
	"board.layout_toggle",
	"board.column.*",
	"board.column.*.title",
	"board.column.*.count",
	"board.column.*.add",
	"board.column.*.cards",
	"board.card.*",
	"board.card.*.title",
	"board.transition_modal",
	"board.transition_modal.title",
	"board.transition_modal.error",
	"board.transition_modal.confirm",
	"board.transition_modal.cancel",
	"board.toast",

	// List (/p/<project>/).
	"list.filters",
	"list.search",
	"list.filter.status",
	"list.filter.system",
	"list.filter.priority",
	"list.filter.assignee",
	"list.filter.type",
	"list.filter.label",
	"list.clear",
	"list.row.*",
	"list.row.*.title",
	"list.row.*.status",
	"list.empty",

	// Detail (/p/<project>/issue/<slug>).
	"detail.back",
	"detail.title",
	"detail.tab.preview",
	"detail.tab.edit",
	"detail.body",
	"detail.body.checkbox.#",
	"detail.body.textarea",
	"detail.body.save",
	"detail.sidebar",
	"detail.status",
	"detail.priority",
	"detail.assignee",
	"detail.approve.*",
	"detail.approve_then.*",
	"detail.labels",
	"detail.label.input",
	"detail.label.add",
	"detail.block.#",
	"detail.comment.eye.#",
	"detail.comment.input.#",
	"detail.comment.submit.#",
	"detail.toast",
	"detail.lead",
	"detail.lead.name",
	"detail.lead.state",

	// Project actions bar and its dispatch modal (list, board, graph).
	"actions.spawn_lead",
	"actions.modal",
	"actions.modal.steps",

	// Agents canvas (/p/<project>/agents). Nodes are keyed by tmux session
	// name (lead-<scope>, agent-<slug>), epic boxes by the epic's slug.
	"agents.toolbar",
	"agents.layout.tree",
	"agents.layout.radial",
	"agents.opt.issues",
	"agents.opt.stopped",
	"agents.live",
	"agents.spawn_lead",
	"agents.canvas",
	"agents.tree",
	"agents.radial",
	"agents.you",
	"agents.node.*",
	"agents.node.*.state",
	"agents.node.*.issue",
	"agents.unfinished",
	"agents.panel",
	"agents.panel.title",
	"agents.panel.issue",
	"agents.panel.approve",
	"agents.panel.spawn",
	"agents.panel.open",
	"agents.panel.open_status",
	"agents.panel.copy",
	"agents.panel.activity",
	"agents.panel.branch",
	"agents.panel.diff",
	"agents.spawn_modal",
	"agents.spawn_modal.steps",
	"agents.spawn_modal.close",
	"detail.canvas_link",

	// Issues canvas (/p/<project>/canvas). Lanes are keyed by epic slug,
	// "no_epic" or the system; columns and cells by version ("0_1",
	// "no_version").
	"canvas.toolbar",
	"canvas.count",
	"canvas.rows",
	"canvas.collapse_done",
	"canvas.show_no_version",
	"canvas.no_version_note",
	"canvas.highlight",
	"canvas.highlight.name",
	"canvas.highlight.clear",
	"canvas.zoom.out",
	"canvas.zoom.label",
	"canvas.zoom.in",
	"canvas.zoom.fit",
	"canvas.viewport",
	"canvas.world",
	"canvas.column.*",
	"canvas.column.*.progress",
	"canvas.column.*.extra",
	"canvas.lane.*",
	"canvas.lane.*.kind",
	"canvas.lane.*.status",
	"canvas.lane.*.parent",
	"canvas.lane.*.title",
	"canvas.lane.*.base",
	"canvas.lane.*.summary",
	"canvas.lane.*.highlight",
	"canvas.lane.*.note",
	"canvas.lane.*.show_no_version",
	"canvas.cell.*.*",
	"canvas.cell.*.*.closed",
	"canvas.cell.*.*.empty",
	"canvas.card.*",
	"canvas.card.*.status",
	"canvas.card.*.type",
	"canvas.card.*.title",
	"canvas.card.*.epic",
	"canvas.card.*.unknown_parent",
	// Decisions index (/p/<project>/decisions). Rows are keyed by the
	// recording issue's slug and the decision id ("d-1").
	"decisions.stats",
	"decisions.stat.total",
	"decisions.stat.active",
	"decisions.stat.replaced",
	"decisions.stat.stale",
	"decisions.empty",
	"decisions.filters",
	"decisions.filter.system.*",
	"decisions.filter.state.*",
	"decisions.nomatch",
	"decisions.nomatch.clear",
	"decisions.table",
	"decisions.row.*.*",
	"decisions.row.*.*.copy",
	"decisions.row.*.*.text",
	"decisions.row.*.*.how",
	"decisions.row.*.*.cited",
	"decisions.row.*.*.state",
	"decisions.toast",

	// Metrics view (templates/metrics.html): guard metrics, knobs, branches.
	"balance.group.*.guard",
	"metrics.page",
	"metrics.counts",
	"metrics.tab.metrics",
	"metrics.tab.knobs",
	"metrics.tab.branches",
	"metrics.git_problem",
	"metrics.problems",
	"metrics.run.master",
	"metrics.run.since",
	"metrics.run.branches",
	"metrics.filter.system",
	"metrics.filter.offonly",
	"metrics.table",
	"metrics.row.*",
	"metrics.row.*.branch",
	"metrics.row.*.verdict",
	"knobs.filter.edited",
	"knobs.row.*",
	"knobs.row.*.history",
	"knobs.row.*.edits",
	"branches.row.*",
	"branches.idle",
	"branches.filter.hot3",
	"branches.filter.hot2",
	"branches.hide_shared",
	"branches.hot.*",
	// System-page panels (templates/_metrics_panels.html).
	"panel.guard_metrics",
	"panel.guard_metrics.row.*",
	"panel.knobs",
	"panel.knobs.row.*",
	// Issue detail: dependency and shared-file rows.
	"detail.field.depends_on",
	"detail.field.depended_on_by",
	"detail.field.decided_by",
	"detail.field.shared_files",

	// Balance board (/p/<project>/balance). Groups are keyed by metric,
	// cards by metric and source slug, passes by number.
	"balance.stats",
	"balance.stat.notes",
	"balance.stat.open",
	"balance.stat.metrics",
	"balance.stat.unsorted",
	"balance.off",
	"balance.passes",
	"balance.pass.#",
	"balance.passes.planned",
	"balance.start",
	"balance.filter.*",
	"balance.empty",
	"balance.nomatch",
	"balance.group.*",
	"balance.group.*.count",
	"balance.card.*.*",
	"balance.card.*.*.state",
	"balance.card.*.*.foot",
	"balance.card.*.*.history",
	"balance.unsorted",
	"balance.unsorted.row.#",
	"balance.unsorted.row.#.more",
	"balance.toast",
}

// Segment turns a free-form value (a status, a slug) into one id segment:
// lower case, with spaces and dots replaced by `_`. Templates call it as
// `tid`.
func Segment(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer(" ", "_", ".", "_").Replace(s)
}

// segRe matches one concrete segment.
var segRe = `[^.]+`

// Regexp compiles an id or pattern (`*` any one segment, `#` digits) into an
// anchored regexp that matches concrete ids.
func Regexp(pattern string) *regexp.Regexp {
	parts := strings.Split(pattern, ".")
	for i, p := range parts {
		switch p {
		case "*":
			parts[i] = segRe
		case "#":
			parts[i] = `[0-9]+`
		default:
			parts[i] = regexp.QuoteMeta(p)
		}
	}
	return regexp.MustCompile(`^` + strings.Join(parts, `\.`) + `$`)
}

// Compatible reports whether two ids or patterns can name the same element:
// same segment count, and each segment pair is equal or one side is a
// wildcard (`*` matches anything, `#` matches digits or a wildcard).
func Compatible(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if !segCompatible(as[i], bs[i]) {
			return false
		}
	}
	return true
}

func segCompatible(x, y string) bool {
	if x == y || x == "*" || y == "*" {
		return true
	}
	if x == "#" {
		return y == "#" || isDigits(y)
	}
	if y == "#" {
		return isDigits(x)
	}
	return false
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Known reports whether id (concrete or a pattern) matches an entry in IDs.
func Known(id string) bool {
	for _, e := range IDs {
		if Compatible(e, id) {
			return true
		}
	}
	return false
}

// Suggest returns the listed entry closest to id (by edit distance), for
// "did you mean" hints. An entry's wildcard segments take id's segment at
// the same place, so `board.colum.backlog` is close to `board.column.*`.
// Returns "" when nothing is reasonably close.
func Suggest(id string) string {
	best, bestD := "", len(id)/2+2
	idSegs := strings.Split(id, ".")
	for _, e := range IDs {
		filled := e
		if segs := strings.Split(e, "."); len(segs) == len(idSegs) {
			for i, s := range segs {
				if s == "*" || s == "#" {
					segs[i] = idSegs[i]
				}
			}
			filled = strings.Join(segs, ".")
		}
		if d := distance(id, filled); d < bestD {
			best, bestD = e, d
		}
	}
	return best
}

func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// templateAttrRe finds data-testid="…" attributes; JS assignments of the
// form `.dataset.testid = '…'` are found by datasetRe.
var (
	templateAttrRe = regexp.MustCompile(`data-testid="([^"]*)"`)
	datasetRe      = regexp.MustCompile(`dataset\.testid\s*=\s*'([^']*)'(\s*\+)?`)
	goActionRe     = regexp.MustCompile(`\{\{[^}]*\}\}`)
	jsConcatRe     = regexp.MustCompile(`'\s*\+[^+]*\+\s*'`)
)

// ScanTemplate returns the ids a template source declares, with every
// dynamic part (a Go template action, a JS string concatenation) turned
// into `*`.
func ScanTemplate(src string) []string {
	var out []string
	for _, m := range templateAttrRe.FindAllStringSubmatch(src, -1) {
		id := goActionRe.ReplaceAllString(m[1], "*")
		id = jsConcatRe.ReplaceAllString(id, "*")
		out = append(out, id)
	}
	for _, m := range datasetRe.FindAllStringSubmatch(src, -1) {
		id := m[1]
		if m[2] != "" {
			id += "*"
		}
		out = append(out, id)
	}
	return out
}
