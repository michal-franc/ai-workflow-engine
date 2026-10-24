#!/usr/bin/env bash
# issue: api/leads-only-terminal-auto-attach-project-leads-keep-workers-headless
# The owner's manual checks MV1-MV9 (boxes HT2-HT10; HT1 when all pass). MV1-MV8 run on a private tmux
# server with a stand-in session "main" playing yours, this branch's issue-viewer and issue-cli built into
# a sandbox, a scratch project outside .worktrees/ and a stand-in claude (no tokens). MV9 is the only step
# that touches your real tmux server, and it asks first.
# $out and $code come from run, and expect evals its condition, so variables used only there look unused:
# shellcheck source=/dev/null disable=SC2034,SC2154
source "${CHECKS_LIB:?run me with: issue-cli check-script <slug>}"
checks_intro "Leads now follow lead_terminal: instead of terminal:. With lead_terminal: tmux-window the lead becomes one" \
	"background tab in your own tmux session, a nested client pinned on the lead, while workers stay headless." \
	"MV1-MV8 run on a private tmux server with a stand-in for your session; MV9 tries it in your real one" \
	"(asks first). About 5 minutes."
command -v go > /dev/null && command -v tmux > /dev/null || {
	echo "Needs go and tmux on PATH."
	exit 2
}
cd "$CHECKS_ROOT" || exit 2

# Your real tmux, for MV9 only; read before private_tmux replaces it.
real_tmux=${TMUX:-}
real_tmpdir=${TMUX_TMPDIR:-}
# Nothing the script runs may think it is an agent of this or another project.
unset TMUX_PANE ISSUE_VIEWER_URL ISSUE_VIEWER_ISSUE_SLUG ISSUE_VIEWER_PROJECT ISSUE_VIEWER_LEAD_SCOPE \
	ISSUE_CLI_LOG ISSUE_VIEWER_SERVER_PWD CLAUDECODE ISSUE_VIEWER_CONFIG

echo "Building this branch's issue-viewer and issue-cli and starting the private server…"
sandbox_dir S
proj=$S/proj
mkdir -p "$S/bin" "$S/state" "$proj/docs"
# The sandbox viewer records itself in viewer.json under XDG_STATE_HOME: keep it off your real one.
export XDG_STATE_HOME=$S/state
go build -o "$S/bin/issue-viewer" . && go build -o "$S/bin/issue-cli" ./cmd/issue-cli || {
	echo "Build failed."
	exit 1
}
export PATH="$S/bin:$PATH"
# The stand-in claude prints the brief it was started with, so the prompt counts as delivered.
stand_in claude 'for last; do :; done
printf "STAND-IN CLAUDE (guided check)\n%s\n" "$last"
exec sleep 3600'
private_tmux

/usr/bin/cp -r playtest/scenarios/basic/tracker "$proj/issues"
/usr/bin/cp playtest/scenarios/basic/workflow.yaml "$proj/workflow.yaml"
git -C "$proj" init -q -b main
git -C "$proj" add -A
git -C "$proj" -c user.name=check -c user.email=check@example.invalid commit -q -m fixtures

write_config() { # write_config <file> <lead_window_session>
	cat > "$1" << YAML
terminal: none
lead_terminal: tmux-window
lead_window_session: "$2"
projects:
  - name: "Lead Tab Check"
    slug: "sbx"
    tmux_session: "sbx"
    lead_window: 3
    issues: "$proj/issues"
    docs: "$proj/docs"
    workflow: "$proj/workflow.yaml"
    workdir: "$proj"
YAML
}
write_config "$S/projects.yaml" main

