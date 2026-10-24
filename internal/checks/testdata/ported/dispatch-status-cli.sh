#!/usr/bin/env bash
# issue: cli/issue-cli-dispatch-issue-cli-status-let-agents-use-the-viewer-without-raw-http-calls
# The owner's shell-side checks (TP4, TP5, TP7 in two parts, TP8, TP9). Everything runs in a sandbox_dir:
# issue-viewer and issue-cli built from this branch, a projects.yaml with viewer_url, its own board on a free port,
# its own viewer.json (XDG_STATE_HOME) and a private tmux server. Every issue-cli call and the board run under
# clean_shell, so the only claude any of them can start is the stand-in (TP9). TP6 (the ▶ banner) stays in
# `make playtest`.
# shellcheck source=/dev/null
source "${CHECKS_LIB:?run me with: issue-cli check-script <slug>}"
checks_intro "issue-cli dispatch and issue-cli status replace the raw HTTP calls leads made to the viewer, dispatch refuses" \
	"an issue that only waits on your approval, and every link follows viewer_url. Five steps, about 3 minutes;" \
	"TP9 starts a worker (a stand-in for Claude) in the sandbox and asks you to look at it in a second terminal." \
	"" \
	"Needs: go, tmux, curl, python3. Your viewer on :8080, your viewer.json and your tmux sessions are never touched."

cd "$CHECKS_ROOT" || exit 2
sandbox_dir
tmp=$SANDBOX
private_tmux
bin=$tmp/bin
proj=$tmp/proj
cfg=$tmp/projects.yaml
mkdir -p "$bin" "$proj/issues" "$tmp/state"
export XDG_STATE_HOME=$tmp/state ISSUE_VIEWER_CONFIG=$cfg
unset TMUX_PANE ISSUE_VIEWER_URL ISSUE_VIEWER_ISSUE_SLUG ISSUE_VIEWER_LEAD_SCOPE ISSUE_VIEWER_PROJECT ISSUE_CLI_LOG

# The stand-in Claude shows its briefing (the last line is what "Prompt delivered" looks for), notes the folder
# it started in, and then keeps reading what is pasted into it, so a re-sent briefing lands in claude.input
# instead of a shell prompt.
stand_in claude "for a; do :; done
printf '%s\n' \"\$a\"
pwd > $(printf %q "$tmp/claude.cwd")
exec cat >> $(printf %q "$tmp/claude.input")"

viewer_pid=
stop_viewer() {
	[ -n "$viewer_pid" ] && kill "$viewer_pid" 2> /dev/null
	viewer_pid=
}
on_exit stop_viewer

echo "Sandbox: $tmp (deleted at the end)."
echo "cli = $bin/issue-cli --config $cfg --project sand (run from $tmp, under clean_shell)"
echo
echo "Building issue-viewer and issue-cli from this branch…"
go build -o "$bin/issue-viewer" . && go build -o "$bin/issue-cli" ./cmd/issue-cli || {
	echo "build failed"
	exit 2
}

port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
base=http://127.0.0.1:$port

# A project with a gate: backlog → in progress is free, in progress → testing needs your approval. try-dispatch
# (backlog) can be dispatched; gated (in progress, nothing else to do) only waits on you.
cat > "$cfg" << EOF
viewer_url: "$base"
projects:
  - name: Sand
    slug: sand
    issues: $proj/issues
    workdir: $proj
    workflow: $proj/workflow.yaml
    terminal: "none"
EOF
cat > "$proj/workflow.yaml" << 'EOF'
worktree: true
statuses:
  - name: "backlog"
  - name: "in progress"
  - name: "testing"
  - name: "done"
transitions:
  - from: "backlog"
    to: "in progress"
  - from: "in progress"
    to: "testing"
    actions:
      - type: "require_human_approval"
        status: "testing"
EOF
printf -- '---\ntitle: "Try dispatch"\nstatus: "backlog"\n---\n\nA sandbox issue. Say hi in one line, then stop and wait.\n' > "$proj/issues/try-dispatch.md"
printf -- '---\ntitle: "Gated"\nstatus: "in progress"\n---\n\nAll done; only waits on the owner.\n' > "$proj/issues/gated.md"
git -C "$proj" init -q -b main && git -C "$proj" add -A && git -C "$proj" -c user.name=check -c user.email=check@example.invalid commit -q -m init

