package telemetry

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// ReportVersion is the "v" of the report JSON — the contract the viewer's
// /stats page consumes.
const ReportVersion = 1

// RetryWindow bounds how long after a failure the next call from the same
// parent process still counts as the retry of that failure.
const RetryWindow = 120 * time.Second

// Manifest is the CLI surface the report compares usage against. The CLI
// builds it from its command registry; this package never imports the CLI.
type Manifest struct {
	Commands    []ManifestCommand `json:"commands"`
	Topics      []string          `json:"topics"`
	GlobalFlags []string          `json:"global_flags"`
}

// ManifestCommand is one top-level command.
type ManifestCommand struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Subcommands []string `json:"subcommands,omitempty"`
	// Flags maps a command key ("list", "data add") to its flag names.
	Flags map[string][]string `json:"flags,omitempty"`
	// FlagsUnknown lists command keys whose flags could not be introspected.
	FlagsUnknown []string `json:"flags_unknown,omitempty"`
}

// Report is the aggregated view of a set of events.
type Report struct {
	V      int    `json:"v"`
	Source string `json:"source"`
	// Enabled is false when recording is opted out (env or project config);
	// Disabled then says why. Previously recorded events are still reported.
	Enabled        bool          `json:"enabled"`
	Disabled       string        `json:"disabled,omitempty"`
	Since          string        `json:"since,omitempty"`
	Summary        Summary       `json:"summary"`
	Commands       []CommandStat `json:"commands"`
	NeverUsed      NeverUsed     `json:"never_used"`
	Errors         []ErrorStat   `json:"errors"`
	Unknown        []UnknownStat `json:"unknown"`
	RetrySequences []RetryStat   `json:"retry_sequences"`
}

type Summary struct {
	Events    int            `json:"events"`
	Failures  int            `json:"failures"`
	ErrorPct  float64        `json:"error_pct"`
	First     string         `json:"first,omitempty"`
	Last      string         `json:"last,omitempty"`
	Callers   map[string]int `json:"callers"`
	Assignees int            `json:"assignees"`
}

type CommandStat struct {
	Command  string  `json:"command"`
	Calls    int     `json:"calls"`
	Failures int     `json:"failures"`
	ErrorPct float64 `json:"error_pct"`
	P50MS    int64   `json:"p50_ms"`
}

type NeverUsed struct {
	Commands    []string            `json:"commands"`
	Aliases     []string            `json:"aliases"`
	Subcommands []string            `json:"subcommands"`
	Topics      []string            `json:"topics"`
	Flags       map[string][]string `json:"flags"`
	GlobalFlags []string            `json:"global_flags"`
}

type ErrorStat struct {
	Class   string `json:"class"`
	Command string `json:"command"`
	Count   int    `json:"count"`
}

type UnknownStat struct {
	Class string `json:"class"`
	Token string `json:"token"`
	Count int    `json:"count"`
}

type RetryStat struct {
	Failed string `json:"failed"`
	Next   string `json:"next"`
	Count  int    `json:"count"`
}

// Options control aggregation.
type Options struct {
	Source string
	Since  time.Time
	Top    int // 0 means 10
}

