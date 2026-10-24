# Guided check library: what an issue's scripts/checks/<name>.sh sources.
# issue-cli ships it (`issue-cli check-script --lib` prints this file) and
# `issue-cli check-script <slug>` runs the issue's scripts with it:
#
#   #!/usr/bin/env bash
#   # issue: <slug>                      ← check-script finds the script by this line
#   source "${CHECKS_LIB:?run me with: issue-cli check-script <slug>}"
#   checks_intro "What changed, in a line or two. About 3 minutes."
#
#   step MV1 "The command answers" --box HT2 \
#     --what "what it runs" --why "why it matters" --look "what to look at" --expect "the expected result"
#   run issue-cli status                 # shows the command, its output and exit code; sets $out and $code
#   expect '[ $code = 0 ]' "status answered"
#   ask "Does the stand-up list the worker?"
#
#   summary --all HT1
#
# Steps
#   step <id> <title> [--box <ids>] [--what <text>] [--why <text>] [--look <text>]
#        [--expect <text>] [--touches-real <what it creates>]
#       Opens a step; the previous one closes (its verdict, then Enter to go on).
#       --box names the checkboxes the step proves (comma or space separated,
#       repeatable); without it the box is the step id. A box passes only when
#       every step naming it passed. The same id twice adds a second part to
#       one step: it passes only if both parts did.
#       --touches-real marks a step that changes the owner's real environment
#       (their tmux session, their config): it lists what it creates and runs
#       only on an explicit y. step returns 1 when the step is skipped, so wrap
#       such a step's own commands:  if step MV9 … --touches-real "…"; then …; fi
#   run <cmd…>          show the command, run it, show its output and exit code;
#                       sets $out (stdout+stderr) and $code
#   expect <cond> <meaning>
#                       a machine check: eval's <cond>, ✓ or ✗; a ✗ fails the step
#   ask <question>      the owner's y/n (q stops); a n fails the step. In a
#                       machine run the step is marked "needs your eyes".
#   eventually <cond> <meaning> [seconds]
#                       expect, retried every 0.2 s for up to [seconds] (default 10)
#   skip <why>          skips the rest of the step (run/expect/ask do nothing)
#   step_active         true while a step is open and not skipped: guard a step's
#                       own commands with it (if step_active; then …; fi)
#   step_end            closes the open step now (its verdict), before summary
#   checks_default_box <ids>
#                       the boxes of every later step that names none (else its id)
#   show <file>         opens an image or file in the desktop viewer, or prints its path
#   pause               Enter to go on, q stops (interactive runs only)
#   summary [--all <boxes>] [--then <text>]
#                       one line per step, the boxes that passed, a run record
#                       under .agent-logs/checks/<name>/; from a terminal it
#                       offers to tick the passed boxes and to comment the
#                       failures as `checks:`. --all passes its boxes (comma or
#                       space separated, repeatable) only when every step
#                       passed; --then is the owner's closing line when
#                       nothing failed. Returns 0 when nothing failed.
#
# Cleanup and sandboxes
#   on_exit <cmd…>      runs on exit, q, Ctrl-C or a kill; last registered runs first
#   sandbox_dir [var]   a fresh temp folder outside any .worktrees/, removed on
#                       exit; its path goes in $var (default SANDBOX)
#   sandbox_project <slug> [template] [--workflow <file>] [--name <name>]
#                       a throwaway project in a sandbox_dir: a git repo, its
#                       workflow (issue-cli init --template, default development,
#                       or a copy of <file>), its own projects.yaml (headless:
#                       terminal: "none"; append more settings to
#                       $SANDBOX_CONFIG, indented four spaces). Sets
#                       SANDBOX_PROJECT, SANDBOX_PROJECT_DIR and SANDBOX_CONFIG,
#                       and exports XDG_STATE_HOME and ISSUE_VIEWER_CONFIG into
#                       the sandbox. Scratch projects must live outside
#                       .worktrees/: there issue-cli takes the caller for a
#                       worker and refuses dispatch and lead spawn.
#   sandbox_git_identity
#                       exports a git author/committer and GIT_CONFIG_GLOBAL=/dev/null
#                       (no signing, hooks or aliases) for git commands in sandboxes
#   sic <args…>         issue-cli against the sandbox project, from its folder
#                       ($SANDBOX_ISSUE_CLI picks the binary, e.g. a branch build);
#                       through clean_shell once a stand-in exists, so a dispatch
#                       or lead spawn gets the stand-in
#   private_tmux [--clean]
#                       a private tmux server (short TMUX_TMPDIR, marker file);
#                       exports TMUX_TMPDIR and unsets TMUX for the rest of the
#                       script; on exit kill-server runs on it only. A tmux
#                       server keeps the environment of whatever started it, and
#                       panes inherit it: with --clean the server starts at once
#                       from clean_shell's environment (stand-ins first on PATH,
#                       no tmux.conf), so agents in its windows can't find the
#                       real claude; ptmux then runs in that environment too (a
#                       new session takes the tmux client's environment). Without
#                       it, start sessions through clean_shell when agents run there.
#   ptmux <args…>       tmux on the private server ($CHECKS_TMUX_DIR is its
#                       TMUX_TMPDIR, for an attach command the owner can type)
#   stand_in [--agent] <name> [script]
#                       puts a recording stand-in for <name> (e.g. claude) first
#                       on the PATH clean_shell uses; [script] is its body
#                       ($CHECKS_STAND_IN_DIR holds the records). --agent: it
#                       says "<name> stand-in ready" and stays alive, appending
#                       what it is sent to <name>.input (a re-sent prompt lands
#                       there, never in a shell)
#   expect_stand_in_ran <name> [meaning] [seconds]
#                       a machine check that the stand-in, not the real <name>, ran
#                       (eventually: agents start a moment later; default 5 s)
#   expect_stand_in_got <name> <text> [meaning]
#                       eventually: the --agent stand-in was sent <text>
#   clean_shell <cmd…>  runs <cmd> under bash without the owner's profile or rc
#                       (env -i, SHELL set to a no-profile bash, stand-ins first
#                       on PATH). Never use $SHELL -lic in a check that starts
#                       agents: a login shell rereads the profile and the real
#                       claude gets in front of the stand-in.
#   background <var> [clean_shell] <cmd…>
#                       starts <cmd> in the background (through clean_shell if
#                       named), its pid in $var, killed on exit
#   free_port [var]     a free local TCP port in $var (default PORT)
#   wait_for_url <url> [seconds]
#                       true once <url> answers (default 10 s)
#
# Environment from check-script: CHECKS_LIB, CHECKS_SLUG, CHECKS_PROJECT (the
# --project ticking uses), CHECKS_ROOT (the issue's checkout; the library cds
# there), CHECKS_NAME, CHECKS_ISSUE_CLI (the binary that ticks), CHECKS_YES.
#
# Gotchas
#   - run runs its command in a subshell (to capture it): call helpers that
#     change the script's state (sandbox_*, private_tmux, on_exit, background,
#     cd, export) directly, never through run.
#   - shellcheck can't see that sandbox_dir assigns its variable: declare it
#     first (build=; sandbox_dir build).
#   - Don't set -e: a failed check belongs to its step, it doesn't stop the script.
#   - A viewer writes viewer.json under XDG_STATE_HOME: run sandbox viewers
#     after sandbox_project (which moves it into the sandbox), or the owner's
#     real viewer.json is overwritten.
#   - shellcheck sees $out and $code used only inside expect strings, and
#     variables set by sandbox_dir, as unused or unassigned: put
#     `# shellcheck source=/dev/null disable=SC2034,SC2154` above the source line.
#
# Modes: from a terminal the run is the owner's (questions, Enter between
# steps, the tick offer). With --yes (CHECKS_YES=1) or stdin not a terminal it
# is a machine run: machine checks only, questions are marked "needs your
# eyes", --touches-real steps are skipped, and nothing is ticked.
# NO_COLOR, or stdout not a terminal, turns colour off.

