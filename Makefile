.PHONY: build build-cli install demo clean

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
	rm -f issue-viewer issue-cli
