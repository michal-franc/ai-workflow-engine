package tracker

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPrivacyBlockParsesAndLints(t *testing.T) {
	var wf WorkflowConfig
	src := "privacy:\n  work_files: tracker\n  work_paths: [issues, notes/]\n  code: snapshots\n  public_remote: pub\n  public_branch: trunk\n  decided: \"2026-10-24, owner: \\\"yes\\\"\"\n"
	if err := yaml.Unmarshal([]byte(src), &wf); err != nil {
		t.Fatal(err)
	}
	p := wf.Privacy
	if !p.Tracker() || !p.Snapshots() || p.Remote() != "pub" || p.Branch() != "trunk" {
		t.Errorf("privacy = %+v", p)
	}
	if got := strings.Join(p.Paths(), ","); got != "issues,notes" {
		t.Errorf("Paths = %s", got)
	}
	if lint := wf.Lint(); len(lint) != 0 {
		t.Errorf("Lint = %v", lint)
	}

	var none *PrivacyConfig
	if none.Tracker() || none.Snapshots() || none.Remote() != "public" || none.Branch() != "main" || strings.Join(none.Paths(), ",") != "issues,sessions,retros,bugs,.health/snapshots" {
		t.Error("nil privacy defaults are wrong")
	}

	bad := &PrivacyConfig{WorkFiles: "cloud", Code: "open", WorkPaths: []string{"/", "../x"}, PublicRemote: "pub"}
	got := strings.Join(bad.Validate(), "\n")
	for _, want := range []string{`privacy.work_files "cloud" is not tracker or in_repo`, `privacy.code "open" is not snapshots or public`, `work_paths[0] "/" is empty or the project root`, `work_paths[1] "../x" is outside the project`, "public_remote and public_branch only apply with code: snapshots", "privacy.decided is empty"} {
		if !strings.Contains(got, want) {
			t.Errorf("Validate lacks %q:\n%s", want, got)
		}
	}
}