CHECKS_LIB_VERSION=2

if [ -z "${BASH_VERSINFO:-}" ] || [ "${BASH_VERSINFO[0]}" -lt 4 ]; then
	echo "guided checks need bash 4 or newer" >&2
	exit 2
fi
set +e

# ── Context ──────────────────────────────────────────────────────────
_ck_script=${CHECKS_SCRIPT:-$0}
CHECKS_NAME=${CHECKS_NAME:-$(basename "$_ck_script" .sh)}
if [ -z "${CHECKS_SLUG:-}" ]; then
	CHECKS_SLUG=$(sed -n '1,15s/^# issue:[[:space:]]*\([^[:space:]]*\).*/\1/p' "$_ck_script" 2> /dev/null | head -n 1)
fi
CHECKS_ROOT=${CHECKS_ROOT:-$PWD}
CHECKS_ISSUE_CLI=${CHECKS_ISSUE_CLI:-issue-cli}
cd "$CHECKS_ROOT" || exit 2

if [ "${CHECKS_YES:-}" = 1 ] || [ ! -t 0 ]; then
	CHECKS_MODE=machine
else
	CHECKS_MODE=owner
fi

if [ -z "${NO_COLOR:-}" ] && [ -t 1 ]; then
	_ck_b=$'\e[1m' _ck_d=$'\e[2m' _ck_g=$'\e[32m' _ck_r=$'\e[31m' _ck_y=$'\e[33m' _ck_c=$'\e[36m' _ck_0=$'\e[0m'