# Everything that can start an agent (the private server, the viewer, issue-cli) runs under clean_shell:
# no profile, SHELL that rereads none, the stand-in claude first on PATH.
# cli: this branch's issue-cli on the scratch project, run from it so it never takes you for a worker.
cli() { (cd "$proj" && clean_shell issue-cli --config "$S/projects.yaml" --project sbx "$@"); }
windows() { ptmux list-windows -t "=$1" -F '#{window_index}:#{window_name}' | tr '\n' ' '; }
active() { ptmux display-message -p -t "=$1:" '#{window_name}'; }
post() { /usr/bin/curl -s -X POST -H "Origin: $url" -d "$2" "$url/p/sbx/$1"; }
wait_for() { # wait_for <condition>: up to 5 s
	for _ in $(seq 1 50); do eval "$1" && return 0; sleep 0.1; done
	return 1
}
stop_viewer() {
	[ -s "$S/viewer.pid" ] && kill "$(cat "$S/viewer.pid")" 2> /dev/null
	[ -n "$viewer_job" ] && wait "$viewer_job" 2> /dev/null
}

# The private server starts with clean_shell's PATH, so every window it makes finds the stand-in claude.
clean_shell tmux new-session -d -s main -n shell -c "$proj" /bin/sh
port=$((18100 + RANDOM % 800))
url=http://127.0.0.1:$port
viewer_job=''
on_exit stop_viewer
# shellcheck disable=SC2016 # $1-$3 are the inner sh's
(cd "$proj" && clean_shell sh -c 'echo $$ > "$1"; exec issue-viewer -config "$2" -port "$3"' sh \
	"$S/viewer.pid" "$S/projects.yaml" "$port") > "$S/viewer.log" 2>&1 &
viewer_job=$!
wait_for '/usr/bin/curl -s -o /dev/null "$url/p/sbx/"' || {
	echo "The viewer didn't start:"
	cat "$S/viewer.log"
	exit 1
}
echo "  Ready: board $url/p/sbx/ · private server $CHECKS_TMUX_DIR"
echo "  To watch along in another terminal: TMUX_TMPDIR=$CHECKS_TMUX_DIR tmux attach -t main"
pause

# ── MV1 ──────────────────────────────────────────────────────────────
step MV1 "lead spawn adds one detached tab" --box HT2 \
	--what "issue-cli lead spawn on a project with terminal: none, lead_terminal: tmux-window, lead_window_session: main, lead_window: 3." \
	--why "you see the lead without it ever stealing your focus, and without workers opening anything." \
	--look "the spawn's steps, then main's windows and which one is active." \
	--expect "\"Lead tab added at main:3 — not selected\", main has 0:shell 3:view-lead-sbx, and shell is still active."
run cli lead spawn
expect '[ $code = 0 ] && [[ $out == *"Lead tab added at main:3 — not selected"* ]]' "the lead started and its tab went to main:3"
expect '[[ $out == *"Prompt delivered"* ]]' "the lead got its brief"
run windows main
expect '[ "$out" = "0:shell 3:view-lead-sbx " ]' "main has exactly one new window, the lead tab"
run active main
expect '[ "$out" = shell ]' "main is still on shell (focus didn't move)"

# ── MV2 ──────────────────────────────────────────────────────────────
step MV2 "The tab is a nested client showing the lead" --box HT3 \
	--what "what tab 3 shows: a tmux client on the view session view-lead-sbx, grouped with the project session sbx and set to the lead's window." \
	--why "it is the lead itself you look at, not a copy, and it isn't listed as an agent anywhere." \
	--look "the tab's screen printed below (the inner status bar is its last line)." \
	--expect "the end of the lead's brief, and an inner status bar starting [view-lead (tmux cuts the name) with 0:lead-sbx."
