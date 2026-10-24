package telemetry

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	line := func(ts, ver string) string {
		return `{"v":1,"ts":"` + ts + `","cmd":"show","exit":0,"caller":"human","pid":1,"ppid":1,"args_hash":"x","ver":"` + ver + `"}` + "\n"
	}
	// Rotated file: older calls on rc.1.
	os.WriteFile(path+".1", []byte(line("2026-10-24T08:00:00Z", "v0.54.0-rc.1")), 0o644)
	os.WriteFile(path, []byte(
		line("2026-10-24T09:00:00Z", "v0.54.0-rc.1")+
			`not json`+"\n"+
			line("2026-10-24T10:00:00Z", "v0.54.0-rc.1-2-gabc")+
			line("2026-10-24T11:00:00Z", "v0.54.0-rc.1")+
			line("2026-10-24T12:00:00Z", "v0.54.0-rc.2")), 0o644)

	use, err := ScanVersion(path, "v0.54.0-rc.1")
	if err != nil {
		t.Fatal(err)
	}
	if use.Calls != 3 {
		t.Errorf("calls = %d, want 3 (exact ver only, both files)", use.Calls)
	}
	if want := time.Date(2026, 10, 24, 11, 0, 0, 0, time.UTC); !use.Last.Equal(want) {
		t.Errorf("last = %v, want %v", use.Last, want)
	}
	if use.NewestVer != "v0.54.0-rc.2" {
		t.Errorf("newest = %q, want the last event's build", use.NewestVer)
	}

	none, err := ScanVersion(filepath.Join(dir, "missing.jsonl"), "v0.54.0-rc.1")
	if err != nil || none.Calls != 0 || none.NewestVer != "" {
		t.Fatalf("missing file = %+v, %v", none, err)
	}
}