else
	_ck_b='' _ck_d='' _ck_g='' _ck_r='' _ck_y='' _ck_c='' _ck_0=''
fi

# The caller's environment as it was before any sandbox helper changed it:
# ticking the real issue runs with it (_ck_real).
_ck_saved_vars=(TMUX TMUX_TMPDIR XDG_STATE_HOME ISSUE_VIEWER_CONFIG SHELL PATH)
declare -A _ck_saved=() _ck_saved_set=()
for _ck_v in "${_ck_saved_vars[@]}"; do
	if [ -n "${!_ck_v+x}" ]; then
		_ck_saved[$_ck_v]=${!_ck_v}
		_ck_saved_set[$_ck_v]=1
	fi
done
unset _ck_v

_ck_real() {
	local unsets=() sets=() v
	for v in "${_ck_saved_vars[@]}"; do
		if [ -n "${_ck_saved_set[$v]:-}" ]; then
			sets+=("$v=${_ck_saved[$v]}")
		else
			unsets+=(-u "$v")
		fi
	done
	env "${unsets[@]}" "${sets[@]}" "$@"
}

# ── Cleanup ──────────────────────────────────────────────────────────
_ck_cleanups=()

on_exit() {
	_ck_cleanups+=("$(printf '%q ' "$@")")
}

_ck_run_cleanups() {
	local i
	for ((i = ${#_ck_cleanups[@]} - 1; i >= 0; i--)); do
		eval "${_ck_cleanups[$i]}" > /dev/null 2>&1
	done
	_ck_cleanups=()
}

_ck_stop() {
	echo
	echo "Stopped."
	exit 130
}

trap _ck_run_cleanups EXIT
trap _ck_stop INT TERM HUP

_ck_quit_if_q() {
	[[ ${1:-} == [qQ]* ]] && _ck_stop
	return 0
}

# ── Steps ────────────────────────────────────────────────────────────
_ck_order=()
declare -A _ck_result=() _ck_note=() _ck_boxes=() _ck_title=()
_ck_cur=
_ck_failed=0
_ck_eyes=0
_ck_skipping=0
_ck_skip_why=
_ck_reason=
_ck_default_box=

_ck_add_boxes() { # _ck_add_boxes <id> <box list>
	local b
	for b in ${2//,/ }; do
		[[ " ${_ck_boxes[$1]:-} " == *" $b "* ]] || _ck_boxes[$1]="${_ck_boxes[$1]:+${_ck_boxes[$1]} }$b"
	done
}

_ck_fail() {
	_ck_failed=1
	[ -n "$_ck_reason" ] || _ck_reason=$1
}

# _ck_close records the open step's verdict. A repeated id merges: fail wins,
# then skip, then needs-eyes, then pass.
_ck_close() {
	[ -n "$_ck_cur" ] || return 0
	local id=$_ck_cur verdict note=
	if [ "$_ck_failed" = 1 ]; then
		verdict=fail note=$_ck_reason
	elif [ "$_ck_skipping" = 1 ]; then
		verdict=skip note=$_ck_skip_why
	elif [ "$_ck_eyes" = 1 ]; then
		verdict=eyes note="needs your eyes"
	else
		verdict=pass
	fi
	local prev=${_ck_result[$id]:-}
	if [ -z "$prev" ] || [ "$verdict" = fail ] || { [ "$verdict" = skip ] && [ "$prev" != fail ]; } ||
		{ [ "$verdict" = eyes ] && [ "$prev" = pass ]; }; then
		_ck_result[$id]=$verdict
		_ck_note[$id]=$note
	fi
	case "$verdict" in
	pass) echo "  ${_ck_g}${_ck_b}$id passed${_ck_0}" ;;
	fail) echo "  ${_ck_r}${_ck_b}$id failed${_ck_0}: $note" ;;
	skip) echo "  ${_ck_y}${_ck_b}$id skipped${_ck_0}: $note" ;;
	eyes) echo "  ${_ck_y}${_ck_b}$id machine checks passed${_ck_0}; the rest needs your eyes" ;;
	esac
	_ck_cur=
	pause
}

