#!/usr/bin/env bash
# issue: workflow/issue-cli-prove-link-each-criterion-to-its-proof
# Ten steps (S1-S10) run this branch's issue-cli, built into a sandbox_dir, on a throwaway project: its own
# projects.yaml, git repo, issues and one playtest run. Needs go and git; no viewer, no tmux, never the real
# projects.yaml or issues tree.
#
# Boxes: the issue's Manual boxes are TP2 (sandbox) and TP3 (human steps) and the original ticked both only when
# every step passed. So every step names --box TP2 (TP2 passes only when all ten do) and `summary --all TP3`
# adds TP3 on the same condition.
# shellcheck source=/dev/null
source "${CHECKS_LIB:?run me with: issue-cli check-script <slug>}"
checks_intro "issue-cli prove links each acceptance criterion to its proof, and the testing → human-testing gate" \
	"requires every criterion to be proven. These ten steps run the branch's issue-cli on a throwaway project:" \
	"links found with no prove call, the gate, ticking, staleness, refusals, the exemption for older issues," \
	"and the upgrade offer for other projects. About 3 minutes; everything is removed when it ends."

cd "$CHECKS_ROOT" || exit 2
sandbox_dir tmp
# shellcheck disable=SC2154 # sandbox_dir sets $tmp through printf -v
proj=$tmp/proj
issues=$tmp/issues
ic_bin=$tmp/bin/issue-cli

export GIT_AUTHOR_NAME=sandbox GIT_AUTHOR_EMAIL=sandbox@example.com GIT_COMMITTER_NAME=sandbox \
	GIT_COMMITTER_EMAIL=sandbox@example.com GIT_CONFIG_GLOBAL=/dev/null TMPDIR=$tmp
g() { /usr/bin/git -C "$proj" "$@"; }
# ic: the branch's issue-cli on the sandbox project
ic() { "$ic_bin" --config "$tmp/projects.yaml" --project sandbox "$@"; }

# issue <file> <created> <title>: an issue in testing with three criteria, one visual
issue() {
	cat > "$issues/Demo/$1.md" << MD
---
title: "$3"
status: "testing"
system: "Demo"
type: "feature"
created: "$2"
---

A badge on each card shows how many comments it has.

## Acceptance Criteria
- [ ] The badge shows the comment count
- [ ] The count updates when a comment is added
- [ ] The badge reads at a glance (visual)

## Test Plan
### Automated
- [x] Automated verification recorded

### Manual
- [x] Throwaway sandbox scenario prepared
- [x] Human steps written

## Testing
- [x] Relevant tests for changed code passing
MD
}

# sandbox: this repo's workflow.yaml (gate since 2026-10-24, playtest runner), demo/count-badge (created after
# the gate) and demo/old-issue (before it), a work branch with two Proves: trailers, and a playtest run whose
# step TP1 proves AC3.
sandbox() {
	mkdir -p "$tmp/bin" "$proj" "$issues/Demo" || return 1
	go build -o "$ic_bin" ./cmd/issue-cli || return 1
	/usr/bin/cp workflow.yaml "$proj/workflow.yaml"
	cat > "$tmp/projects.yaml" << YAML
projects:
  - name: "Sandbox"
    slug: "sandbox"
    issues: "$issues"
    workflow: "$proj/workflow.yaml"
    workdir: "$proj"
YAML
	issue count-badge 2026-10-25 "count badge"
	issue old-issue 2026-10-20 "old issue"
	g init -q -b main && printf '.playtest/\n.agent-logs/\n' > "$proj/.gitignore" && g add -A && g commit -q -m init || return 1
	g checkout -q -b work/demo/count-badge
	printf 'package badge\n\nfunc TestBadgeCount(t *testing.T) {}\n' > "$proj/badge_test.go"
	g add -A && g commit -q -m "test the badge count" -m "Proves: AC1 TestBadgeCount"
	printf 'package badge\n' > "$proj/badge.go"
	g add -A && g commit -q -m "badge updates on new comments" -m "Proves: AC2"
	g checkout -q main
	mkdir -p "$proj/.playtest/runs/badge/20261025-100000-auto"
	cat > "$proj/.playtest/runs/badge/20261025-100000-auto/results.yaml" << YAML
session: badge
issue: demo/count-badge
mode: auto
started: "2026-10-25T10:00:00Z"
steps:
  - id: TP1
    status: pass
    proves: [AC3]
YAML
	echo "sandbox at $tmp"
}