# The board runs under clean_shell too: if its ▶ endpoint ever started an agent, it would get the stand-in.
clean_shell "$bin/issue-viewer" -config "$cfg" -port "$port" > "$tmp/viewer.log" 2>&1 &
for _ in $(seq 1 40); do command curl -s -o /dev/null "$base/" && break; sleep 0.25; done
viewer_pid=$(pgrep -f -- "^$bin/issue-viewer -config" | head -n 1)
command curl -s -o /dev/null "$base/" || {
	echo "the sandbox board did not start:"
	cat "$tmp/viewer.log"
	exit 2
}
echo "Sandbox board: $base (pid $viewer_pid). Its startup lines:"
sed 's/^/    /' "$tmp/viewer.log"
pause

# cli: the branch's issue-cli on the sandbox config and project, from the sandbox folder (under .worktrees/
# issue-cli would take you for a worker, and workers can't dispatch), without your profile, stand-ins first.
cli() { (cd "$tmp" && clean_shell "$bin/issue-cli" --config "$cfg" --project sand "$@"); }
# post <path> <json>: the HTTP status of a POST to the sandbox board.
post() { command curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d "$2" "$base$1"; }
# same <path> <cli args...>: endpoint body and cli output match, once more if a minute ticked over; status.json's
# as_of is the moment of each request, so it is masked.
same() {
	local p=$1
	shift
	for _ in 1 2; do
		command curl -s "$base$p" | sed 's/"as_of":"[^"]*"/"as_of":…/' > "$tmp/want"
		cli "$@" 2>&1 | sed 's/"as_of":"[^"]*"/"as_of":…/' > "$tmp/got"
		cmp -s "$tmp/want" "$tmp/got" && return 0
	done
	diff "$tmp/want" "$tmp/got" | head -5 | sed 's/^/    /'
	return 1
}
# eventually <cmd...>: true once <cmd> succeeds, trying for up to 5 s (the stand-in writes after dispatch returns).
eventually() {
	for _ in $(seq 1 20); do
		"$@" && return 0
		sleep 0.25
	done
	return 1
}
# What the stand-in noted: the folder it started in, and what was pasted into it.
worker_started_in() { [ "$(cat "$tmp/claude.cwd" 2> /dev/null)" = "$1" ]; }
worker_got() { grep -qF -- "$1" "$tmp/claude.input" 2> /dev/null; }
board_down() { ! command curl -s -o /dev/null "$base/"; }

# ── TP7 ──────────────────────────────────────────────────────────────
step TP7 "issue-cli status prints the viewer's stand-up" \
	--what "issue-cli status against the sandbox board, compared byte for byte with GET /p/sand/status.md; --json with /status.json; a slug by mistake." \
	--why "leads read the stand-up with curl today; one issue-cli permission should cover it, with no host typed." \
	--look "the stand-up below and the three checks." \
	--expect "the same text as the endpoint, the same JSON, and status <slug> pointing at issue-cli show (exit 2)."
run cli status
expect '[ $code = 0 ] && [[ $out == "# Sand status"* ]]' "status printed the stand-up (exit 0)"
expect 'same /p/sand/status.md status' "byte-identical to GET /p/sand/status.md"
expect 'same /p/sand/status.json status --json' "status --json is GET /p/sand/status.json (as_of aside: it is each request's time)"
run cli status gated
expect '[ $code = 2 ] && [[ $out == *"issue-cli --project sand show gated"* ]]' "a slug is a usage error that points at show (exit 2)"

# ── TP8 ──────────────────────────────────────────────────────────────
step TP8 "Links follow viewer_url" \
	--what "the sandbox projects.yaml sets viewer_url: $base. show's Timeline link, an approval link, and projects --json." \
	--why "changing the port or hostname used to mean editing every brief and permission file; now it is one key." \
	--look "the URLs printed." \
	--expect "every URL starts with $base, never localhost:8080."
run cli show gated
expect '[[ $out == *"Timeline: $base/p/sand/issue/gated?tab=timeline"* ]]' "show links the Timeline on the sandbox board"
run cli --json projects
expect '[[ $out == *"\"viewer_url\": \"$base\""* ]]' "projects --json names viewer_url (what the issue-approve mod reads)"
expect '[[ $out != *"localhost:8080"* ]]' "nothing points at localhost:8080"

# ── TP4 ──────────────────────────────────────────────────────────────
step TP4 "Dispatch refuses an issue that only waits on you" \
	--what "issue-cli dispatch gated: in progress → testing needs only your approval. Then the same through ▶'s endpoint." \
	--why "a worker started there would only claim the issue and stop at the same gate." \
	--look "the message: does it say why, where to approve, and how to dispatch anyway?" \
	--expect "exit 3, the approve link on $base, the --force command, POST 409, no agent-gated session."
