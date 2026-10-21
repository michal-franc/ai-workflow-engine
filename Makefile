.PHONY: build build-cli install demo clean validate test vet cover-cli playtest-check ux-test playtest playtest-report

build:
	go build -o issue-viewer .

build-cli:
	go build -o issue-cli ./cmd/issue-cli/

install:
	go install ./cmd/issue-cli/
	go install .
	@echo "Installed issue-cli and issue-viewer via go install"

demo: build
	./issue-viewer -config demo/projects.yaml -port 8080

clean:
	rm -f issue-viewer issue-cli cover.out
	rm -rf .playtest

# Static validation chain: vet, full test suite, then a coverage gate on the
# cmd/issue-cli package. The gate fails the build when coverage drops below
# the configured floor so regressions can't slip in unnoticed.
CLI_COVERAGE_FLOOR ?= 70.0

vet:
	go vet ./...

test:
	go test ./... -count=1

cover-cli:
	@go test -coverprofile=cover.out ./cmd/issue-cli/... -count=1 >/dev/null
	@pct=$$(go tool cover -func=cover.out | awk '/^total:/ { sub("%","",$$3); print $$3 }'); \
	  printf "cmd/issue-cli coverage: %s%% (floor: %s%%)\n" "$$pct" "$(CLI_COVERAGE_FLOOR)"; \
	  awk -v p="$$pct" -v f="$(CLI_COVERAGE_FLOOR)" 'BEGIN { exit (p+0 < f+0) }' || \
	    { echo "FAIL: coverage below floor"; exit 1; }

validate: vet test cover-cli playtest-check
	@echo "✓ validate passed"

# Playtest sessions (playtest/sessions/*.yaml): an issue's manual checks run
# against a throwaway scenario board in Chrome. See docs/patterns/playtest/.
#   make playtest-check                       validate every session (no browser)
#   make ux-test [ISSUE=slug] [S=session] [STEP=TP2,TP3] [REPEAT=2]
#                                             machine checks, headless, desktop + phone
#   make playtest [ISSUE=slug] [S=session]    the owner's judgement steps, in an overlay
#   make playtest-report ISSUE=slug [APPLY=1] tick passed boxes, comment failures
# In a dispatched worktree (branch work/<slug>) ISSUE defaults to <slug>, so a
# plain `make playtest` runs that issue's sessions. Pass ISSUE= to run them all.
ISSUE ?= $(patsubst work/%,%,$(filter work/%,$(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)))
PLAYTEST = go run ./cmd/playtest
PLAYTEST_SEL = $(if $(ISSUE),-issue $(ISSUE)) $(if $(S),-s $(S))

playtest-check:
	@$(PLAYTEST) check

ux-test:
	@$(PLAYTEST) ux-test $(PLAYTEST_SEL) $(if $(STEP),-step $(STEP)) $(if $(REPEAT),-repeat $(REPEAT))

playtest:
	@$(PLAYTEST) playtest $(PLAYTEST_SEL)

playtest-report:
	@$(PLAYTEST) report -issue $(ISSUE) $(if $(S),-s $(S)) $(if $(APPLY),-apply)
