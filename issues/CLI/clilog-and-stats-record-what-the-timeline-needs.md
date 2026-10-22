---
title: "clilog and stats record what the timeline needs"
status: "idea"
system: "CLI"
priority: "high"
type: "feature"
version: "0.54"
parent: "ui/issue-page-tabs-issue-timeline-evidence"
created: "2026-10-22"
---

Agents already run the commands at the right moments; each can record one more field with no prompt change.
From the data analysis (`analysis` on the epic):

1. **Exit code in the clilog.** `logAction` (`cmd/issue-cli/main.go`) writes the record before the call runs, so it
   has args but no result. Telemetry computes `exit`, `err_class` and `dur_ms` after the call but stores no values.
   Write the clilog record after the call (or a second line) with exit, error class and duration, so one record
   shows a refused gate with its args.
2. **`waiting_since` per gate.** When `transition` (or `--dry-run`) fails with a missing approval, store the time in
   the stats sidecar. "Waiting on you" then runs from that time to `approved_at` instead of leaning on the wording of
   a `progress:` comment.
3. **Branch head and merge sha.** Store the work branch head at each transition, and the merge sha at
   `shipping → done`. Commits, the change summary and "CI after the merge" keep working after the branch is deleted.
4. **Comment time.** `AddComment` stores `date` as `2006-01-02`. Store a full timestamp (keep reading old dates).
5. **`dispatched_at`.** The viewer's dispatch (`internal/agentrun.Start`) writes it into the stats sidecar.

Keep the clilog's location question open: the `$TMPDIR` copy is lost on reboot, and the timeline now depends on the
per-agent copy under `.agent-logs/`.