step S1 "Build the branch and the sandbox" --box TP2 \
	--what "builds issue-cli from this branch and makes the throwaway project in a temp folder." \
	--why "every later step runs the branch's binary on fake data, never your real projects." \
	--look "the build output and the folder it names." \
	--expect "\"sandbox at /tmp/ck-…\", exit 0."
run sandbox
expect '[ $code = 0 ] && [ -x "$ic_bin" ]' "the branch's issue-cli built and the sandbox exists"
expect 'g rev-parse -q --verify work/demo/count-badge > /dev/null' "the work branch work/demo/count-badge exists"

step S2 "Links are found where the proof was written" --box TP2 \
	--what "prove --list on demo/count-badge. Nobody ran prove: the links come from two commit trailers and a playtest step's proves: line." \
	--why "agents shouldn't have to call a command per criterion; they declare the proof where they write it (D-5)." \
	--look "each criterion's line, its links (where each came from) and their checks." \
	--expect "AC1 UNPROVEN (test TestBadgeCount — no tests: report, with the comment command), AC2 proven (commit, trailer), AC3 proven (step TP1, proves: in session badge), the four ways to prove shown once, AC2/AC3 ticked, \"2/3 criteria proven\"."
run ic prove demo/count-badge
expect '[[ $out == *"AC1 [ ] UNPROVEN"* && $out == *"no tests: report"* ]]' "AC1's test is found but has no tests: report"
expect '[[ $out == *"AC2 [x] proven"* && $out == *"(trailer) — found"* ]]' "AC2 is proven by its commit trailer"
expect '[[ $out == *"AC3 [x] proven"* && $out == *"step TP1 (proves: in session badge) — passed"* ]]' "AC3 is proven by the playtest step"
expect '[[ $out == *"2/3 criteria proven"* ]]' "2 of 3 proven"
ask "Is it clear from this output what proves each criterion and what AC1 still needs?"

step S3 "A criterion can't be ticked by hand" --box TP2 \
	--what "issue-cli check demo/count-badge AC1 on an issue the gate covers." \
	--why "a ticked criterion should mean proven, not \"the agent said so\" (D-2)." \
	--look "the error." \
	--expect "refused (exit 1): \"acceptance criteria are ticked by their proof, not by hand: AC1\", pointing at prove --list."
run ic check demo/count-badge AC1
expect '[ $code != 0 ] && [[ $out == *"ticked by their proof, not by hand: AC1"* ]]' "check refused AC1"

step S4 "The gate names what is missing" --box TP2 \
	--what "transition --dry-run to human-testing: every unmet requirement, nothing changed." \
	--why "the gate's error is how agents learn the feature, so it must say what's wrong and how to fix it." \
	--look "the second requirement's text." \
	--expect "2 unmet: the missing tests: comment, and \"1 of 3 acceptance criteria are not proven\" with AC1's failing link and the ways to prove it."
run ic transition demo/count-badge --to human-testing --dry-run
expect '[[ $out == *"2 unmet requirement"* && $out == *"1 of 3 acceptance criteria are not proven"* ]]' "both requirements listed"
ask "Would an agent know from this message what to do next?"

step S5 "A tests: report proves the test, and the gate passes" --box TP2 \
	--what "posts \"tests: pass\", then runs the real transition to human-testing." \
	--why "the test link passes once the agent reports after its last commit; the transition ticks what is proven." \
	--look "the transition output." \
	--expect "\"✓ Ticked AC1 (proven)\" and \"✓ testing → human-testing\"."
