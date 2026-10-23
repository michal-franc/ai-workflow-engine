package clilog

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRedactKeepsStructureAndHidesText(t *testing.T) {
	args := []string{
		"--project", "issue-viewer", "--json", "comment", "cli/foo",
		"--text", "progress: secret plan", "--how", "lead chat",
		"--to", "in progress", "--field", "waiting=design review",
		"--body=- [ ] one", "--dry-run", "check", "D3", "AC1",
		"report-bug", "it broke when I typed hunter2 here",
	}
	got := Redact(args)
	want := []string{
		"--project", "issue-viewer", "--json", "comment", "cli/foo",
		"--text", Placeholder("progress: secret plan"), "--how", "lead chat",
		"--to", "in progress", "--field", "waiting=" + Placeholder("design review"),
		"--body=" + Placeholder("- [ ] one"), "--dry-run", "check", "D3", "AC1",
		"report-bug", Placeholder("it broke when I typed hunter2 here"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Redact:\n got %q\nwant %q", got, want)
	}
}

func TestRedactIsIdempotent(t *testing.T) {
	args := []string{"comment", "x", "--text", "a b", "--field", "k=v w", "free text here"}
	once := Redact(args)
	if twice := Redact(once); !reflect.DeepEqual(once, twice) {
		t.Fatalf("second pass changed args:\n%q\n%q", once, twice)
	}
}

func TestRedactValueThatLooksLikeAFlag(t *testing.T) {
	got := Redact([]string{"append", "x", "--body", "--secret", "--section", "Design"})
	want := []string{"append", "x", "--body", Placeholder("--secret"), "--section", "Design"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRedactUnknownFlagHidesValue(t *testing.T) {
	got := Redact([]string{"show", "x", "--brand-new", "token123"})
	if got[3] != Placeholder("token123") {
		t.Fatalf("unknown flag value kept: %q", got)
	}
}

func TestRedactLongPositional(t *testing.T) {
	long := strings.Repeat("a", maxKeptPositional+1)
	got := Redact([]string{"show", long})
	if got[1] != Placeholder(long) {
		t.Fatalf("long positional kept: %q", got[1])
	}
}

func TestPlaceholderStable(t *testing.T) {
	p := Placeholder("héllo")
	if p != Placeholder("héllo") || !IsPlaceholder(p) || !strings.Contains(p, "n=5 ") {
		t.Fatalf("placeholder %q", p)
	}
	if Placeholder("a") == Placeholder("b") {
		t.Fatal("different text, same placeholder")
	}
}

// TestBoolFlagsCoverCLI keeps boolFlags in step with the bool flags
// cmd/issue-cli defines, so a positional after one is not redacted as its
// value.
func TestBoolFlagsCoverCLI(t *testing.T) {
	files, err := filepath.Glob("../../cmd/issue-cli/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no cmd/issue-cli sources: %v", err)
	}
	re := regexp.MustCompile(`\.Bool(?:Var)?\([^"\n]*"([a-z0-9-]+)"`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			if !boolFlags[m[1]] {
				t.Errorf("%s defines bool flag --%s; add it to boolFlags", filepath.Base(f), m[1])
			}
		}
	}
}

func TestAppendAndReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agent-logs", FileName)
	exit, dur := 1, int64(1500)
	rec := Record{TS: "2026-10-23T10:00:00Z", PID: 1, PPID: 2, Args: []string{"show", "x"}, Exit: &exit, ErrClass: "validation", DurMS: &dur}
	if err := Append(path, rec); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".1", []byte(`{"ts":"2026-10-22T09:00:00Z","pid":3,"ppid":4,"args":["next"]}`+"\nnot json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []Record
	Read(path, func(r Record) { got = append(got, r) })
	if len(got) != 2 || got[0].Args[0] != "next" || got[0].Exit != nil || *got[1].Exit != 1 || got[1].ErrClass != "validation" {
		t.Fatalf("read %+v", got)
	}
	if !got[1].End().Equal(got[1].Start().Add(1500 * time.Millisecond)) {
		t.Fatalf("End %v Start %v", got[1].End(), got[1].Start())
	}
	if !got[0].End().Equal(got[0].Start()) {
		t.Fatal("old record End should equal Start")
	}
}

func TestStripGlobalAndFlagValue(t *testing.T) {
	args := []string{"--config", "c.yaml", "--project", "p", "--json", "transition", "x", "--to=done"}
	if got := StripGlobal(args); !reflect.DeepEqual(got, []string{"transition", "x", "--to=done"}) {
		t.Fatalf("StripGlobal %q", got)
	}
	if FlagValue(args, "--to") != "done" || FlagValue(args, "--project") != "p" || FlagValue(args, "--x") != "" {
		t.Fatal("FlagValue")
	}
}
