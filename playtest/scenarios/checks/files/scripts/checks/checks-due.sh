#!/usr/bin/env bash
# issue: cli/checks-due
source "${CHECKS_LIB:?run me with: issue-cli check-script <slug>}"
checks_intro "A one-step demo script."
step TP1 "The command answers" --what "echo hello" --expect "hello"
run echo hello
expect '[[ $out == hello ]]' "it said hello"
ask "Did it say hello?"
summary
