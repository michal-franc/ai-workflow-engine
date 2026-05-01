.PHONY: build build-cli install demo clean validate test vet cover-cli

build:
	go build -o issue-viewer .

build-cli:
	go build -o issue-cli ./cmd/issue-cli/

install:
	go install ./cmd/issue-cli/
	@echo "Installed issue-cli via go install"

demo: build
	./issue-viewer -config demo/projects.yaml -port 8080

clean:
	rm -f issue-viewer issue-cli cover.out

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

validate: vet test cover-cli
	@echo "✓ validate passed"