run ic comment demo/count-badge --text "tests: pass"
run ic transition demo/count-badge --to human-testing
expect '[ $code = 0 ] && [[ $out == *"Ticked AC1"* && $out == *"testing → human-testing"* ]]' "AC1 ticked and the issue moved"

step S6 "New code makes the report stale" --box TP2 \
	--what "adds a commit to the work branch, then prove --list." \
	--why "a tests: comment older than the code doesn't prove the code." \
	--look "AC1's test link." \
	--expect "\"stale (code changed since the last tests: comment)\" with \"re-run the tests\"."
sleep 1
g checkout -q work/demo/count-badge && printf 'package badge\n// tweak\n' > "$proj/badge.go" && g add -A && g commit -q -m "tweak the badge" && g checkout -q main
run ic prove demo/count-badge
expect '[[ $out == *"stale (code changed since the last tests: comment)"* ]]' "AC1's test is stale"

step S7 "No hand-picked screenshots where a playtest runner exists" --box TP2 \
	--what "prove AC3 --shot x.png in a project with the playtest-sessions feature (the sandbox copies this repo's workflow.yaml)." \
	--why "screenshots come from playtest runs, one per step; no 20 shots per change (owner, 2026-10-22)." \
	--look "the error." \
	--expect "refused: \"this project has a playtest runner\", pointing at proves: on the step."
run ic prove demo/count-badge AC3 --shot x.png
expect '[ $code != 0 ] && [[ $out == *"this project has a playtest runner"* ]]' "--shot refused"

step S8 "Older issues are exempt from the gate" --box TP2 \
	--what "check demo/old-issue AC1; the issue was created 2026-10-20, before the gate's date (2026-10-24)." \
	--why "issues already in flight keep working as before (D-3)." \
	--look "the check output." \
	--expect "\"✓ Checked: AC1\"."
run ic check demo/old-issue AC1
expect '[ $code = 0 ] && [[ $out == *"Checked: AC1"* ]]' "AC1 ticked by hand on the old issue"

step S9 "A measured value proves a criterion" --box TP2 \
	--what "prove demo/old-issue AC2 --value \"3 badges\"." \
	--why "a value counts on its own, shown as reported (D-1); prove ticks the box." \
	--look "AC2's line and the last lines." \
	--expect "AC2 proven with value \"3 badges\" (prove) — reported, \"Ticked AC2\", and a note that the gate doesn't cover this issue."
run ic prove demo/old-issue AC2 --value "3 badges"
expect '[[ $out == *"AC2 [x] proven"* && $out == *"— reported"* ]]' "AC2 proven by the value"
expect '[[ $out == *"gate doesn'"'"'t cover this issue"* ]]' "the exemption is explained"

step S10 "Other projects are offered the feature" --box TP2 \
	--what "workflow upgrade on a copy of main's workflow.yaml (which doesn't have the feature yet)." \
	--why "RogueTech, Eldritch and the rest adopt it through workflow upgrade (D-4), with today's date so their in-flight issues are exempt." \
	--look "the proof-links proposal and its snippet." \
	--expect "a proof-links proposal whose snippet has rule: \"criteria_proven: since $(date +%F)\"."
mkdir -p "$tmp/upgrade" && /usr/bin/git show main:workflow.yaml > "$tmp/upgrade/workflow.yaml"
run bash -c "cd '$tmp/upgrade' && '$ic_bin' workflow upgrade | sed -n '/proof-links/,/Ask:/p;/criteria_proven/p'"
expect '[[ $out == *"proof-links"* && $out == *"criteria_proven: since $(date +%F)"* ]]' "proof-links proposed from today"

summary --all TP3
rc=$?
if [ "$rc" = 0 ] && [ "$CHECKS_MODE" = owner ]; then
	echo "Approve \"documentation\" in the issue viewer when you're happy."
fi
exit "$rc"