// Aggregate builds a Report from events (in file order) against manifest.
func Aggregate(events []Event, m Manifest, opt Options) Report {
	top := opt.Top
	if top <= 0 {
		top = 10
	}
	r := Report{V: ReportVersion, Source: opt.Source, Enabled: true}
	if !opt.Since.IsZero() {
		r.Since = opt.Since.UTC().Format(time.RFC3339)
	}
	r.Summary.Callers = map[string]int{}

	type cmdAgg struct {
		calls, fails int
		durs         []int64
	}
	cmds := map[string]*cmdAgg{}
	seenCmd := map[string]bool{}
	seenAlias := map[string]bool{}
	seenKey := map[string]bool{}
	seenTopic := map[string]bool{}
	seenFlag := map[string]map[string]bool{} // key -> flag
	seenGlobal := map[string]bool{}
	errs := map[[2]string]int{}
	unknown := map[[2]string]int{}
	assignees := map[string]bool{}

	globals := toSet(m.GlobalFlags)
	topics := toSet(m.Topics)

	markFlag := func(key, flag string) {
		if seenFlag[key] == nil {
			seenFlag[key] = map[string]bool{}
		}
		seenFlag[key][flag] = true
	}

	for _, e := range events {
		r.Summary.Events++
		if r.Summary.First == "" || e.TS < r.Summary.First {
			r.Summary.First = e.TS
		}
		if e.TS > r.Summary.Last {
			r.Summary.Last = e.TS
		}
		r.Summary.Callers[e.Caller]++
		if e.Assignee != "" {
			assignees[e.Assignee] = true
		}
		failed := e.Exit != 0
		if failed {
			r.Summary.Failures++
		}

		if e.Cmd != "" {
			key := e.CommandKey()
			a := cmds[key]
			if a == nil {
				a = &cmdAgg{}
				cmds[key] = a
			}
			a.calls++
			a.durs = append(a.durs, e.DurMS)
			if failed {
				a.fails++
			}
			seenCmd[e.Cmd] = true
			seenKey[key] = true
			if e.Alias != "" {
				seenAlias[e.Alias] = true
			}
			if (e.Cmd == "help" || e.Cmd == "process") && topics[e.Sub] {
				seenTopic[e.Sub] = true
			}
			for _, f := range e.Flags {
				if globals[f] {
					seenGlobal[f] = true
					continue
				}
				markFlag(key, f)
				markFlag(e.Cmd, f)
			}
		}
		if failed {
			class := e.ErrClass
			if class == "" {
				class = ErrOther
			}
			errs[[2]string{class, displayCmd(e)}]++
		}
		if e.Unknown != "" {
			unknown[[2]string{e.ErrClass, e.Unknown}]++
		}
	}
	r.Summary.Assignees = len(assignees)
	r.Summary.ErrorPct = pct(r.Summary.Failures, r.Summary.Events)

	for key, a := range cmds {
		r.Commands = append(r.Commands, CommandStat{
			Command:  key,
			Calls:    a.calls,
			Failures: a.fails,
			ErrorPct: pct(a.fails, a.calls),
			P50MS:    median(a.durs),
		})
	}
	sort.Slice(r.Commands, func(i, j int) bool {
		if r.Commands[i].Calls != r.Commands[j].Calls {
			return r.Commands[i].Calls > r.Commands[j].Calls
		}
		return r.Commands[i].Command < r.Commands[j].Command
	})

	nu := NeverUsed{Flags: map[string][]string{}}
	for _, c := range m.Commands {
		if !seenCmd[c.Name] {
			nu.Commands = append(nu.Commands, c.Name)
		}
		for _, a := range c.Aliases {
			if !seenAlias[a] {
				nu.Aliases = append(nu.Aliases, a)
			}
		}
		if c.Name != "help" && c.Name != "process" {
			for _, s := range c.Subcommands {
				if !seenKey[c.Name+" "+s] {
					nu.Subcommands = append(nu.Subcommands, c.Name+" "+s)
				}
			}
		}
		for key, flags := range c.Flags {
			for _, f := range flags {
				if !seenFlag[key][f] {
					nu.Flags[key] = append(nu.Flags[key], f)
				}
			}
			sort.Strings(nu.Flags[key])
		}
	}
	for _, t := range m.Topics {
		if !seenTopic[t] {
			nu.Topics = append(nu.Topics, t)
		}
	}
	for _, g := range m.GlobalFlags {
		if !seenGlobal[g] {
			nu.GlobalFlags = append(nu.GlobalFlags, g)
		}
	}
	sort.Strings(nu.Commands)
	sort.Strings(nu.Aliases)
	sort.Strings(nu.Subcommands)
	sort.Strings(nu.Topics)
	r.NeverUsed = nu

	for k, n := range errs {
		r.Errors = append(r.Errors, ErrorStat{Class: k[0], Command: k[1], Count: n})
	}
	sort.Slice(r.Errors, func(i, j int) bool {
		a, b := r.Errors[i], r.Errors[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Class != b.Class {
			return a.Class < b.Class
		}
		return a.Command < b.Command
	})
	r.Errors = capLen(r.Errors, top)

	for k, n := range unknown {
		r.Unknown = append(r.Unknown, UnknownStat{Class: k[0], Token: k[1], Count: n})
	}
	sort.Slice(r.Unknown, func(i, j int) bool {
		a, b := r.Unknown[i], r.Unknown[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Token < b.Token
	})
	r.Unknown = capLen(r.Unknown, top)

	r.RetrySequences = capLen(retrySequences(events, globals), top)

	// Stable empty slices so --json consumers never see null.
	if r.Commands == nil {
		r.Commands = []CommandStat{}
	}
	if r.Errors == nil {
		r.Errors = []ErrorStat{}
	}
	if r.Unknown == nil {
		r.Unknown = []UnknownStat{}
	}
	if r.RetrySequences == nil {
		r.RetrySequences = []RetryStat{}
	}
	for _, s := range []*[]string{&r.NeverUsed.Commands, &r.NeverUsed.Aliases, &r.NeverUsed.Subcommands, &r.NeverUsed.Topics, &r.NeverUsed.GlobalFlags} {
		if *s == nil {
			*s = []string{}
		}
	}
	return r
}

// retrySequences pairs each failed call with the next call from the same
// parent process (the same agent shell) within RetryWindow. That pairing is
// what shows where bots get confused and what they try next.
func retrySequences(events []Event, globals map[string]bool) []RetryStat {
	byPPID := map[int][]Event{}
	for _, e := range events {
		byPPID[e.PPID] = append(byPPID[e.PPID], e)
	}
	counts := map[[2]string]int{}
	for _, seq := range byPPID {
		sort.SliceStable(seq, func(i, j int) bool { return seq[i].TS < seq[j].TS })
		for i := 0; i+1 < len(seq); i++ {
			cur, next := seq[i], seq[i+1]
			if cur.Exit == 0 {
				continue
			}
			t1, err1 := time.Parse(time.RFC3339Nano, cur.TS)
			t2, err2 := time.Parse(time.RFC3339Nano, next.TS)
			if err1 != nil || err2 != nil || t2.Sub(t1) > RetryWindow {
				continue
			}
			class := cur.ErrClass
			if class == "" {
				class = ErrOther
			}
			failed := fmt.Sprintf("%s (%s)", displayCmd(cur), class)
			nextDesc := displayCmd(next)
			for _, f := range next.Flags {
				if !globals[f] {
					nextDesc += " --" + f
				}
			}
			switch {
			case next.ArgsHash == cur.ArgsHash:
				nextDesc = "same args again"
			case next.Exit == 0:
				nextDesc += " → ok"
			default:
				nextDesc += " → fail"
			}
			counts[[2]string{failed, nextDesc}]++
		}
	}
	var out []RetryStat
	for k, n := range counts {
		out = append(out, RetryStat{Failed: k[0], Next: k[1], Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Failed != out[j].Failed {
			return out[i].Failed < out[j].Failed
		}
		return out[i].Next < out[j].Next
	})
	return out
}

func displayCmd(e Event) string {
	if e.Cmd == "" {
		if e.Unknown != "" {
			return "?" + e.Unknown
		}
		return "?"
	}
	return e.CommandKey()
}

// RenderText writes the agent-facing report. Section headings are stable —
// bots and tests key off them.
func RenderText(w io.Writer, r Report) {
	fmt.Fprintln(w, "== Summary ==")
	fmt.Fprintf(w, "source:    %s\n", r.Source)
	if r.Since != "" {
		fmt.Fprintf(w, "since:     %s\n", r.Since)
	}
	fmt.Fprintf(w, "events:    %d (%d failed, %.1f%%)\n", r.Summary.Events, r.Summary.Failures, r.Summary.ErrorPct)
	if r.Summary.First != "" {
		fmt.Fprintf(w, "range:     %s .. %s\n", r.Summary.First, r.Summary.Last)
	}
	fmt.Fprintf(w, "callers:   %s\n", formatCounts(r.Summary.Callers))
	fmt.Fprintf(w, "assignees: %d\n", r.Summary.Assignees)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "== Commands ==")
	if len(r.Commands) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	width := 0
	for _, c := range r.Commands {
		if len(c.Command) > width {
			width = len(c.Command)
		}
	}
	for _, c := range r.Commands {
		fmt.Fprintf(w, "  %-*s  %5d calls  %5.1f%% err  p50 %dms\n", width, c.Command, c.Calls, c.ErrorPct, c.P50MS)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "== Never used ==")
	writeList(w, "commands", r.NeverUsed.Commands)
	writeList(w, "aliases", r.NeverUsed.Aliases)
	writeList(w, "subcommands", r.NeverUsed.Subcommands)
	writeList(w, "topics", r.NeverUsed.Topics)
	writeList(w, "global flags", prefixFlags(r.NeverUsed.GlobalFlags))
	keys := make([]string, 0, len(r.NeverUsed.Flags))
	for k := range r.NeverUsed.Flags {
		if len(r.NeverUsed.Flags[k]) > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		fmt.Fprintln(w, "  flags: (none)")
	} else {
		fmt.Fprintln(w, "  flags:")
		for _, k := range keys {
			fmt.Fprintf(w, "    %s: %s\n", k, strings.Join(prefixFlags(r.NeverUsed.Flags[k]), " "))
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "== Errors ==")
	if len(r.Errors) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, e := range r.Errors {
		fmt.Fprintf(w, "  %4d  %-20s %s\n", e.Count, e.Class, e.Command)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "== Unknown ==")
	if len(r.Unknown) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, u := range r.Unknown {
		fmt.Fprintf(w, "  %4d  %-20s %s\n", u.Count, u.Class, u.Token)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "== Retry sequences ==")
	if len(r.RetrySequences) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, s := range r.RetrySequences {
		fmt.Fprintf(w, "  %4d  %s  ⇒  %s\n", s.Count, s.Failed, s.Next)
	}
}

func writeList(w io.Writer, label string, items []string) {
	if len(items) == 0 {
		fmt.Fprintf(w, "  %s: (none)\n", label)
		return
	}
	fmt.Fprintf(w, "  %s: %s\n", label, strings.Join(items, ", "))
}

func prefixFlags(flags []string) []string {
	out := make([]string, len(flags))
	for i, f := range flags {
		out[i] = "--" + f
	}
	return out
}

func formatCounts(m map[string]int) string {
	if len(m) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, m[k])
	}
	return strings.Join(parts, " ")
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, i := range items {
		s[i] = true
	}
	return s
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(int(float64(n)*1000/float64(d)+0.5)) / 10
}

func median(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func capLen[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}