checks_intro() {
	echo "${_ck_b}Manual checks for ${CHECKS_SLUG:-$CHECKS_NAME}${_ck_0} (${CHECKS_NAME})"
	local line
	for line in "$@"; do echo "$line"; done
	if [ "$CHECKS_MODE" = machine ]; then
		echo "${_ck_d}Machine run: machine checks only; questions are left for the owner and nothing is ticked.${_ck_0}"
	fi
	pause
}

step() {
	_ck_close
	local id=${1:?step needs an id} title=${2:?step needs a title}
	shift 2
	local what='' why='' look='' exp='' real='' boxes=''
	while [ $# -gt 0 ]; do
		case "$1" in
		--box) boxes="$boxes ${2:-}" ;;
		--what) what=${2:-} ;;
		--why) why=${2:-} ;;
		--look) look=${2:-} ;;
		--expect) exp=${2:-} ;;
		--touches-real) real=${2:-} ;;
		*)
			echo "step $id: unknown option $1" >&2
			exit 2
			;;
		esac
		shift 2
	done
	if [ -z "${_ck_title[$id]:-}" ]; then
		_ck_order+=("$id")
		_ck_title[$id]=$title
	fi
	_ck_add_boxes "$id" "${boxes:-${_ck_default_box:-$id}}"
	_ck_cur=$id _ck_failed=0 _ck_eyes=0 _ck_skipping=0 _ck_skip_why='' _ck_reason=''

	echo
	echo "${_ck_b}${_ck_c}━━ $id · $title ━━${_ck_0}  ${_ck_d}(box ${_ck_boxes[$id]// /, })${_ck_0}"
	[ -n "$what" ] && echo "  ${_ck_b}What:${_ck_0}    $what"
	[ -n "$why" ] && echo "  ${_ck_b}Why:${_ck_0}     $why"
	[ -n "$look" ] && echo "  ${_ck_b}Look at:${_ck_0} $look"
	[ -n "$exp" ] && echo "  ${_ck_b}Expect:${_ck_0}  $exp"
	if [ -n "$real" ]; then
		echo "  ${_ck_y}${_ck_b}Touches your real environment:${_ck_0} $real"
		if [ "$CHECKS_MODE" = machine ]; then
			skip "touches your real environment; run it from a terminal"
			return 1
		fi
		local answer
		read -r -p "  Run this step? It creates the above and removes it at the end [y/N, q stops] " answer
		_ck_quit_if_q "$answer"
		if [[ $answer != [yY]* ]]; then
			skip "you chose not to run it"
			return 1
		fi
	fi
	echo
	return 0
}

checks_default_box() {
	_ck_default_box="$*"
}

step_active() {
	[ -n "$_ck_cur" ] && [ "$_ck_skipping" = 0 ]
}

step_end() {
	_ck_close
}

skip() {
	[ -n "$_ck_cur" ] || return 1
	_ck_skipping=1
	_ck_skip_why=${1:-skipped}
	return 1
}

