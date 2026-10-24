.PHONY: build build-cli install demo clean validate test vet cover-cli catalog-check playtest-check ux-test playtest playtest-report

# The build's version is `git describe`: the tag on a release or release
# candidate (v0.54.0-rc.1), "<tag>-N-g<sha>" past one, "-dirty" with
# uncommitted changes. `issue-cli version`, the sidebar and the usage log
# show it. Override with make install VERSION=...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.Version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o issue-viewer .

build-cli:
	go build -ldflags "$(LDFLAGS)" -o issue-cli ./cmd/issue-cli/

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/issue-cli/
	go install -ldflags "$(LDFLAGS)" .
	@echo "Installed issue-cli and issue-viewer $(VERSION) via go install"

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

# In local CI ($CI_RESULTS_DIR set) the tests also write go test -json for the
# health tests family; ci gotest prints the same text plain go test would.
test:
	go test ./... -count=1 $(if $(CI_RESULTS_DIR),-json | go run ./cmd/issue-cli ci gotest --out $(CI_RESULTS_DIR)/unit.json)

cover-cli:
	@go test -coverprofile=cover.out ./cmd/issue-cli/... -count=1 >/dev/null
	@pct=$$(go tool cover -func=cover.out | awk '/^total:/ { sub("%","",$$3); print $$3 }'); \
	  printf "cmd/issue-cli coverage: %s%% (floor: %s%%)\n" "$$pct" "$(CLI_COVERAGE_FLOOR)"; \
	  awk -v p="$$pct" -v f="$(CLI_COVERAGE_FLOOR)" 'BEGIN { exit (p+0 < f+0) }' || \
	    { echo "FAIL: coverage below floor"; exit 1; }

# A branch that changes a catalog feature's code (its sources:) or a bundled
# template must bump its version past the last release, or say why not with
# a `Catalog: n/a <id> <why>` trailer. Skipped on main.
catalog-check:
	@go run ./cmd/issue-cli workflow catalog-check

validate: vet test cover-cli catalog-check playtest-check
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
