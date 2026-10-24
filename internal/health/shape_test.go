package health

import (
	"context"
	"strings"
	"testing"
	"time"
)

func goFunc(name string, body int) string {
	return "func " + name + "() {\n" + strings.Repeat("\t_ = 1\n", body) + "}\n"
}

func TestShapeFamily(t *testing.T) {
	now := time.Now()
	root, proj := healthRepo(t, now)
	// big_file_lines is 50 in healthRepo; long functions are over 80 lines.
	writeT(t, root, "svc/svc.go", "package svc\n\n"+goFunc("Long", 90)+goFunc("Short", 3)+"func (s *Svc) M() {\n}\n")
	writeT(t, root, "svc/svc_test.go", "package svc\n\n"+goFunc("TestHuge", 200)) // tests are not measured
	writeT(t, root, "web/app.js", "function render(a) {\n  if (a) {\n    return '}';\n  }\n}\n")
	writeT(t, root, "broken.go", "package x\nfunc (\n")
	writeT(t, root, "tool.rb", "def x\nend\n")
	commitAt(t, root, now.AddDate(0, 0, -2), "shape")
	for i := 0; i < 5; i++ { // svc.go: big (over 50 lines) and churned in 6 commits
		writeT(t, root, "svc/svc.go", "package svc\n\n"+goFunc("Long", 90+i+1)+goFunc("Short", 3)+"func (s *Svc) M() {\n}\n")
		commitAt(t, root, now.AddDate(0, 0, -1), "churn")
	}

	env, err := NewEnv(proj, root, now)
	if err != nil {
		t.Fatal(err)
	}
	snap, _, err := take(context.Background(), env, TriggerManual, []registered{{"shape", shapeFamily}})
	if err != nil {
		t.Fatal(err)
	}
	if e := snap.Families["shape"].Error; e != "" {
		t.Fatalf("shape error: %s", e)
	}
	want := map[string]float64{
		"shape.funcs{lang=go}":                             3,
		"shape.funcs{lang=js}":                             1,
		"shape.long_funcs":                                 1,
		"shape.long_func.lines{func=Long,path=svc/svc.go}": 97, // 95 body lines + signature + brace
		"shape.func_lines{stat=max}":                       97,
		"shape.unparsed_files{lang=go}":                    3, // broken.go, plus healthRepo\'s x-line main.go and app.go
		"shape.unparsed_files{lang=ruby}":                  1,
		"shape.churn.commits{path=svc/svc.go,window=30d}":  6,
		"shape.hotspot{path=svc/svc.go}":                   6 * 106, // 2 + 97 + 5 + 2 lines
	}
	for id, v := range want {
		got, ok := value(snap, id)
		if !ok || got != v {
			t.Errorf("%s = %v (present %v), want %v", id, got, ok, v)
		}
	}
	if _, ok := value(snap, "shape.long_func.lines{func=TestHuge,path=svc/svc_test.go}"); ok {
		t.Error("a test function was measured")
	}
	share, _ := value(snap, "shape.long_funcs.share")
	if share <= 0 || share >= 1 {
		t.Errorf("long_funcs.share = %v", share)
	}
	if v, _ := value(snap, "shape.churn.big_share{window=30d}"); v <= 0.5 {
		t.Errorf("churn lands in the big file, big_share = %v", v)
	}
}

func value(s *Snapshot, id string) (float64, bool) {
	for _, r := range s.Metrics {
		if r.ID == id {
			return r.Value, true
		}
	}
	return 0, false
}

func TestBraceFuncs(t *testing.T) {
	src := `namespace Game {
	public class Panel : Control {
		// a comment with { braces
		public int Count { get; set; }
		private static string Label(Loco l)
		{
			if (l == null) return "{";
			var c = '}';
			return l.Type switch
			{
				"a" => "A",
				_ => "B",
			};
		}
		public override void _Ready() {
			items.ForEach(x => {
				Use(x);
			});
			/* } */
			foreach (var i in items) { Use(i); }
		}
		public Panel(int n) : base(n) { }
	}
}`
	got := map[string]int{}
	for _, f := range braceFuncs([]byte(src), "csharp") {
		got[f.name] = f.lines
	}
	want := map[string]int{"Label": 10, "_Ready": 7, "Panel": 1}
	if len(got) != len(want) {
		t.Fatalf("found %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d lines, want %d (all: %v)", k, got[k], v, got)
		}
	}
}

func TestBraceFuncsJS(t *testing.T) {
	src := "const f = () => {\n  return 1;\n};\nfunction g(a, b) {\n  const s = '{';\n  return `${a}}`;\n}\nclass C {\n  m() {\n    if (x) { y(); }\n  }\n}\n"
	got := map[string]int{}
	for _, f := range braceFuncs([]byte(src), "js") {
		got[f.name] = f.lines
	}
	if len(got) != 2 || got["g"] != 4 || got["m"] != 3 {
		t.Errorf("found %v", got)
	}
}

func TestIndentFuncs(t *testing.T) {
	src := "import os\n\ndef a(x):\n    if x:\n        return 1\n\n    return 2\n\nasync def b():\n    pass\nclass K:\n    def c(self):\n        pass\n"
	got := map[string]int{}
	for _, f := range indentFuncs([]byte(src), "def ") {
		got[f.name] = f.lines
	}
	if got["a"] != 5 || got["b"] != 2 || got["c"] != 2 || len(got) != 3 {
		t.Errorf("found %v", got)
	}
}