# _ck_words prints a command the way you would type it: plain words as they
# are, others in single quotes.
_ck_words() {
	local a sep='' s=''
	for a in "$@"; do
		if [[ $a =~ ^[A-Za-z0-9_./:=@%+,-]+$ ]]; then
			s+="$sep$a"
		else
			s+="$sep'${a//\'/\'\\\'\'}'"
		fi
		sep=' '
	done
	printf '%s' "$s"
}

run() {
	[ "$_ck_skipping" = 1 ] && return 0
	echo "${_ck_d}\$ $(_ck_words "$@")${_ck_0}"
	out=$("$@" 2>&1)
	code=$?
	[ -n "$out" ] && printf '%s\n' "$out" | sed 's/^/    /'
	echo "${_ck_d}  (exit $code)${_ck_0}"
	return 0
}

expect() {
	[ "$_ck_skipping" = 1 ] && return 0
	if eval "$1"; then
		echo "  ${_ck_g}✓ $2${_ck_0}"
		return 0
	fi
	echo "  ${_ck_r}✗ $2${_ck_0}"
	_ck_fail "$2"
	return 1
}

eventually() {
	[ "$_ck_skipping" = 1 ] && return 0
	local tries=$((${3:-10} * 5))
	while [ "$tries" -gt 0 ]; do
		eval "$1" && break
		sleep 0.2
		tries=$((tries - 1))
	done
	expect "$1" "$2"
}

ask() {
	[ "$_ck_skipping" = 1 ] && return 0
	if [ "$CHECKS_MODE" = machine ]; then
		echo "  ${_ck_y}? $1${_ck_0} ${_ck_d}(needs your eyes)${_ck_0}"
		_ck_eyes=1
		return 0
	fi
	local answer
	read -r -p "  $1 [y/n, q stops] " answer
	_ck_quit_if_q "$answer"
	[[ $answer == [yY]* ]] && return 0
	_ck_fail "you answered no: $1"
	return 1
}

pause() {
	[ "$CHECKS_MODE" = owner ] || return 0
	local answer
	read -r -p "${_ck_d}  Enter to continue, q to stop…${_ck_0}" answer
	_ck_quit_if_q "$answer"
}

show() {
	if [ "$CHECKS_MODE" = owner ] && [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ] && command -v xdg-open > /dev/null; then
		xdg-open "$1" > /dev/null 2>&1 &
		echo "  Opened $1"
	else
		echo "  Look at $1"
	fi
}

