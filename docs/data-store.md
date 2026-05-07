---
title: "Per-issue Data Store"
order: 7
---

Every issue can carry a small structured data store next to its markdown body — a table of `{id, description, status, comment}` rows persisted as a sidecar JSON file. The motivating use case is **agent code-review findings**: an agent runs a review, drops findings into the table via `issue-cli data add`, and the human triages inline on the detail page (change status from a dropdown, edit comments inline) without rewriting the issue body.

## Sidecar file

The sidecar lives next to the issue's `.md` file:

```
issues/Backend/10.md         ← issue body
issues/Backend/10.data.json  ← structured data
```

A missing sidecar is **not an error** — it means "no entries". The first `issue-cli data add` (or first `POST /issue/<slug>/data`) creates it.

Schema (v1):

```json
{
  "next_id": 6,
  "entries": [
    { "id": 1, "description": "...", "status": "🔥 must-fix", "tier": "🔴 critical", "comment": "..." },
    { "id": 2, "description": "...", "status": "✅ resolved", "comment": "" }
  ]
}
```

`tier` is the optional **second axis** (e.g. critical / nice, S1 / S2 / S3,
high / medium / low). It is `omitempty` in the JSON: entries that don't set
a tier — including every entry written before this field existed — keep the
old shape on disk and load as `Tier: ""`.

`next_id` is a monotonic counter — `data remove` does not reuse ids. Writes are atomic (temp file + `fsync` + `rename`) so a crashed agent never leaves a half-written JSON file. There is **no locking**: two concurrent writers race, last-write-wins. The atomic rename only guarantees the file on disk is never corrupt, not that the read–modify–write was serialized.

## Placement marker in the body

Drop an HTML comment in the issue body to choose where the table renders:

```markdown
## Findings

<!-- data statuses=🔥 must-fix,👍 nice-to-have,✅ resolved,❌ wontfix tiers=🔴 critical,🟢 nice -->
```

The renderer replaces the marker with the table. Without a marker the table renders below the body if entries exist, or not at all if entries are empty.

The marker accepts two attributes; both are optional and may appear in any order:

- `statuses=` — the **status dropdown** menu (review state).
- `tiers=` — the **tier dropdown** menu, an optional orthogonal second axis (priority / severity / blast-radius).

Both follow the same value rules:

- Comma-separated; each entry is trimmed.
- Spaces and emojis are allowed inside an entry (`🔥 must-fix` is one entry, not two).
- Commas inside a value are not supported (the comma is the separator).
- `statuses=` omitted → defaults to `open, resolved`. `tiers=` omitted → tier UI (column, filter, dropdown) is **hidden entirely**.

If a row's current status (or tier) is not in the declared list, the dropdown shows it anyway so it stays selectable until the human picks a new value. Only the **first** marker in the body is replaced; subsequent markers render literally as HTML comments.

### Why generic tiers, not "priority"

Different workflows want different second axes — ADR review (critical / nice), bug triage (S1 / S2 / S3), operational (high / medium / low blast-radius). A free-form configurable enum mirrors how `statuses=` already works, without baking domain semantics into the engine.

## CLI

`issue-cli data` is the agent contract. Agents must not poke at the JSON file directly — the on-disk shape may change.

```bash
# Append a row, prints the assigned id on stdout
issue-cli data add <slug> --description "finding text" [--status "open"] [--tier "🔴 critical"]

# Read entries (table by default, JSON with --json)
# Output includes a <tier> column when the body declares tiers= or any entry has one set.
issue-cli data list <slug>
issue-cli data list <slug> --json

# Mutate an entry
issue-cli data set-status  <slug> <id> "✅ resolved"
issue-cli data set-tier    <slug> <id> "🔴 critical"   # pass "" to clear
issue-cli data set-comment <slug> <id> --text "fixed in commit 8a1c2e0"

# Delete an entry (id is not reused)
issue-cli data remove <slug> <id>
```

`--tier` (and `set-tier`) reject values that aren't declared in the body's `tiers=` marker, so typos like `🔴 critcal` fail loudly. When the marker omits `tiers=`, any value is accepted — the workflow is opting out of validation.

The CLI prints the new id on stdout (and a human line on stderr) so agents can pipe into other commands:

```bash
id=$(issue-cli data add <slug> --description "finding")
issue-cli data set-comment <slug> "$id" --text "looked into it"
```

## API

The web UI's row actions hit the same tracker functions as the CLI. Routes live under the project prefix (`/p/<project-slug>`):

