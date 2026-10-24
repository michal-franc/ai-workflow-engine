package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/michal-franc/issue-viewer/internal/ci"
)

// runCIGoTest is `issue-cli ci gotest`: go test -json on stdin, plain go test
// text on stdout, the raw stream saved for local CI's results: entry.
//
//	go test ./... -json | issue-cli ci gotest --out $CI_RESULTS_DIR/unit.json
//
// It prints what go test without -v prints: the output of failing tests, the
// ok/FAIL line per package, build errors. It exits 1 when any test or package
// failed, so the pipe keeps go test's verdict without pipefail.
func runCIGoTest(ctx *Context, args []string) error {
	fs := newFlagSet("ci gotest", ctx)
	outFlag := fs.String("out", "", "save the raw go test -json stream here (default: $CI_RESULTS_DIR/unit.json when set)")
	if err := parseFlags(ctx, fs, args); err != nil {
		return err
	}
	path := *outFlag
	if path == "" {
		if dir := os.Getenv(ci.ResultsEnv); dir != "" {
			path = filepath.Join(dir, "unit.json")
		}
	}
	var raw io.Writer = io.Discard
	if path != "" {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		raw = f
	}
	failed, events, err := goTestText(ctx.Stdin, ctx.Stdout, raw)
	if err != nil {
		return err
	}
	if events == 0 {
		fmt.Fprintln(ctx.Stderr, "ci gotest: no go test -json events on stdin (pipe `go test -json ./...` into it)")
		return &exitCodeError{Code: 1}
	}
	if failed {
		return &exitCodeError{Code: 1}
	}
	return nil
}

// goTestText copies in to raw and writes go test's non-verbose text to out.
// A test's output is held until it ends and printed only when it failed.
func goTestText(in io.Reader, out, raw io.Writer) (failed bool, events int, err error) {
	type event struct {
		Action  string
		Package string
		Test    string
		Output  string
	}
	held := map[string][]string{} // package + top-level test → output lines
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	w := bufio.NewWriter(out)
	defer w.Flush()
	for sc.Scan() {
		line := sc.Bytes()
		raw.Write(line)
		raw.Write([]byte{'\n'})
		var e event
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil {
			fmt.Fprintf(w, "%s\n", line) // not part of the stream: show it
			continue
		}
		events++
		top, _, _ := strings.Cut(e.Test, "/")
		key := e.Package + "\x00" + top
		switch e.Action {
		case "output", "build-output":
			if e.Test == "" {
				if e.Output != "PASS\n" {
					w.WriteString(e.Output)
				}
				continue
			}
			held[key] = append(held[key], e.Output)
		case "fail", "build-fail":
			failed = true
			if e.Test != "" && e.Test == top {
				for _, o := range held[key] {
					w.WriteString(o)
				}
				delete(held, key)
			}
		case "pass", "skip":
			if e.Test != "" && e.Test == top {
				delete(held, key)
			}
		}
	}
	return failed, events, sc.Err()
}
