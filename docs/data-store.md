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
    { "id": 1, "description": "...", "status": "🔥 must-fix", "comment": "..." },
    { "id": 2, "description": "...", "status": "✅ resolved", "comment": "" }
  ]
}
```

`next_id` is a monotonic counter — `data remove` does not reuse ids. Writes are atomic (temp file + `fsync` + `rename`) so a crashed agent never leaves a half-written JSON file. There is **no locking**: two concurrent writers race, last-write-wins. The atomic rename only guarantees the file on disk is never corrupt, not that the read–modify–write was serialized.

## Placement marker in the body

Drop an HTML comment in the issue body to choose where the table renders:

```markdown
## Findings

<!-- data statuses=🔥 must-fix,👍 nice-to-have,✅ resolved,❌ wontfix -->
```

The renderer replaces the marker with the table. Without a marker the table renders below the body if entries exist, or not at all if entries are empty.

The `statuses=` attribute is the **per-issue dropdown menu** for the status column. Entries:

- Comma-separated; each entry is trimmed.
- Spaces and emojis are allowed inside an entry (`🔥 must-fix` is one entry, not two).
- Commas inside a status value are not supported in v1 (the comma is the separator).
- Omitted → defaults to `open, resolved`.

If a row's current status is not in the declared list (e.g. an old value, or a status set by an agent that did not know about the marker), the dropdown shows it anyway so it stays selectable until the human picks a new value. Only the **first** marker in the body is replaced; subsequent markers render literally as HTML comments.

## CLI

`issue-cli data` is the agent contract. Agents must not poke at the JSON file directly — the on-disk shape may change.

```bash
# Append a row, prints the assigned id on stdout
issue-cli data add <slug> --description "finding text" [--status "open"]

# Read entries (table by default, JSON with --json)
issue-cli data list <slug>
issue-cli data list <slug> --json

# Mutate an entry
issue-cli data set-status  <slug> <id> "✅ resolved"
issue-cli data set-comment <slug> <id> --text "fixed in commit 8a1c2e0"

# Delete an entry (id is not reused)
issue-cli data remove <slug> <id>
```

The CLI prints the new id on stdout (and a human line on stderr) so agents can pipe into other commands:

```bash
id=$(issue-cli data add <slug> --description "finding")
issue-cli data set-comment <slug> "$id" --text "looked into it"
```

## API

The web UI's row actions hit the same tracker functions as the CLI. Routes live under the project prefix (`/p/<project-slug>`):

| Method | Path                              | Description                                                  |
|:-------|:----------------------------------|:-------------------------------------------------------------|
| POST   | `/issue/<slug>/data`              | Add an entry. Body: `{description, status}`. Returns `{id}`. |
| POST   | `/issue/<slug>/data/<id>/status`  | Set status. Body: `{status}`.                                |
| POST   | `/issue/<slug>/data/<id>/comment` | Set comment. Body: `{comment}`.                              |
| DELETE | `/issue/<slug>/data/<id>`         | Remove entry. NextID is unchanged.                           |

400 on missing description, 404 on unknown issue or unknown entry id.

## UI

The detail view scans the rendered body for the first `<!-- data ... -->` comment, replaces it with the table HTML, then runs the existing `linkIssueRefs` post-processing. With no marker, the table is appended after the body when entries exist.

Each row exposes:

- A status `<select>` populated from the marker statuses (plus the row's current status if not declared) — `onchange` posts to the status endpoint.
- A `contenteditable` comment cell — `onblur` posts to the comment endpoint when the value changed.
- A `×` remove button — `onclick` confirms then DELETEs.

A toast confirms each save (or surfaces the error). Embedded templates and CSS do not hot-reload, so a server restart is required after rebuilding to pick up changes.

## Implementation pointers

- `internal/tracker/data.go` — `DataStore`, `DataEntry`, `LoadData`, `SaveData`, `AddEntry`, `SetEntryStatus`, `SetEntryComment`, `RemoveEntry`, `ParseDataMarker`, `ResolveDataStatuses`.
- `cmd/issue-cli/main.go` — `case "data":` in the top-level switch dispatches to `runDataAdd` / `runDataList` / `runDataSetStatus` / `runDataSetComment` / `runDataRemove`.
- `handlers.go` — `handleDataAdd` / `handleDataSetStatus` / `handleDataSetComment` / `handleDataRemove`; `renderDataTable` builds the inline HTML; `renderBodyWithDataTable` does the marker substitution.
- `templates/detail.html` — `dataSetStatus`, `dataSetComment`, `dataRemove` row-action JS.
- `static/style.css` — `.data-table-wrap` / `.data-table` styles.

## Out of scope (v1)

- Cross-issue querying (`data query 'status=open'` for board-level rollups).
- Multiple tables per issue.
- Richer columns (`severity`, `file`, `link`) — the schema is strictly `{id, description, status, comment}`.
- Locking / optimistic concurrency.
- Frontmatter-based config — the status list lives only on the marker.