| Method | Path                              | Description                                                          |
|:-------|:----------------------------------|:---------------------------------------------------------------------|
| POST   | `/issue/<slug>/data`              | Add an entry. Body: `{description, status, tier?}`. Returns `{id}`.  |
| POST   | `/issue/<slug>/data/<id>/status`  | Set status. Body: `{status}`.                                        |
| POST   | `/issue/<slug>/data/<id>/tier`    | Set tier (pass empty string to clear). Body: `{tier}`.               |
| POST   | `/issue/<slug>/data/<id>/comment` | Set comment. Body: `{comment}`.                                      |
| DELETE | `/issue/<slug>/data/<id>`         | Remove entry. NextID is unchanged.                                   |

400 on missing description, 404 on unknown issue or unknown entry id.

## UI

The detail view scans the rendered body for the first `<!-- data ... -->` comment, replaces it with the table HTML, then runs the existing `linkIssueRefs` post-processing. With no marker, the table is appended after the body when entries exist.

Each row exposes:

- A status `<select>` populated from the marker statuses (plus the row's current status if not declared) — `onchange` posts to the status endpoint.
- A tier `<select>` (rendered only when the marker declares `tiers=`) with an empty `—` option that clears the tier — `onchange` posts to the tier endpoint. When tiers are configured the status and tier selects share a single **Status / Tier** column, stacked vertically, so the second axis costs no extra horizontal space. With no tiers marker the column reverts to plain **Status**.
- A `contenteditable` comment cell — `onblur` posts to the comment endpoint when the value changed. Long comments truncate with `text-overflow: ellipsis` and a hover `title`; clicking into the cell expands to full content for editing.
- A `×` remove button — `onclick` confirms then DELETEs.

A toolbar above the table provides view preferences, persisted per-user in localStorage:

- **Status filter** — dropdown listing every status currently in use; selecting one hides non-matching rows. Key: `dataTable.statusFilter`.
- **Tier filter** — same shape as the status filter but for the second axis; only rendered when the marker has `tiers=`. Composes with the status filter (AND). Key: `dataTable.tierFilter`.
- **↔ Expand / ↔ Shrink** — toggles a `.wide` class that lets the table spill rightward under the metadata sidebar (negative margin equal to the sidebar + gap). Default is shrunk; the breakout is automatically disabled below the 768px viewport breakpoint. Key: `dataTable.wide`.

The table itself uses `table-layout: fixed` so column widths are stable and a long unbroken string in any cell does not steal width from siblings (in particular, the status column always renders the full label).

A toast confirms each save (or surfaces the error). Embedded templates and CSS do not hot-reload, so a server restart is required after rebuilding to pick up changes.

## Implementation pointers

- `internal/tracker/data.go` — `DataStore`, `DataEntry` (with `Tier`), `LoadData`, `SaveData`, `AddEntry` / `AddEntryWithTier`, `SetEntryStatus`, `SetEntryTier`, `SetEntryComment`, `RemoveEntry`, `ParseDataMarker`, `ResolveDataStatuses`, `ResolveDataTiers`.
- `cmd/issue-cli/cmd_data.go` — `runDataAdd` (`--tier`), `runDataList` (tier column), `runDataSetStatus`, `runDataSetTier`, `runDataSetComment`, `runDataRemove`. `validateTier` enforces the body's `tiers=` enum.
- `handlers_data.go` — `handleDataAdd` / `handleDataSetStatus` / `handleDataSetTier` / `handleDataSetComment` / `handleDataRemove`. `handlers_detail.go::renderDataTable` builds the inline HTML (Tier column conditional on `len(tiers) > 0`); `renderBodyWithDataTable` does the marker substitution.
- `templates/detail.html` — `dataSetStatus`, `dataSetTier`, `dataSetComment`, `dataRemove` row-action JS; `dataTableInit`, `dataTableFilter`, `dataTableTierFilter`, `dataTableApplyFilters`, `dataTableToggleWide` for toolbar/persistence.
- `static/style.css` — `.data-table-wrap`, `.data-table`, `.data-table.has-tier .data-status`, `.data-table .data-tier`, `.data-table-tier-filter` styles.

## Out of scope (v1)

- Cross-issue querying (`data query 'status=open'` for board-level rollups).
- Multiple tables per issue.
- More than two axes — the schema is strictly `{id, description, status, tier, comment}`.
- Locking / optimistic concurrency.
- Frontmatter-based config — the enum lists live only on the marker.