run cli dispatch gated --quote "check"
expect '[ $code = 3 ]' "exit 3: waiting on a gate"
expect '[[ $out == *"A human approves at: $base/p/sand/issue/gated#approve-testing"* ]]' "the approve link is on the sandbox board"
expect '[[ $out == *"issue-cli --project sand dispatch gated --force"* ]]' "it names the --force command"
run post /p/sand/issue/gated/dispatch '{"agent":"claude"}'
expect '[ "$out" = 409 ]' "▶'s endpoint answers 409"
expect '! ptmux has-session -t agent-gated 2> /dev/null' "no session was started"
ask "Does the refusal read clearly: why nothing started, where to approve, how to force?"

# ── TP9 ──────────────────────────────────────────────────────────────
step TP9 "A worker from issue-cli dispatch, then --reprompt" \
	--what "issue-cli dispatch try-dispatch starts the Claude stand-in in the issue's worktree on the private tmux server, with no viewer call; --reprompt pastes the briefing into it again." \
	--why "this is what a lead runs instead of curl; the board's ▶ starts the same spec." \
	--look "the steps and the two commands below, then the worker's session in a second terminal." \
	--expect "\"✓ Dispatched agent-try-dispatch\", Prompt delivered, Inside tmux / From a shell lines; the stand-in in $proj/.worktrees/try-dispatch with its briefing."
echo "  Starting the worker (takes a few seconds while the prompt is delivered)…"
run cli dispatch try-dispatch --quote "check"
expect '[ $code = 0 ] && [[ $out == *"✓ Dispatched agent-try-dispatch for try-dispatch"* ]]' "dispatched (exit 0)"
expect '[[ $out == *"Prompt delivered"* ]]' "the briefing reached the worker (no trust question with the stand-in)"
expect '[[ $out == *"From a shell: TMUX_TMPDIR="* ]]' "it prints how to attach (on the private server)"
expect '[ -d "$proj/.worktrees/try-dispatch" ]' "the issue's worktree exists"
expect_stand_in_ran claude "the stand-in claude ran, not the real one"
expect 'eventually worker_started_in "$proj/.worktrees/try-dispatch"' "the worker started in $proj/.worktrees/try-dispatch"
echo
echo "  In a second terminal, attach (detach again with Ctrl-b d; don't close the worker):"
echo "    TMUX_TMPDIR=$CHECKS_TMUX_DIR tmux attach -t agent-try-dispatch"
ask "Is the worker running in $proj/.worktrees/try-dispatch, briefed with \"You have been assigned this issue: try-dispatch\"?"
run cli dispatch try-dispatch --reprompt --quote "check"
expect '[ $code = 0 ] && [[ $out == *"Re-sent the briefing to agent-try-dispatch"* ]]' "--reprompt re-sent the briefing (exit 0)"
expect 'eventually worker_got "You have been assigned this issue: try-dispatch"' "the re-sent briefing reached the worker as new input"
ask "In the attached session, did the briefing arrive again as a new message?"

# ── TP5 ──────────────────────────────────────────────────────────────
step TP5 "Dispatch onto a live worker re-attaches" \
	--what "issue-cli dispatch try-dispatch again, while the worker from TP9 runs." \
	--why "dispatch must never start a second agent on the same issue." \
	--look "the first line and the session list." \
	--expect "\"Already live: agent-try-dispatch\", exit 0, the attach lines, and still one agent-try-dispatch session."
run cli dispatch try-dispatch --quote "check"
expect '[ $code = 0 ] && [[ $out == *"Already live: agent-try-dispatch"* ]]' "re-attached (exit 0)"
expect '[[ $out == *"Inside tmux:  tmux switch-client -t agent-try-dispatch"* ]]' "it prints the switch command"
run ptmux list-sessions -F '#{session_name}'
expect '[ "$(grep -c "^agent-try-dispatch$" <<< "$out")" = 1 ]' "still exactly one agent-try-dispatch session"

# ── TP7, part two ────────────────────────────────────────────────────
step TP7 "status when the viewer is down" \
	--what "the sandbox board is stopped, then issue-cli status." \
	--why "an agent must see which address it tried, not a bare connection error." \
	--look "the error." \
	--expect "exit 1, \"viewer not reachable at $base/p/sand/status.md\", and the viewer_url hint."
stop_viewer
eventually board_down
run cli status
expect '[ $code = 1 ] && [[ $out == *"viewer not reachable at $base/p/sand/status.md"* ]] && [[ $out == *"viewer_url"* ]]' "exit 1 with the URL it tried and the viewer_url hint"

summary
rc=$?
echo "TP6 (the ▶ banner) is checked in make playtest; after it, approve \"documentation\" in the issue viewer when you're happy."
exit "$rc"
