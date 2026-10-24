package checks

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// testdata/ported holds the first hand-written check scripts (prove,
// dispatch-status-cli, lead-tab), rewritten on the library: proof that it
// covers what they needed. They must stay library scripts: a header, the
// source line, and none of the helpers they used to inline.
var inlinedHelper = regexp.MustCompile(`(?m)^\s*(step|run|expect|ask|pause|begin|verdict|summary|quit_if_q|skip)\s*\(\)`)

func TestPortedScriptsUseTheLibrary(t *testing.T) {
	needBash(t)
	files, _ := filepath.Glob(filepath.Join("testdata", "ported", "*.sh"))
	if len(files) != 3 {
		t.Fatalf("want the three ported scripts, got %v", files)
	}
	_, shellcheck := exec.LookPath("shellcheck")
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if IssueOf(f) == "" {
			t.Errorf("%s: no # issue: header", f)
		}
		if !regexp.MustCompile(`(?m)^source "\$\{CHECKS_LIB:\?`).Match(data) {
			t.Errorf("%s: doesn't source $CHECKS_LIB", f)
		}
		if m := inlinedHelper.Find(data); m != nil {
			t.Errorf("%s: defines its own %q: use the library's", f, m)
		}
		if out, err := exec.Command("bash", "-n", f).CombinedOutput(); err != nil {
			t.Errorf("%s: bash -n: %s", f, out)
		}
		if shellcheck == nil {
			if out, err := exec.Command("shellcheck", "-s", "bash", "-S", "warning", f).CombinedOutput(); err != nil {
				t.Errorf("%s: shellcheck:\n%s", f, out)
			}
		}
	}
}