wait_for 'ptmux has-session -t =view-lead-sbx 2> /dev/null'
run ptmux list-sessions -F '#{session_name} group=#{session_group} clients=#{session_attached}'
expect '[[ $out == *"view-lead-sbx group=sbx clients=1"* ]]' "a view session grouped with sbx, with one client (the tab)"
sleep 0.5
run ptmux capture-pane -p -t '=main:3' # the tab's screen
screen=$(grep -v '^[[:space:]]*$' <<< "$out")
expect '[[ $screen == *"0:lead-sbx"* ]] && [[ $screen == *"project lead (lead-sbx)"* ]]' "the tab shows the lead's window and its brief"
expect '[[ $(ptmux capture-pane -p -S - -t "=sbx:=lead-sbx") == *"STAND-IN CLAUDE"* ]]' "the lead is the stand-in, not a real claude"
run cli lead list
expect '[[ $out != *view-lead* ]]' "the view isn't listed as an agent or a lead"
ask "Does the screen above look like a tmux inside a tmux (its own status bar on top), showing the lead's brief?"

# ── MV3 ──────────────────────────────────────────────────────────────
step MV3 "Spawning again adds nothing" --box HT4 \
	--what "issue-cli lead spawn a second time, while the lead and its tab are live." \
	--why "a lead is spawned again after restarts and from both the CLI and the board; you must never collect duplicate tabs." \
	--look "the steps and main's windows." \
	--expect "\"Already live\", \"Lead tab already at main:3 — left as is\", and main's windows unchanged."
run cli lead spawn
expect '[[ $out == *"Already live"* ]] && [[ $out == *"Lead tab already at main:3 — left as is"* ]]' "reattached and the existing tab was found"
run windows main
expect '[ "$out" = "0:shell 3:view-lead-sbx " ]' "still exactly one lead tab"

# ── MV4 ──────────────────────────────────────────────────────────────
step MV4 "A worker dispatch opens nothing and selects nothing" --box HT5 \
	--what "the board's Dispatch for issue cli/export-issues-as-csv (POST …/dispatch), with terminal: none." \
	--why "workers keep terminal:, so with none they stay out of your way; only their window appears in the project session." \
	--look "the dispatch steps, then every session's windows and active window before and after." \
	--expect "\"Terminal: headless\", a new agent window in sbx only, and no active window changed anywhere."
before=$(ptmux list-windows -a -F '#{session_name}:#{window_name}:#{window_active}' | grep -v agent-cli | sort)
run post issue/cli/export-issues-as-csv/dispatch '{}'
expect '[[ $out == *"\"status\":\"dispatched\""* ]] && [[ $out == *"Terminal: headless"* ]]' "dispatched headless"
after=$(ptmux list-windows -a -F '#{session_name}:#{window_name}:#{window_active}' | grep -v agent-cli | sort)
run windows main
expect '[ "$out" = "0:shell 3:view-lead-sbx " ]' "main gained no window"
expect '[ "$before" = "$after" ]' "no session's active window changed"
run active view-lead-sbx
expect '[ "$out" = lead-sbx ]' "the lead tab still shows the lead"

# ── MV5 ──────────────────────────────────────────────────────────────
step MV5 "The view stays on the lead; closing the tab keeps the lead" --box HT6 \
	--what "select the worker's window in the project session sbx, then close tab 3 (kill-window, like prefix &)." \
	--why "a plain nested attach would follow every select-window; and closing a tab must never kill the lead (moving its pane would)." \
	--look "which window the view shows after the select, then the sessions after the close." \
	--expect "the view still on lead-sbx; after the close no view-lead-sbx session, and lead-sbx still in sbx."
run ptmux select-window -t '=sbx:=agent-cli-export-issues-as-csv'
run active sbx
expect '[ "$out" = agent-cli-export-issues-as-csv ]' "the project session now shows the worker"
run active view-lead-sbx
expect '[ "$out" = lead-sbx ]' "the tab still shows the lead"
run ptmux kill-window -t '=main:3'
wait_for '! ptmux has-session -t =view-lead-sbx 2> /dev/null'
run ptmux list-sessions -F '#{session_name}'
expect '[[ $out != *view-lead-sbx* ]]' "the view session went with the tab"
run windows sbx
expect '[[ $out == *lead-sbx* ]]' "the lead is still running in sbx"

