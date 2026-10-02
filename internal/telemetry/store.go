package telemetry

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// FileName is the telemetry log inside a project's .agent-logs dir and
	// inside the global fallback dir.
	FileName = "telemetry.jsonl"
	// MaxFileBytes triggers rotation to FileName+".1" before the next write.
	MaxFileBytes int64 = 10 << 20
	// DisableEnv turns telemetry off when set to off/0/false/no.
	DisableEnv = "ISSUE_CLI_TELEMETRY"
	// SkipEnv, when non-empty, skips recording only the current invocation
	// without counting as an opt-out. The viewer sets it on its own
	// `telemetry report` call so page loads aren't counted as CLI usage.
	SkipEnv = "ISSUE_CLI_TELEMETRY_SKIP"
)

// EnvDisabled reports whether DisableEnv opts out of telemetry.
func EnvDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(DisableEnv))) {
	case "off", "0", "false", "no":
		return true
	}
	return false
}

// ProjectPath is the per-project telemetry file under <workdir>/.agent-logs/.
func ProjectPath(workDir string) string {
	return filepath.Join(workDir, ".agent-logs", FileName)
}

// GlobalPath is the fallback for invocations with no resolvable project:
// $XDG_STATE_HOME/issue-cli/telemetry.jsonl, default ~/.local/state/...
// Returns "" when no home directory can be determined.
func GlobalPath() string {
	base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "issue-cli", FileName)
}

// Sink receives events. FileSink is the only implementation today; the event
// hub idea can add another without touching the CLI.
type Sink interface {
	Write(Event) error
}

// FileSink appends events to a JSONL file with size-based rotation.
type FileSink struct {
	Path     string
	MaxBytes int64 // 0 means MaxFileBytes
}

// Write serializes e and appends it as one line. The stat/rotate/append
// sequence runs under an exclusive flock on Path+".lock" so concurrent
// writers never double-rotate (which would drop a full file of history) and
// each line lands in a single write.
func (s FileSink) Write(e Event) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if len(line)+1 > MaxEventBytes {
		return errEventTooLarge
	}
	line = append(line, '\n')

	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := flockWithTimeout(lock, 500*time.Millisecond); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	max := s.MaxBytes
	if max <= 0 {
		max = MaxFileBytes
	}
	if info, err := os.Stat(s.Path); err == nil && info.Size() >= max {
		if err := os.Rename(s.Path, s.Path+".1"); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(line)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

type telemetryError string

func (e telemetryError) Error() string { return string(e) }

const (
	errEventTooLarge = telemetryError("telemetry event exceeds size cap")
	errLockTimeout   = telemetryError("telemetry lock timeout")
)

// flockWithTimeout polls a non-blocking exclusive lock so a wedged writer can
// never hang the CLI; on timeout the event is dropped.
func flockWithTimeout(f *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != syscall.EWOULDBLOCK {
			return err
		}
		if time.Now().After(deadline) {
			return errLockTimeout
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// ReadFiles streams events from path+".1" then path (oldest first), skipping
// malformed lines and events newer than SchemaVersion. Missing files are not
// an error. visit is called only for events with TS >= since (zero since
// means no filter).
func ReadFiles(path string, since time.Time, visit func(Event)) error {
	for _, p := range []string{path + ".1", path} {
		if err := readFile(p, since, visit); err != nil {
			return err
		}
	}
	return nil
}

func readFile(path string, since time.Time, visit func(Event)) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		raw, err := r.ReadBytes('\n')
		if len(raw) > 0 {
			readLine(raw, since, visit)
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// readLine decodes one JSONL line; malformed or foreign lines are skipped so
// one bad write can never hide the rest of the history.
func readLine(raw []byte, since time.Time, visit func(Event)) {
	var e Event
	if json.Unmarshal(raw, &e) != nil || e.V < 1 || e.V > SchemaVersion {
		return
	}
	if !since.IsZero() {
		ts, err := time.Parse(time.RFC3339Nano, e.TS)
		if err != nil || ts.Before(since) {
			return
		}
	}
	visit(e)
}
