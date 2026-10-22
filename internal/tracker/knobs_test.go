package tracker

import (
	"strings"
	"testing"
)

const tuningV1 = `package sim

// Tuning knobs.
const (
	PrepTime    = 30   // stage 1 set-up
	CallNoise   = 0.4f // AI call blur
	RoleNatural = 70, RoleCapable = 55
)
`

func knobByName(ks []Knob, name string) *Knob {
	for i := range ks {
		if ks[i].Name == name {
			return &ks[i]
		}
	}
	return nil
}

func TestLoadKnobsHistoryAndBranchEdits(t *testing.T) {
	r := newGitRepo(t)
	r.write("Sim/Tune.go", tuningV1)
	r.commit("ET core: knobs")
	r.write("Sim/Tune.go", strings.Replace(tuningV1, "PrepTime    = 30", "PrepTime    = 20", 1))
	r.commit("Play calls balance pass 2")
	r.write("Sim/Tune.go", strings.NewReplacer("PrepTime    = 30", "PrepTime    = 20", "CallNoise   = 0.4f", "CallNoise   = 0.6f", "RoleCapable = 55", "RoleCapable = 58").Replace(tuningV1))
	r.commit("Play calls balance pass 1")
	current := strings.NewReplacer("PrepTime    = 30", "PrepTime    = 20", "CallNoise   = 0.4f", "CallNoise   = 0.6f", "RoleCapable = 55", "RoleCapable = 58").Replace(tuningV1)

	wt := r.worktree("work/ai/weights", "main")
	r.writeIn(wt, "Sim/Tune.go", strings.Replace(current, "CallNoise   = 0.6f", "CallNoise   = 0.5f", 1))
	r.commitIn(wt, "weights")

	r.write("build/knobs.json", `{"knobs":[
	  {"name":"PrepTime","value":"20","comment":"stage 1 set-up","file":"Sim/Tune.go","line":5},
	  {"name":"CallNoise","value":0.6,"comment":"AI call blur","file":"Sim/Tune.go","line":6},
	  {"name":"RoleCapable","value":"58","file":"Sim/Tune.go","line":7},
	  {"name":"Loose","value":true},
	  {"name":"Escapes","value":"1","file":"../x.go","line":1}]}`)

	wb := WorkbenchConfig{
		Tuning:  []string{"build/knobs.json", "missing.json"},
		Systems: map[string]WorkbenchSystem{"AI": {Knobs: []string{"Call*", "Role*"}}, "Sim": {Knobs: []string{"PrepTime"}}},
	}
	branches := OpenBranches(r.root, "main", []*Issue{{Slug: "ai/weights", Status: "in progress"}})
	knobs, problems := LoadKnobs(r.root, wb, branches)

	if len(knobs) != 5 {
		t.Fatalf("knobs = %+v", knobs)
	}
	if len(problems) != 2 || !strings.Contains(strings.Join(problems, "|"), "missing.json: no such file") || !strings.Contains(strings.Join(problems, "|"), "outside the project") {
		t.Errorf("problems = %v", problems)
	}

	prep := knobByName(knobs, "PrepTime")
	if len(prep.History) != 2 || prep.History[0].Label() != "30 → 20" || prep.History[0].Subject != "Play calls balance pass 2" || prep.History[1].Label() != "added 30" {
		t.Errorf("PrepTime history = %+v", prep.History)
	}
	noise := knobByName(knobs, "CallNoise")
	if noise.Value != "0.6" || len(noise.History) != 2 || noise.History[0].Label() != "0.4 → 0.6" {
		t.Errorf("CallNoise = %+v", noise)
	}
	if len(noise.Edits) != 1 || noise.Edits[0].Branch != "work/ai/weights" || noise.Edits[0].Label() != "0.6 → 0.5" || noise.Edits[0].IssueSlug != "ai/weights" {
		t.Errorf("CallNoise edits = %+v", noise.Edits)
	}
	if len(prep.Edits) != 0 {
		t.Errorf("PrepTime is not edited on the branch: %+v", prep.Edits)
	}
	role := knobByName(knobs, "RoleCapable")
	if len(role.History) != 2 || role.History[0].Label() != "55 → 58" {
		t.Errorf("RoleCapable history (second assignment on a line) = %+v", role.History)
	}
	if loose := knobByName(knobs, "Loose"); loose.Value != "true" || loose.History != nil {
		t.Errorf("Loose = %+v", loose)
	}
	if esc := knobByName(knobs, "Escapes"); esc.File != "" {
		t.Errorf("Escapes kept an outside file: %+v", esc)
	}

	ai := KnobsForSystem(knobs, wb, "ai", "")
	if len(ai) != 2 {
		t.Errorf("AI knobs = %+v", ai)
	}
	if s := knobByName(knobs, "PrepTime").Systems; len(s) != 1 || s[0] != "Sim" {
		t.Errorf("PrepTime systems = %v", s)
	}
}

func TestAssignedValue(t *testing.T) {
	cases := []struct {
		line, name, want string
		ok               bool
	}{
		{"    public const float OpeningPrep = 20;       // stage 1 (was 30)", "OpeningPrep", "20", true},
		{"public const float RoleNatural = 70, RoleCapable = 55, RoleStretch = 40;", "RoleCapable", "55", true},
		{"  max_hp: 100  # per unit", "max_hp", "100", true},
		{"BanNoise = 0.3f", "BanNoise", "0.3", true},
		{"if (OpeningPrep == 20) {", "OpeningPrep", "", false},
		{"// OpeningPrep = 30 was too long", "OpeningPrep", "", false},
		{"MyOpeningPrep = 4", "OpeningPrep", "", false},
		{`Name = "ram"`, "Name", `"ram"`, true},
	}
	for _, c := range cases {
		got, ok := assignedValue([]string{c.line}, c.name)
		if got != c.want || ok != c.ok {
			t.Errorf("%q / %s = %q, %v; want %q, %v", c.line, c.name, got, ok, c.want, c.ok)
		}
	}
}