# ── MV6 ──────────────────────────────────────────────────────────────
step MV6 "Spawn lead button and canvas Open follow lead_terminal" --box HT7 \
	--what "the board's Spawn lead (POST /lead/spawn) with the tab closed, then Open on the lead (POST /agents/open)." \
	--why "the CLI and the board must behave the same; Open must not stack a second tab." \
	--look "the two answers' steps and main's windows." \
	--expect "spawn: reattached + \"Lead tab added at main:3\"; Open: \"Lead tab already at main:3 — left as is\"; one tab."
run post lead/spawn '{}'
expect '[[ $out == *"\"status\":\"reattached\""* ]] && [[ $out == *"Lead tab added at main:3"* ]]' "the button re-added the tab"
run post agents/open '{"session":"lead-sbx"}'
expect '[[ $out == *"\"status\":\"opened\""* ]] && [[ $out == *"Lead tab already at main:3"* ]]' "Open found the tab and left it"
run windows main
expect '[ "$out" = "0:shell 3:view-lead-sbx " ]' "still exactly one lead tab"
run active main
expect '[ "$out" = shell ]' "main is still on shell"

# ── MV7 ──────────────────────────────────────────────────────────────
step MV7 "A taken lead_window slot falls back with a warning" --box HT8 \
	--what "close the tab, put a window of yours at index 3, then spawn again." \
	--why "lead_window must never replace or push around a window you already have there." \
	--look "the warning step and main's windows." \
	--expect "\"! Lead window slot taken — window 3 in main is taken (lead_window) — used 1\", the tab at 1, your window 3 untouched."
ptmux kill-window -t '=main:3'
wait_for '! ptmux has-session -t =view-lead-sbx 2> /dev/null'
run ptmux new-window -d -t '=main:3' -n mine /bin/sh
run cli lead spawn
expect '[[ $out == *"! Lead window slot taken — window 3 in main is taken (lead_window) — used 1"* ]]' "warned and named both slots"
run windows main
expect '[ "$out" = "0:shell 1:view-lead-sbx 3:mine " ]' "the tab went to 1, your window stayed at 3"

# ── MV8 ──────────────────────────────────────────────────────────────
step MV8 "A missing lead_window_session warns and hands out the attach commands" --box HT9 \
	--what "lead spawn with lead_window_session: nope (a session that isn't running)." \
	--why "a typo in the config must say what's wrong, not fail silently or create a session." \
	--look "the steps and the two attach lines." \
	--expect "\"! No lead tab added — session nope (lead_window_session) is not running\", then Inside tmux: / From a shell:, and no session nope."
write_config "$S/projects.yaml" nope
run cli lead spawn
expect '[[ $out == *"No lead tab added — session nope (lead_window_session) is not running"* ]]' "warned with the session's name"
expect '[[ $out == *"Inside tmux:"* ]] && [[ $out == *"From a shell:"* ]]' "printed both attach commands"
expect '! ptmux has-session -t =nope 2> /dev/null' "created no session nope"
write_config "$S/projects.yaml" main

# ── MV9 ──────────────────────────────────────────────────────────────
# real: a command on your real tmux server. real_cli: this branch's issue-cli there, under clean_shell
# like cli (if the live lead weren't found it would start the stand-in, not your claude).
real() { env -u TMUX_TMPDIR TMUX="$real_tmux" ${real_tmpdir:+TMUX_TMPDIR="$real_tmpdir"} "$@"; }
real_cli() {
	(
		cd "$proj" || exit 2
		unset TMUX_TMPDIR
		export TMUX=$real_tmux
		[ -n "$real_tmpdir" ] && export TMUX_TMPDIR=$real_tmpdir
		CLEAN_SHELL_KEEP=TMUX clean_shell issue-cli --config "$S/real.yaml" --project ltchk "$@"
	)
}
real_started=''
real_cleanup() {
	[ -n "$real_started" ] || return 0
	real tmux kill-window -t "=$real_session:=view-lead-ltchk" 2> /dev/null
	real tmux kill-session -t =view-lead-ltchk 2> /dev/null
	real tmux kill-session -t =ltchk 2> /dev/null
	real_started=
}
# Which session the tab would land in is only read in your own run: a machine run never asks your server.
real_session='' real_window=''
if [ -n "$real_tmux" ] && [ "$CHECKS_MODE" = owner ]; then
	real_session=$(real tmux display-message -p '#S')
	real_window=$(real tmux display-message -p '#W')
