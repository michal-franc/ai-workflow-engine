package telemetry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testEvent(cmd string, ts time.Time) Event {
	return Event{V: SchemaVersion, TS: ts.UTC().Format(time.RFC3339Nano), Cmd: cmd, Caller: CallerScript, ArgsHash: HashArgs([]string{cmd})}
}

func readAll(t *testing.T, path string, since time.Time) []Event {
	t.Helper()
	var out []Event
	if err := ReadFiles(path, since, func(e Event) { out = append(out, e) }); err != nil {
		t.Fatalf("ReadFiles: %v", err)
	}
	return out
}

func countValidLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("corrupt line in %s: %q (%v)", path, sc.Text(), err)
		}
		n++
	}
	return n
}

func TestFileSinkWriteAndReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", FileName)
	now := time.Now()
	for _, c := range []string{"list", "show", "transition"} {
		if err := (FileSink{Path: path}).Write(testEvent(c, now)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	got := readAll(t, path, time.Time{})
	if len(got) != 3 || got[0].Cmd != "list" || got[2].Cmd != "transition" {
		t.Fatalf("unexpected events: %+v", got)
	}
}

func TestFileSinkRotatesAtCapAndReaderReadsBoth(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	sink := FileSink{Path: path, MaxBytes: 300}
	now := time.Now()
	for i := 0; i < 5; i++ {
		if err := sink.Write(testEvent(fmt.Sprintf("c%d", i), now)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotated file: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Size() >= 300 {
		t.Fatalf("current file should be below cap after rotation, got %d", info.Size())
	}
	got := readAll(t, path, time.Time{})
	// .1 is read first so order is oldest → newest across the rotation.
	if got[len(got)-1].Cmd != "c4" {
		t.Fatalf("expected newest event last, got %+v", got)
	}
}

func TestFileSinkRotationReplacesPreviousBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path+".1", []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 100)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (FileSink{Path: path, MaxBytes: 50}).Write(testEvent("list", time.Now())); err != nil {
		t.Fatal(err)
	}
	backup, _ := os.ReadFile(path + ".1")
	if strings.Contains(string(backup), "old") {
		t.Fatalf("previous .1 should be replaced, got %q", backup)
	}
	if n := countValidLines(t, path); n != 1 {
		t.Fatalf("expected fresh file with 1 event, got %d", n)
	}
}

// Concurrent writers must never interleave lines, and the flock must stop two
// writers from both rotating (the second rename would drop the first .1).
func TestFileSinkConcurrentWritersNoCorruptionNoDoubleRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	probe, _ := json.Marshal(testEvent("concurrent", time.Now()))
	lineLen := int64(len(probe) + 1)
	const writers, perWriter = 16, 25
	total := writers * perWriter
	// Cap at ~60% of the total so exactly one rotation is needed.
	sink := FileSink{Path: path, MaxBytes: lineLen * int64(total) * 6 / 10}

	var wg sync.WaitGroup
	errs := make(chan error, total)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if err := sink.Write(testEvent("concurrent", time.Now())); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("write error: %v", err)
	}
	got := countValidLines(t, path) + countValidLines(t, path+".1")
	if got != total {
		t.Fatalf("expected %d events across current+.1 (single rotation), got %d", total, got)
	}
}

func TestFileSinkRejectsOversizedEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	e := testEvent("x", time.Now())
	e.Unknown = strings.Repeat("a", MaxEventBytes)
	if err := (FileSink{Path: path}).Write(e); err == nil {
		t.Fatal("expected size-cap error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("oversized event must not be written")
	}
}

func TestReadFilesSkipsMalformedFutureAndOldEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	now := time.Now()
	old, _ := json.Marshal(testEvent("old", now.Add(-48*time.Hour)))
	fresh, _ := json.Marshal(testEvent("fresh", now))
	future := testEvent("future", now)
	future.V = SchemaVersion + 1
	fut, _ := json.Marshal(future)
	content := strings.Join([]string{string(old), "{not json", string(fut), `{"cmd":"noversion"}`, strings.Repeat("z", 200000), string(fresh)}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readAll(t, path, now.Add(-time.Hour))
	if len(got) != 1 || got[0].Cmd != "fresh" {
		t.Fatalf("expected only the fresh event, got %+v", got)
	}
	if all := readAll(t, path, time.Time{}); len(all) != 2 {
		t.Fatalf("zero since should return both valid v1 events, got %d", len(all))
	}
}

func TestReadFilesMissingFileIsNotAnError(t *testing.T) {
	if got := readAll(t, filepath.Join(t.TempDir(), FileName), time.Time{}); len(got) != 0 {
		t.Fatalf("expected no events, got %d", len(got))
	}
}

func TestEnvDisabled(t *testing.T) {
	for _, v := range []string{"off", "0", "false", "NO", " Off "} {
		t.Setenv(DisableEnv, v)
		if !EnvDisabled() {
			t.Errorf("%q should disable telemetry", v)
		}
	}
	for _, v := range []string{"", "on", "1", "yes"} {
		t.Setenv(DisableEnv, v)
		if EnvDisabled() {
			t.Errorf("%q should not disable telemetry", v)
		}
	}
}

func TestGlobalPathHonoursXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	if got := GlobalPath(); got != "/xdg/state/issue-cli/telemetry.jsonl" {
		t.Fatalf("got %s", got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/someone")
	if got := GlobalPath(); got != "/home/someone/.local/state/issue-cli/telemetry.jsonl" {
		t.Fatalf("got %s", got)
	}
}

func TestProjectPath(t *testing.T) {
	if got := ProjectPath("/work/proj"); got != "/work/proj/.agent-logs/telemetry.jsonl" {
		t.Fatalf("got %s", got)
	}
}

func TestTruncateIsRuneSafe(t *testing.T) {
	if got := Truncate("abc", 5); got != "abc" {
		t.Fatalf("got %q", got)
	}
	got := Truncate("aé", 2) // é is 2 bytes; cutting at 2 would split it
	if got != "a" {
		t.Fatalf("got %q", got)
	}
}

func TestHashArgsStableAndDistinct(t *testing.T) {
	a := HashArgs([]string{"comment", "x", "--text", "hi"})
	if a != HashArgs([]string{"comment", "x", "--text", "hi"}) {
		t.Fatal("hash not stable")
	}
	if a == HashArgs([]string{"comment", "x", "--text", "ho"}) {
		t.Fatal("different args must hash differently")
	}
	if len(a) != 16 {
		t.Fatalf("expected 16 hex chars, got %q", a)
	}
}
