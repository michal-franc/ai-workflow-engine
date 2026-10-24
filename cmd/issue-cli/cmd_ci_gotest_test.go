package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestGoTestTextPrintsWhatPlainGoTestPrints(t *testing.T) {
	in := strings.Join([]string{
		`{"Action":"run","Package":"m/a","Test":"TestOK"}`,
		`{"Action":"output","Package":"m/a","Test":"TestOK","Output":"=== RUN   TestOK\n"}`,
		`{"Action":"output","Package":"m/a","Test":"TestOK","Output":"--- PASS: TestOK (0.00s)\n"}`,
		`{"Action":"pass","Package":"m/a","Test":"TestOK"}`,
		`{"Action":"output","Package":"m/a","Test":"TestBad","Output":"=== RUN   TestBad\n"}`,
		`{"Action":"output","Package":"m/a","Test":"TestBad/sub","Output":"    bad_test.go:9: boom\n"}`,
		`{"Action":"fail","Package":"m/a","Test":"TestBad/sub"}`,
		`{"Action":"output","Package":"m/a","Test":"TestBad","Output":"--- FAIL: TestBad (0.01s)\n"}`,
		`{"Action":"fail","Package":"m/a","Test":"TestBad"}`,
		`{"Action":"output","Package":"m/a","Output":"FAIL\n"}`,
		`{"Action":"output","Package":"m/a","Output":"FAIL\tm/a\t0.02s\n"}`,
		`{"Action":"fail","Package":"m/a"}`,
		`{"Action":"output","Package":"m/b","Output":"PASS\n"}`,
		`{"Action":"output","Package":"m/b","Output":"ok  \tm/b\t0.01s\n"}`,
		`{"Action":"pass","Package":"m/b"}`,
		`# m/c [build failed]`,
	}, "\n") + "\n"
	var out, raw bytes.Buffer
	failed, events, err := goTestText(strings.NewReader(in), &out, &raw)
	if err != nil || !failed || events != 15 {
		t.Fatalf("failed=%v events=%d err=%v", failed, events, err)
	}
	want := "=== RUN   TestBad\n    bad_test.go:9: boom\n--- FAIL: TestBad (0.01s)\nFAIL\nFAIL\tm/a\t0.02s\nok  \tm/b\t0.01s\n# m/c [build failed]\n"
	if out.String() != want {
		t.Errorf("text =\n%s\nwant\n%s", out.String(), want)
	}
	if raw.String() != in {
		t.Error("the raw stream must be saved unchanged")
	}
}

func TestCIGoTestNeedsEvents(t *testing.T) {
	var out, errw bytes.Buffer
	ctx := &Context{Stdin: strings.NewReader("ok  \tm\t0.1s\n"), Stdout: &out, Stderr: &errw}
	err := runCIGoTest(ctx, []string{"--out", t.TempDir() + "/unit.json"})
	if err == nil || !strings.Contains(errw.String(), "go test -json") {
		t.Errorf("err=%v stderr=%q", err, errw.String())
	}
}