fi
mv9_real="a session ltchk with one window lead-ltchk running a stand-in that prints \"LEAD STAND-IN\" (no claude; lead spawn finds this live lead and only adds its tab), a tab view-lead-ltchk in your session ${real_session:-(the one you run this from)} (next free index), and its view session view-lead-ltchk."
if [[ $real_window == agent-* || $real_window == lead-* ]]; then
	mv9_real+=" This window is named $real_window, so issue-cli would take you for that agent; run from another window."
fi
mv9=(MV9 "In your real tmux session" --box HT10
	--what "the same tab, in the tmux session you are in right now, on your real tmux server."
	--why "the private server has no real terminal attached; only you can see the tab appear in your status bar and keep your focus."
	--look "your status bar, and the tab itself (prefix <its number>)."
	--expect "one new tab view-lead-ltchk, you stay on this window, the tab shows \"LEAD STAND-IN\"; afterwards nothing is left.")
if [ -z "$real_tmux" ]; then
	step "${mv9[@]}"
	skip "not run from inside tmux (run the script in a tmux window to include it)"
elif step "${mv9[@]}" --touches-real "$mv9_real"; then
	if real tmux has-session -t =ltchk 2> /dev/null; then
		skip "you already have a tmux session named ltchk; left alone"
	else
		on_exit real_cleanup
		cat > "$S/real.yaml" << YAML
terminal: none
lead_terminal: tmux-window
lead_window_session: "$real_session"
projects:
  - name: "Lead Tab Check (real)"
    slug: "ltchk"
    tmux_session: "ltchk"
    issues: "$proj/issues"
    docs: "$proj/docs"
    workflow: "$proj/workflow.yaml"
    workdir: "$proj"
YAML
		real_started=1
		real tmux new-session -d -s ltchk -n lead-ltchk -c "$proj" "printf 'LEAD STAND-IN (guided check, lead-ltchk)\n'; exec sleep 3600"
		before_active=$(real tmux display-message -p -t "=$real_session:" '#{window_index}')
		before_count=$(real tmux list-windows -t "=$real_session" | wc -l)
		run real_cli lead spawn
		real_idx=$(sed -n "s/.*Lead tab added at $real_session:\([0-9]*\) — not selected.*/\1/p" <<< "$out")
		expect '[ -n "$real_idx" ]' "the tab went into $real_session"
		expect '[ "$(real tmux list-windows -t "=$real_session" | wc -l)" = $((before_count + 1)) ]' "exactly one window more in $real_session"
		expect '[ "$(real tmux display-message -p -t "=$real_session:" "#{window_index}")" = "$before_active" ]' "your active window didn't change"
		run real_cli lead spawn
		expect '[[ $out == *"Lead tab already at $real_session:$real_idx"* ]]' "a second spawn added nothing"
		ask "Did ${real_idx:-?}:view-lead-ltchk appear in your status bar while you stayed on this window?"
		ask "Press prefix ${real_idx:-?} (then come back): does the tab show LEAD STAND-IN in a nested tmux?"
		real_cleanup
		expect '! real tmux has-session -t =ltchk 2> /dev/null && ! real tmux has-session -t =view-lead-ltchk 2> /dev/null' "ltchk and its view are gone from your server"
		expect '[ "$(real tmux list-windows -t "=$real_session" | wc -l)" = "$before_count" ]' "your session is back to $before_count windows"
	fi
fi

summary --all HT1
