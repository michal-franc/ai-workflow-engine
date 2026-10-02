package telemetry

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var testManifest = Manifest{
	Commands: []ManifestCommand{
		{Name: "list", Flags: map[string][]string{"list": {"status", "system"}}},
		{Name: "context", Aliases: []string{"show"}},
		{Name: "transition", Flags: map[string][]string{"transition": {"field", "to"}}},
		{Name: "data", Subcommands: []string{"add", "list"}, Flags: map[string][]string{"data add": {"description"}}},
		{Name: "process", Subcommands: []string{"workflow", "transitions"}},
		{Name: "search"},
	},
	Topics:      []string{"workflow", "transitions"},
	GlobalFlags: []string{"config", "project", "json"},
}

type evOpt func(*Event)

func ev(cmd string, at time.Time, opts ...evOpt) Event {
	e := Event{V: 1, TS: at.UTC().Format(time.RFC3339Nano), Cmd: cmd, Caller: CallerAgent, PPID: 100, ArgsHash: cmd}
	for _, o := range opts {
		o(&e)
	}
	return e
}

func fail(class string) evOpt { return func(e *Event) { e.Exit = 1; e.ErrClass = class } }
func flags(f ...string) evOpt { return func(e *Event) { e.Flags = f } }
func sub(s string) evOpt      { return func(e *Event) { e.Sub = s } }
func hash(h string) evOpt     { return func(e *Event) { e.ArgsHash = h } }
func unknown(u string) evOpt  { return func(e *Event) { e.Unknown = u } }
func ppid(p int) evOpt        { return func(e *Event) { e.PPID = p } }

func fixtureEvents(base time.Time) []Event {
	at := func(s int) time.Time { return base.Add(time.Duration(s) * time.Second) }
	return []Event{
		ev("list", at(0), flags("project", "status")),
		ev("context", at(1), func(e *Event) { e.Alias = "show"; e.Assignee = "agent-a" }),
		ev("transition", at(2), flags("to"), fail(ErrValidation), hash("t1")),
		ev("transition", at(3), flags("to"), fail(ErrValidation), hash("t1")),
		ev("transition", at(4), flags("project", "to"), hash("t2")),
		ev("", at(5), fail(ErrUnknownCommand), unknown("add-comment"), hash("u1")),
		ev("process", at(6), sub("transitions")),
		ev("data", at(7), sub("add"), flags("description"), fail(ErrUnknownFlag), unknown("--message")),
		// different shell: a failure whose next call is > RetryWindow later
		ev("list", at(10), ppid(200), fail(ErrFlagParse)),
		ev("list", at(10+int(RetryWindow/time.Second)+5), ppid(200)),
	}
}

func TestAggregateCountsCommandsAndSummary(t *testing.T) {
	r := Aggregate(fixtureEvents(time.Now()), testManifest, Options{Source: "/x"})
	if r.V != ReportVersion || !r.Enabled || r.Source != "/x" {
		t.Fatalf("header wrong: %+v", r)
	}
	if r.Summary.Events != 10 || r.Summary.Failures != 5 || r.Summary.ErrorPct != 50 {
		t.Fatalf("summary wrong: %+v", r.Summary)
	}
	if r.Summary.Assignees != 1 || r.Summary.Callers[CallerAgent] != 10 {
		t.Fatalf("callers/assignees wrong: %+v", r.Summary)
	}
	// list and transition tie on 3 calls; ties sort by name.
	if r.Commands[0].Command != "list" || r.Commands[1].Command != "transition" {
		t.Fatalf("expected list, transition first, got %+v", r.Commands[:2])
	}
	if tr := r.Commands[1]; tr.Calls != 3 || tr.Failures != 2 || tr.ErrorPct != 66.7 {
		t.Fatalf("transition row wrong: %+v", tr)
	}
	for _, c := range r.Commands {
		if c.Command == "" {
			t.Fatal("unknown-command events must not appear as a command row")
		}
	}
}

