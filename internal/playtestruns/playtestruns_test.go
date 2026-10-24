package playtestruns

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeRun makes runsDir/<session>/<name>/ with a results.yaml for issue and
// one PNG shot per viewport, w×h pixels.
func writeRun(t *testing.T, runsDir, session, name, issue string, w, h int, viewports ...string) string {
	t.Helper()
	dir := filepath.Join(runsDir, session, name)
	if err := os.MkdirAll(filepath.Join(dir, "shots"), 0o755); err != nil {
		t.Fatal(err)
	}
	mode := modeFromName(name)
	var vps strings.Builder
	for _, vp := range viewports {
		shot := "TP1-" + vp + ".png"
		writePNG(t, filepath.Join(dir, "shots", shot), w, h)
		vps.WriteString("        - viewport: " + vp + "\n          status: pass\n          screenshot: " + filepath.Join(dir, "shots", shot) + "\n")
	}
	y := "session: " + session + "\nissue: " + issue + "\nmode: " + mode + "\nbuild: abc\nstarted: \"2026-10-23T14:03:55+02:00\"\nsteps:\n    - id: TP1\n      title: One\n      status: pass\n      custom_field: keep me\n"
	if vps.Len() > 0 {
		y += "      viewports:\n" + vps.String()
	}
	if err := os.WriteFile(filepath.Join(dir, "results.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestArchiveKeepsLatestOwnRuns(t *testing.T) {
	root := t.TempDir()
	slug := "ui/evidence"
	wt := RunsDir(WorktreeDir(root, slug))
	oldAuto := writeRun(t, wt, "evidence-tab", "20261023-100000.000-auto", slug, 4, 4, "1440x900")
	newAuto := writeRun(t, wt, "evidence-tab", "20261023-120000.000-auto", slug, 4, 4, "1440x900", "390x844")
	judge := writeRun(t, wt, "evidence-tab", "20261023-110000.000-judge", slug, 4, 4)
	other := writeRun(t, wt, "status-page", "20261023-120500.000-auto", "ui/status", 4, 4, "1440x900")
	aborted := filepath.Join(wt, "canvas-basics", "20261023-120600.000-auto")
	os.MkdirAll(aborted, 0o755)
	bin := filepath.Join(wt, ".bin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "issue-viewer"), make([]byte, 2048), 0o755)

	rep, err := Archive(root, slug)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Moved != 2 || rep.Deleted != 3 || rep.Kept != 0 || rep.CacheBytes != 2048 {
		t.Fatalf("report = %+v, want 2 moved, 3 deleted, the 2 KB binary cache", rep)
	}
	if exists(bin) {
		t.Error("the runner's binary cache was left in a done issue's worktree")
	}
	if len(rep.Sessions) != 1 || rep.Sessions[0] != "evidence-tab" {
		t.Errorf("sessions = %v", rep.Sessions)
	}
	for _, gone := range []string{oldAuto, newAuto, judge, other, aborted} {
		if exists(gone) {
			t.Errorf("%s is still in the worktree", gone)
		}
	}
	for _, name := range []string{"20261023-120000.000-auto", "20261023-110000.000-judge"} {
		if !exists(filepath.Join(RunsDir(root), "evidence-tab", name, "results.yaml")) {
			t.Errorf("%s was not moved to the root", name)
		}
	}
	if exists(filepath.Join(wt, "status-page")) {
		t.Error("an empty session folder was left behind")
	}

	// A second archive finds nothing; a run already in the root stays put.
	writeRun(t, wt, "evidence-tab", "20261023-120000.000-auto", slug, 4, 4)
	rep, err = Archive(root, slug)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Kept != 1 || rep.Moved != 0 {
		t.Errorf("second archive = %+v, want the existing root run kept", rep)
	}
}

func TestPruneOpenIssueDeletesOnlyOtherIssuesRuns(t *testing.T) {
	root := t.TempDir()
	slug := "ui/evidence"
	wt := RunsDir(WorktreeDir(root, slug))
	own1 := writeRun(t, wt, "evidence-tab", "20261023-100000.000-auto", slug, 4, 4, "1440x900")
	own2 := writeRun(t, wt, "evidence-tab", "20261023-120000.000-auto", slug, 4, 4, "1440x900")
	other := writeRun(t, wt, "status-page", "20261023-120500.000-auto", "ui/status", 4, 4, "1440x900")
	bin := filepath.Join(wt, ".bin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "issue-cli"), make([]byte, 2048), 0o755)
	defer func() {
		if !exists(bin) {
			t.Error("an open issue lost the runner's binary cache")
		}
	}()

	dry, err := Prune(root, slug, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Kept != 2 || dry.Deleted != 1 || dry.DeletedBytes == 0 {
		t.Fatalf("dry run = %+v", dry)
	}
	if !exists(other) {
		t.Fatal("a dry run deleted a run")
	}
	if _, err := Prune(root, slug, false, false); err != nil {
		t.Fatal(err)
	}
	if exists(other) || !exists(own1) || !exists(own2) {
		t.Errorf("open issue prune: other=%v own1=%v own2=%v", exists(other), exists(own1), exists(own2))
	}
	if exists(filepath.Join(RunsDir(root), "evidence-tab")) {
		t.Error("an open issue's runs were moved")
	}
}

func TestPruneDryRunForDoneIssueTouchesNothing(t *testing.T) {
	root := t.TempDir()
	slug := "ui/evidence"
	wt := RunsDir(WorktreeDir(root, slug))
	run := writeRun(t, wt, "evidence-tab", "20261023-100000.000-auto", slug, 4, 4, "1440x900")
	rep, err := Prune(root, slug, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Moved != 1 || rep.MovedBytes == 0 || !exists(run) {
		t.Errorf("dry run = %+v, run still there: %v", rep, exists(run))
	}
}

func TestPruneWithoutRunsIsEmpty(t *testing.T) {
	rep, err := Prune(t.TempDir(), "ui/none", true, false)
	if err != nil || rep.Moved+rep.Deleted+rep.Kept != 0 {
		t.Errorf("rep = %+v, err = %v", rep, err)
	}
}

func TestShrinkReducesOldDoneIssues(t *testing.T) {
	root := t.TempDir()
	runs := RunsDir(root)
	old := writeRun(t, runs, "evidence-tab", "20261023-120000.000-auto", "ui/old", 1440, 3000, "1440x900")
	phone := writeRun(t, runs, "evidence-tab-phone", "20261023-120000.000-auto", "ui/old", 390, 5000, "390x844")
	recent := writeRun(t, runs, "status-page", "20261023-120500.000-auto", "ui/recent", 64, 64, "1440x900")
	open := writeRun(t, runs, "canvas", "20261023-120500.000-auto", "ui/open", 64, 64, "1440x900")
	now := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	doneAt := map[string]time.Time{
		"ui/old":    now.AddDate(0, 0, -40),
		"ui/recent": now.AddDate(0, 0, -5),
	}
	doneSince := func(s string) (time.Time, bool) { t, ok := doneAt[s]; return t, ok }

	rep, err := Shrink(root, doneSince, 30*24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Issues != 1 || rep.BytesBefore == 0 || rep.BytesAfter == 0 {
		t.Fatalf("report = %+v", rep)
	}

	jpg := filepath.Join(old, "shots", "TP1-1440x900.jpg")
	if exists(filepath.Join(old, "shots", "TP1-1440x900.png")) || !exists(jpg) {
		t.Fatal("the desktop shot was not converted")
	}
	if w, h := jpegSize(t, jpg); w != 720 || h != 1350 {
		t.Errorf("desktop shot is %dx%d, want 720x1350 (half width, 3 × 450)", w, h)
	}
	if w, h := jpegSize(t, filepath.Join(phone, "shots", "TP1-390x844.jpg")); w != 390 || h != 3*844 {
		t.Errorf("phone shot is %dx%d, want 390x%d (not scaled, cut to 3 screens)", w, h, 3*844)
	}

	b, _ := os.ReadFile(filepath.Join(old, "results.yaml"))
	y := string(b)
	for _, want := range []string{`reduced: "2026-12-01"`, "screenshot: shots/TP1-1440x900.jpg", "custom_field: keep me"} {
		if !strings.Contains(y, want) {
			t.Errorf("results.yaml lacks %q:\n%s", want, y)
		}
	}
	for _, untouched := range []string{recent, open} {
		if !exists(filepath.Join(untouched, "shots", "TP1-1440x900.png")) {
			t.Errorf("%s was shrunk", untouched)
		}
	}

	// A reduced run is left alone the next day.
	rep, err = Shrink(root, doneSince, 30*24*time.Hour, now.AddDate(0, 0, 1))
	if err != nil || rep.Issues != 0 {
		t.Errorf("second shrink = %+v, %v", rep, err)
	}
	if runs, _ := List(RunsDir(root)); runs[0].Reduced == "" && runs[1].Reduced == "" {
		t.Error("List doesn't read reduced:")
	}
}

func jpegSize(t *testing.T, p string) (int, int) {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := jpeg.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}

func TestMB(t *testing.T) {
	for b, want := range map[int64]string{0: "0.0 MB", 3_984_588: "3.8 MB", 115_343_360: "110 MB"} {
		if got := MB(b); got != want {
			t.Errorf("MB(%d) = %q, want %q", b, got, want)
		}
	}
}

// writeGameRun makes a run the way a game's runner writes it (RogueTech):
// mode ux-test or playtest, started: without an offset, each step's shots
// beside results.yaml (screenshots:). steps is the YAML after "steps:";
// empty writes a header-only run. shots are PNGs to put beside it.
func writeGameRun(t *testing.T, runsDir, session, name, issue, mode, steps string, shots ...string) string {
	t.Helper()
	dir := filepath.Join(runsDir, session, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range shots {
		writePNG(t, filepath.Join(dir, s), 8, 6)
	}
	y := "session: " + session + "\nissue: " + issue + "\nmode: " + mode + "\nbuild: b5de0554\nseed: 13\nstarted: 2026-10-24T11:05:08\n"
	if steps != "" {
		y += "steps:\n" + steps
	}
	if err := os.WriteFile(filepath.Join(dir, "results.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const gameStep = "- id: MV7\n  title: The overheated Sniper holds fire\n  status: pass\n  screenshots:\n  - MV7@1920x1080.png\n"

func TestModeAndModeFromName(t *testing.T) {
	for in, want := range map[string]string{"ux-test": "auto", "playtest": "judge", "auto": "auto", "judge": "judge", "replay": "replay", "": ""} {
		if got := Mode(in); got != want {
			t.Errorf("Mode(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"20261023-100000.000-auto":       "auto",
		"20261023-100000.000-judge":      "judge",
		"20261024-110508-ux-test":        "auto",
		"20261024-104708-ux-test-cached": "auto",
		"20261024-133651-playtest":       "judge",
	} {
		if got := modeFromName(in); got != want {
			t.Errorf("modeFromName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A game project's runs (RogueTech): modes ux-test / playtest, shots beside
// results.yaml, a -cached run that points at the run holding its shots, a
// header-only playtest the owner abandoned. Done keeps the latest of each
// session and mode, moves them to the root and deletes only what a newer
// kept run replaces.
func TestArchiveGameRuns(t *testing.T) {
	root := t.TempDir()
	slug := "api/battles"
	wt := RunsDir(WorktreeDir(root, slug))
	cachedStep := gameStep + "  cached: .playtest/runs/battle-ai-heat/20261024-110508-ux-test\n"
	old := writeGameRun(t, wt, "battle-ai-heat", "20261024-090000-ux-test", slug, "ux-test", gameStep, "MV7@1920x1080.png")
	shotRun := writeGameRun(t, wt, "battle-ai-heat", "20261024-110508-ux-test", slug, "ux-test", gameStep, "MV7@1920x1080.png")
	cached := writeGameRun(t, wt, "battle-ai-heat", "20261024-120000-ux-test-cached", slug, "ux-test", cachedStep)
	abandoned := writeGameRun(t, wt, "battle-ai-heat", "20261024-133313-playtest", slug, "playtest", "")
	judged := writeGameRun(t, wt, "battle-ai-heat", "20261024-133651-playtest", slug, "playtest", "- id: MV7\n  status: pass\n  verdict: pass\n")
	ammo := writeGameRun(t, wt, "battle-ammo", "20261024-100000-ux-test", slug, "ux-test", gameStep, "MV7@1920x1080.png")
	crashed := writeGameRun(t, wt, "battle-ammo", "20261024-140000-ux-test", slug, "ux-test", "")
	replay := writeGameRun(t, wt, "battle-ammo", "20261024-150000-replay", slug, "replay", gameStep)
	other := writeGameRun(t, wt, "enemy-3d", "20261021-160928-ux-test", "combat/glitchers", "ux-test", gameStep, "MV2@1920x1080.png")
	broken := filepath.Join(wt, "battle-ammo", "20261024-160000-ux-test")
	os.MkdirAll(broken, 0o755)
	os.WriteFile(filepath.Join(broken, "results.yaml"), []byte("session: [battle-ammo\nsteps: {"), 0o644)
	writePNG(t, filepath.Join(broken, "MV7@1920x1080.png"), 4, 4)
	aborted := filepath.Join(wt, "battle-ammo", "20261024-170000-ux-test")
	os.MkdirAll(aborted, 0o755)

	rep, err := Archive(root, slug)
	if err != nil {
		t.Fatal(err)
	}
	// Own: 8 runs name the issue (the broken one can't be read: not counted).
	// Archived: the shot run (its -cached run points at it), the -cached
	// run, the judged playtest, ammo's latest and the newer crashed run (no
	// newer run replaces it), the replay (a mode it doesn't know).
	if rep.Own != 8 || rep.Archived != 6 {
		t.Errorf("own %d, archived %d; want 8 own, 6 archived", rep.Own, rep.Archived)
	}
	if rep.Moved != 7 || rep.Deleted != 4 {
		t.Errorf("report = %+v; want 7 moved (6 own and the unreadable run), 4 deleted (older ux-test, abandoned playtest, other issue's run, empty folder)", rep)
	}
	for _, kept := range []string{shotRun, cached, judged, ammo, crashed, replay, broken} {
		rel, _ := filepath.Rel(wt, kept)
		if !exists(filepath.Join(RunsDir(root), rel)) {
			t.Errorf("%s was not moved to the root", rel)
		}
	}
	if !exists(filepath.Join(RunsDir(root), "battle-ai-heat", "20261024-110508-ux-test", "MV7@1920x1080.png")) {
		t.Error("the shot beside results.yaml did not move with its run")
	}
	for _, gone := range []string{old, abandoned, other, aborted} {
		rel, _ := filepath.Rel(wt, gone)
		if exists(gone) || exists(filepath.Join(RunsDir(root), rel)) {
			t.Errorf("%s was kept", rel)
		}
	}
	if want := []string{"battle-ai-heat", "battle-ammo"}; strings.Join(rep.Sessions, ",") != strings.Join(want, ",") {
		t.Errorf("sessions = %v, want %v", rep.Sessions, want)
	}
}

// An open game issue keeps all its own runs; only other issues' runs go.
func TestPruneOpenGameIssueKeepsOwnRuns(t *testing.T) {
	root := t.TempDir()
	slug := "api/battles"
	wt := RunsDir(WorktreeDir(root, slug))
	a := writeGameRun(t, wt, "battle-ai-heat", "20261024-090000-ux-test", slug, "ux-test", gameStep, "MV7@1920x1080.png")
	b := writeGameRun(t, wt, "battle-ai-heat", "20261024-110508-ux-test", slug, "ux-test", gameStep, "MV7@1920x1080.png")
	other := writeGameRun(t, wt, "enemy-3d", "20261021-160928-ux-test", "combat/glitchers", "ux-test", gameStep)
	rep, err := Prune(root, slug, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Kept != 2 || rep.Deleted != 1 || rep.Own != 2 {
		t.Errorf("report = %+v", rep)
	}
	if !exists(a) || !exists(b) || exists(other) {
		t.Error("an open issue lost its own runs or kept another issue's")
	}
}

func TestShrinkGameShotsBesideResults(t *testing.T) {
	root := t.TempDir()
	run := writeGameRun(t, RunsDir(root), "battle-ai-heat", "20261024-110508-ux-test", "api/battles", "ux-test", gameStep, "MV7@1920x1080.png")
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	done := func(string) (time.Time, bool) { return now.AddDate(0, 0, -40), true }
	rep, err := Shrink(root, done, 30*24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Issues != 1 || rep.BytesBefore == 0 {
		t.Fatalf("report = %+v, want the game's run shrunk", rep)
	}
	if exists(filepath.Join(run, "MV7@1920x1080.png")) || !exists(filepath.Join(run, "MV7@1920x1080.jpg")) {
		t.Error("the shot beside results.yaml was not turned into a JPEG in place")
	}
	b, _ := os.ReadFile(filepath.Join(run, "results.yaml"))
	if !strings.Contains(string(b), "- MV7@1920x1080.jpg") || !strings.Contains(string(b), "reduced:") {
		t.Errorf("results.yaml not rewritten:\n%s", b)
	}
}
