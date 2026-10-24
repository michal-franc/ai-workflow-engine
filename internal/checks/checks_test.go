package checks

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeScript(t *testing.T, root, name, body string) string {
	t.Helper()
	path := filepath.Join(root, Dir, name+".sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindMatchesTheIssueHeader(t *testing.T) {
	wt, main := t.TempDir(), t.TempDir()
	writeScript(t, wt, "lead-tab", "#!/usr/bin/env bash\n# issue: api/lead-tab\nsource x\n")
	writeScript(t, wt, "other", "#!/usr/bin/env bash\n# issue: api/other\n")
	writeScript(t, wt, "no-header", "#!/usr/bin/env bash\necho hi\n")
	// The same name in a later root loses to the first.
	writeScript(t, main, "lead-tab", "#!/usr/bin/env bash\n# issue: api/lead-tab\n")
	writeScript(t, main, "late", "#!/usr/bin/env bash\n# a comment\n#issue:api/lead-tab trailing words\n")

	got := Find([]string{wt, "", filepath.Join(wt, "missing"), main}, "api/lead-tab")
	var names, roots []string
	for _, s := range got {
		names = append(names, s.Name)
		roots = append(roots, s.Root)
	}
	if !reflect.DeepEqual(names, []string{"lead-tab", "late"}) {
		t.Fatalf("names = %v", names)
	}
	if !reflect.DeepEqual(roots, []string{wt, main}) {
		t.Fatalf("roots = %v", roots)
	}
}

func TestIssueOfOnlyReadsTheTopOfTheFile(t *testing.T) {
	root := t.TempDir()
	body := "#!/usr/bin/env bash\n" + strings.Repeat("# filler\n", headerLines) + "# issue: too/late\n"
	path := writeScript(t, root, "deep", body)
	if got := IssueOf(path); got != "" {
		t.Fatalf("IssueOf = %q, want none past line %d", got, headerLines)
	}
}

func TestDueUntilAnOwnerRunIsRecorded(t *testing.T) {
	root := t.TempDir()
	writeScript(t, root, "a", "# issue: x/y\n")
	writeScript(t, root, "b", "# issue: x/y\n")
	if got := Due([]string{root}, "x/y"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Due = %v", got)
	}
	// A machine run doesn't count; an owner run does.
	rec := filepath.Join(root, RecordDir, "a")
	os.MkdirAll(rec, 0o755)
	os.WriteFile(filepath.Join(rec, "20261023T100000Z-machine.txt"), nil, 0o644)
	if got := Due([]string{root}, "x/y"); len(got) != 2 {
		t.Fatalf("a machine run cleared it: %v", got)
	}
	os.WriteFile(filepath.Join(rec, "20261023T110000Z-owner.txt"), nil, 0o644)
	if got := Due([]string{root}, "x/y"); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("Due after the owner run = %v", got)
	}
}

func TestWindowName(t *testing.T) {
	if got := WindowName("api/leads-only.terminal"); got != "check-script-api-leads-only-terminal" {
		t.Fatalf("WindowName = %q", got)
	}
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{"lead-tab": true, "prove2": true, "": false, "-x": false, "Lead": false, "a/b": false, "a.sh": false} {
		if ValidName(name) != want {
			t.Errorf("ValidName(%q) = %v", name, !want)
		}
	}
}

func TestLibPathIsStableAndHoldsTheLibrary(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	a, err := LibPath()
	if err != nil {
		t.Fatal(err)
	}
	b, err := LibPath()
	if err != nil || a != b {
		t.Fatalf("second LibPath = %q, %v; want %q", b, err, a)
	}
	data, _ := os.ReadFile(a)
	if string(data) != string(Lib) {
		t.Fatal("the cached library differs from the embedded one")
	}
}

func TestSkeletonIsValidBashWithOneStepPerBox(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	sk := Skeleton("cli/x", "x", []Box{{ID: "TP2", Text: "The owner's sandbox"}, {ID: "TP3", Text: "steps written"}})
	for _, want := range []string{"# issue: cli/x\n", `source "${CHECKS_LIB:?`, "step TP2 'The owner'\\''s sandbox' --box TP2", "step TP3 'steps written' --box TP3", "\nsummary\n"} {
		if !strings.Contains(sk, want) {
			t.Errorf("skeleton lacks %q:\n%s", want, sk)
		}
	}
	path := writeScript(t, t.TempDir(), "x", sk)
	if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}
	if IssueOf(path) != "cli/x" {
		t.Fatal("the skeleton's header isn't found")
	}
	if !strings.Contains(Skeleton("cli/x", "x", nil), "step MV1 ") {
		t.Fatal("no boxes: want one example step")
	}
}