func TestAggregateNeverUsed(t *testing.T) {
	r := Aggregate(fixtureEvents(time.Now()), testManifest, Options{})
	nu := r.NeverUsed
	if strings.Join(nu.Commands, ",") != "search" {
		t.Fatalf("never-used commands: %v", nu.Commands)
	}
	if len(nu.Aliases) != 0 {
		t.Fatalf("show alias was used: %v", nu.Aliases)
	}
	if strings.Join(nu.Subcommands, ",") != "data list" {
		t.Fatalf("never-used subcommands: %v", nu.Subcommands)
	}
	if strings.Join(nu.Topics, ",") != "workflow" {
		t.Fatalf("never-used topics: %v", nu.Topics)
	}
	if strings.Join(nu.Flags["list"], ",") != "system" {
		t.Fatalf("list never-used flags: %v", nu.Flags["list"])
	}
	if strings.Join(nu.Flags["transition"], ",") != "field" {
		t.Fatalf("transition never-used flags: %v", nu.Flags["transition"])
	}
	if len(nu.Flags["data add"]) != 0 {
		t.Fatalf("data add --description was used: %v", nu.Flags["data add"])
	}
	if strings.Join(nu.GlobalFlags, ",") != "config,json" {
		t.Fatalf("never-used global flags: %v", nu.GlobalFlags)
	}
}

func TestAggregateErrorsAndUnknown(t *testing.T) {
	r := Aggregate(fixtureEvents(time.Now()), testManifest, Options{})
	if r.Errors[0].Class != ErrValidation || r.Errors[0].Command != "transition" || r.Errors[0].Count != 2 {
		t.Fatalf("top error wrong: %+v", r.Errors)
	}
	foundUnknownCmd := false
	for _, e := range r.Errors {
		if e.Class == ErrUnknownCommand && e.Command == "?add-comment" {
			foundUnknownCmd = true
		}
	}
	if !foundUnknownCmd {
		t.Fatalf("unknown command should show as ?token: %+v", r.Errors)
	}
	tokens := map[string]string{}
	for _, u := range r.Unknown {
		tokens[u.Token] = u.Class
	}
	if tokens["add-comment"] != ErrUnknownCommand || tokens["--message"] != ErrUnknownFlag {
		t.Fatalf("unknown tokens: %+v", r.Unknown)
	}
}

func TestAggregateRetrySequences(t *testing.T) {
	r := Aggregate(fixtureEvents(time.Now()), testManifest, Options{})
	got := map[string]int{}
	for _, s := range r.RetrySequences {
		got[s.Failed+" => "+s.Next] = s.Count
	}
	want := map[string]int{
		"transition (validation) => same args again":                 1,
		"transition (validation) => transition --to → ok":            1, // --project is global, omitted
		"?add-comment (unknown_command) => process transitions → ok": 1,
		"data add (unknown_flag) => list → fail":                     0, // next is another shell
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("retry %q: got %d want %d (all: %v)", k, got[k], n, got)
		}
	}
	for k := range got {
		if strings.HasPrefix(k, "list (flag_parse)") {
			t.Errorf("retry outside the window must not count: %q", k)
		}
	}
}

func TestAggregateTopCapsLists(t *testing.T) {
	r := Aggregate(fixtureEvents(time.Now()), testManifest, Options{Top: 1})
	if len(r.Errors) != 1 || len(r.Unknown) != 1 || len(r.RetrySequences) != 1 {
		t.Fatalf("top=1 not applied: %d %d %d", len(r.Errors), len(r.Unknown), len(r.RetrySequences))
	}
}

func TestAggregateEmptyProducesNonNullJSON(t *testing.T) {
	r := Aggregate(nil, Manifest{}, Options{})
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "null") {
		t.Fatalf("empty report must not contain null lists: %s", b)
	}
}

func TestRenderTextHasStableSections(t *testing.T) {
	var buf bytes.Buffer
	RenderText(&buf, Aggregate(fixtureEvents(time.Now()), testManifest, Options{Source: "/x"}))
	out := buf.String()
	for _, h := range []string{"== Summary ==", "== Commands ==", "== Never used ==", "== Errors ==", "== Unknown ==", "== Retry sequences =="} {
		if !strings.Contains(out, h) {
			t.Errorf("missing section %q in:\n%s", h, out)
		}
	}
	for _, s := range []string{"commands: search", "list: --system", "add-comment", "same args again"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
	buf.Reset()
	RenderText(&buf, Aggregate(nil, Manifest{}, Options{}))
	if !strings.Contains(buf.String(), "(none)") {
		t.Fatalf("empty report should say (none):\n%s", buf.String())
	}
}