# ── Sandboxes ────────────────────────────────────────────────────────
_ck_tmp_base() {
	local base=${TMPDIR:-/tmp}
	[[ $base == */.worktrees/* ]] && base=/tmp
	echo "$base"
}

# sandbox_dir's locals are prefixed: printf -v would set a local of the same
# name instead of the caller's variable.
sandbox_dir() {
	local _ck_sd_var=${1:-SANDBOX} _ck_sd_path
	_ck_sd_path=$(mktemp -d "$(_ck_tmp_base)/ck-XXXXXX") || {
		echo "sandbox_dir: mktemp failed" >&2
		exit 2
	}
	on_exit rm -rf -- "$_ck_sd_path"
	printf -v "$_ck_sd_var" '%s' "$_ck_sd_path"
}

sandbox_project() {
	local name=${1:?sandbox_project needs a slug} template=development workflow='' display='' dir
	shift
	if [ $# -gt 0 ] && [[ $1 != --* ]]; then
		template=$1
		shift
	fi
	while [ $# -gt 0 ]; do
		case "$1" in
		--template) template=${2:-} ;;
		--workflow) workflow=${2:-} ;;
		--name) display=${2:-} ;;
		*)
			echo "sandbox_project: unknown option $1" >&2
			exit 2
			;;
		esac
		shift 2
	done
	dir=''
	sandbox_dir dir
	SANDBOX_PROJECT=$name
	SANDBOX_PROJECT_DIR=$dir/$name
	SANDBOX_CONFIG=$dir/projects.yaml
	mkdir -p "$SANDBOX_PROJECT_DIR" "$dir/state"
	export XDG_STATE_HOME=$dir/state ISSUE_VIEWER_CONFIG=$SANDBOX_CONFIG
	cat > "$SANDBOX_CONFIG" << YAML
projects:
  - name: "${display:-$name}"
    slug: "$name"
    issues: "$SANDBOX_PROJECT_DIR/issues"
    workflow: "$SANDBOX_PROJECT_DIR/workflow.yaml"
    docs: "$SANDBOX_PROJECT_DIR/docs"
    workdir: "$SANDBOX_PROJECT_DIR"
    terminal: "none"
YAML
	(
		cd "$SANDBOX_PROJECT_DIR" &&
			git init -q -b main &&
			if [ -n "$workflow" ]; then
				cp -- "$workflow" workflow.yaml && mkdir -p issues docs
			else
				"${SANDBOX_ISSUE_CLI:-$CHECKS_ISSUE_CLI}" init --template "$template" > /dev/null
			fi &&
			printf '.agent-logs/\n.playtest/\n' > .gitignore &&
			git add -A && git -c user.name=checks -c user.email=checks@localhost -c commit.gpgsign=false -c core.hooksPath=/dev/null commit -q -m init
	) || {
		echo "sandbox_project: could not set up $SANDBOX_PROJECT_DIR" >&2
		exit 2
	}
}

sandbox_git_identity() {
	export GIT_AUTHOR_NAME=checks GIT_AUTHOR_EMAIL=checks@localhost
	export GIT_COMMITTER_NAME=checks GIT_COMMITTER_EMAIL=checks@localhost
	export GIT_CONFIG_GLOBAL=/dev/null
}

sic() {
	local argv=("${SANDBOX_ISSUE_CLI:-$CHECKS_ISSUE_CLI}" --config "$SANDBOX_CONFIG" --project "$SANDBOX_PROJECT" "$@")
	(
		cd "${SANDBOX_PROJECT_DIR:?sic needs sandbox_project first}" || exit 2
		if [ -n "${CHECKS_STAND_IN_DIR:-}" ]; then
			clean_shell "${argv[@]}"
		else
			"${argv[@]}"
		fi
	)
}

private_tmux() {
	CHECKS_TMUX_DIR=$(mktemp -d /tmp/ckt-XXXXXX) || {
		echo "private_tmux: mktemp failed" >&2
		exit 2
	}
	: > "$CHECKS_TMUX_DIR/.checks-private-tmux"
	on_exit _ck_kill_private_tmux "$CHECKS_TMUX_DIR"
	export TMUX_TMPDIR=$CHECKS_TMUX_DIR
	unset TMUX
	if [ "${1:-}" = --clean ]; then
		_ck_tmux_clean=1
		_ck_clean_argv tmux -f /dev/null new-session -d -s checks-keepalive 'sleep 2147483647'
		"${_ck_argv[@]}" || {
			echo "private_tmux: could not start the private tmux server" >&2
			exit 2
		}
	fi
}

_ck_kill_private_tmux() {
	[ -f "$1/.checks-private-tmux" ] || return 0
	env -u TMUX TMUX_TMPDIR="$1" tmux kill-server 2> /dev/null
	rm -rf -- "$1"
}

# ptmux on a --clean server runs tmux itself in clean_shell's environment: a
# new session takes its environment from the tmux client.
ptmux() {
	: "${CHECKS_TMUX_DIR:?ptmux needs private_tmux first}"
	if [ -n "${_ck_tmux_clean:-}" ]; then
		_ck_clean_argv tmux "$@"
		"${_ck_argv[@]}"
	else
		env -u TMUX TMUX_TMPDIR="$CHECKS_TMUX_DIR" tmux "$@"
	fi
}

_ck_stand_in_dir() {
	if [ -z "${CHECKS_STAND_IN_DIR:-}" ]; then
		CHECKS_STAND_IN_DIR=''
		sandbox_dir CHECKS_STAND_IN_DIR
		export CHECKS_STAND_IN_DIR
		mkdir -p "$CHECKS_STAND_IN_DIR/bin"
		printf '#!/bin/sh\nexec bash --noprofile --norc "$@"\n' > "$CHECKS_STAND_IN_DIR/shell"
		chmod +x "$CHECKS_STAND_IN_DIR/shell"
	fi
}

stand_in() {
	local agent=''
	if [ "${1:-}" = --agent ]; then
		agent=1
		shift
	fi
	local name=${1:?stand_in needs a name}
	_ck_stand_in_dir
	local f=$CHECKS_STAND_IN_DIR/bin/$name body=${2:-exit 0}
	if [ -n "$agent" ] && [ -z "${2:-}" ]; then
		body="echo \"$name stand-in ready\"
exec cat >> $(printf '%q' "$CHECKS_STAND_IN_DIR/$name.input")"
	fi
	{
		printf '#!/bin/sh\n'
		printf 'echo "$0 $*" >> %q\n' "$CHECKS_STAND_IN_DIR/$name.calls"
		printf '%s\n' "$body"
	} > "$f"
	chmod +x "$f"
}

expect_stand_in_ran() {
	local name=${1:?expect_stand_in_ran needs a name}
	eventually "[ -s $(printf '%q' "${CHECKS_STAND_IN_DIR:-/nonexistent}/$name.calls") ]" "${2:-the stand-in $name ran, not the real one}" "${3:-5}"
}

expect_stand_in_got() {
	local name=${1:?expect_stand_in_got needs a name} text=${2:?expect_stand_in_got needs a text}
	eventually "grep -qF -- $(printf '%q' "$text") $(printf '%q' "${CHECKS_STAND_IN_DIR:-/nonexistent}/$name.input") 2> /dev/null" "${3:-the stand-in $name was sent: $text}"
}

# _ck_clean_argv fills _ck_argv with <cmd> under clean_shell's environment:
# HOME, TERM, USER, LANG, the caller's PATH behind the stand-ins, and the
# sandbox variables.
_ck_clean_argv() {
	_ck_stand_in_dir
	local keep=() v
	for v in HOME TERM USER LANG TMUX_TMPDIR XDG_STATE_HOME ISSUE_VIEWER_CONFIG CHECKS_STAND_IN_DIR ${CLEAN_SHELL_KEEP:-}; do
		[ -n "${!v+x}" ] && keep+=("$v=${!v}")
	done
	_ck_argv=(env -i "${keep[@]}" SHELL="$CHECKS_STAND_IN_DIR/shell" PATH="$CHECKS_STAND_IN_DIR/bin:$PATH"
		bash --noprofile --norc -c "exec $(printf '%q ' "$@")")
}

clean_shell() {
	_ck_clean_argv "$@"
	"${_ck_argv[@]}"
}

background() {
	local _ck_bg_var=${1:?background needs a variable} _ck_bg_pid
	shift
	if [ "${1:-}" = clean_shell ]; then
		shift
		_ck_clean_argv "$@"
		"${_ck_argv[@]}" &
	else
		"$@" &
	fi
	_ck_bg_pid=$!
	on_exit kill "$_ck_bg_pid"
	printf -v "$_ck_bg_var" '%s' "$_ck_bg_pid"
}

free_port() {
	local _ck_fp_var=${1:-PORT} _ck_fp_port _ck_fp_try
	for _ck_fp_try in $(seq 1 50); do
		_ck_fp_port=$((20000 + RANDOM % 20000))
		if ! (: > "/dev/tcp/127.0.0.1/$_ck_fp_port") 2> /dev/null; then
			printf -v "$_ck_fp_var" '%s' "$_ck_fp_port"
			return 0
		fi
	done
	echo "free_port: no free port found" >&2
	return 1
}

wait_for_url() {
	local url=${1:?wait_for_url needs a url} tries=$((${2:-10} * 5))
	while [ "$tries" -gt 0 ]; do
		curl -s -o /dev/null --max-time 1 "$url" && return 0
		sleep 0.2
		tries=$((tries - 1))
	done
	return 1
}

# ── Summary ──────────────────────────────────────────────────────────
summary() {
	_ck_close
	local all_boxes=() then='' b
	while [ $# -gt 0 ]; do
		case "$1" in
		--all)
			for b in ${2//,/ }; do all_boxes+=("$b"); done
			shift
			;;
		--then) then=${2:-} && shift ;;
		esac
		shift
	done

	local id failed=() eyes=() skipped=() passed_boxes=() lines=()
	declare -A box_ok=() box_seen=()
	local box_order=()
	echo
	echo "${_ck_b}Summary${_ck_0}"
	for id in "${_ck_order[@]}"; do
		local r=${_ck_result[$id]:-fail} n=${_ck_note[$id]:-}
		case "$r" in
		pass) echo "  ${_ck_g}✓ $id pass${_ck_0}" ;;
		fail) echo "  ${_ck_r}✗ $id fail${_ck_0}: $n" && failed+=("$id") ;;
		skip) echo "  ${_ck_y}- $id skipped${_ck_0}: $n" && skipped+=("$id") ;;
		eyes) echo "  ${_ck_y}? $id needs your eyes${_ck_0} (machine checks passed)" && eyes+=("$id") ;;
		esac
		lines+=("step $id $r boxes=${_ck_boxes[$id]// /,}${n:+ note=$n}")
		for b in ${_ck_boxes[$id]}; do
			if [ -z "${box_seen[$b]:-}" ]; then
				box_seen[$b]=1
				box_order+=("$b")
				box_ok[$b]=1
			fi
			[ "$r" = pass ] || box_ok[$b]=0
		done
	done
	for b in "${box_order[@]}"; do
		[ "${box_ok[$b]}" = 1 ] && passed_boxes+=("$b")
	done
	if [ ${#all_boxes[@]} -gt 0 ] && [ ${#_ck_order[@]} -gt 0 ] && [ ${#failed[@]} = 0 ] && [ ${#eyes[@]} = 0 ] && [ ${#skipped[@]} = 0 ]; then
		for b in "${all_boxes[@]}"; do
			[ -n "${box_ok[$b]:-}" ] || passed_boxes+=("$b")
		done
	fi
	echo "  Boxes passed: ${passed_boxes[*]:-none}"

	local dir="$CHECKS_ROOT/.agent-logs/checks/$CHECKS_NAME" stamp
	stamp=$(date -u +%Y%m%dT%H%M%SZ)
	if mkdir -p "$dir" 2> /dev/null; then
		{
			echo "script: $CHECKS_NAME"
			echo "issue: ${CHECKS_SLUG:-}"
			echo "mode: $CHECKS_MODE"
			echo "lib: $CHECKS_LIB_VERSION"
			printf '%s\n' "${lines[@]}"
			echo "boxes passed: ${passed_boxes[*]:-}"
		} > "$dir/$stamp-$CHECKS_MODE.txt"
	fi

	if [ "$CHECKS_MODE" = machine ]; then
		echo "Machine run: ${#failed[@]} failed, ${#eyes[@]} need the owner's eyes, ${#skipped[@]} skipped. Nothing ticked."
		[ ${#failed[@]} = 0 ]
		return
	fi

	if [ -n "${CHECKS_SLUG:-}" ]; then
		local answer
		if [ ${#passed_boxes[@]} -gt 0 ]; then
			read -r -p "  Tick the passed boxes (${passed_boxes[*]}) on ${CHECKS_SLUG}? [y/n] " answer
			[[ $answer == [yY]* ]] && _ck_tick "${passed_boxes[@]}"
		fi
		if [ ${#failed[@]} -gt 0 ]; then
			local text="checks: $CHECKS_NAME:" sep=
			for id in "${failed[@]}"; do
				text="$text$sep $id failed: ${_ck_note[$id]}"
				sep=";"
			done
			read -r -p "  Comment the failures on ${CHECKS_SLUG} for the agent? [y/n] " answer
			[[ $answer == [yY]* ]] && _ck_comment "$text"
		fi
	fi
	if [ ${#failed[@]} = 0 ] && [ ${#skipped[@]} = 0 ]; then
		echo "${then:-All steps passed.}"
	elif [ ${#failed[@]} -gt 0 ]; then
		echo "Tell the agent which step failed and what you saw."
	fi
	[ ${#failed[@]} = 0 ]
}

_ck_project_args() {
	[ -n "${CHECKS_PROJECT:-}" ] && printf '%s\n' --project "$CHECKS_PROJECT"
}

_ck_tick() {
	local proj=()
	mapfile -t proj < <(_ck_project_args)
	_ck_real "$CHECKS_ISSUE_CLI" "${proj[@]}" check "$CHECKS_SLUG" "$@"
}

_ck_comment() {
	local proj=()
	mapfile -t proj < <(_ck_project_args)
	_ck_real "$CHECKS_ISSUE_CLI" "${proj[@]}" comment "$CHECKS_SLUG" "$1"
}
