package telemetry

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"time"
)

// VersionUse is how a project's telemetry file saw one issue-cli build.
type VersionUse struct {
	// Calls and Last count the events whose ver is exactly the build.
	Calls int
	Last  time.Time
	// Newest is the newest event's build and time, whatever it is: what the
	// project runs right now.
	NewestVer string
	NewestAt  time.Time
}

// ScanVersion reads path (and its rotated path+".1") for events from build
// ver. Only lines carrying `"ver":"<ver>"` are decoded, so a large file
// costs a byte scan. Missing files are not an error.
func ScanVersion(path, ver string) (VersionUse, error) {
	var use VersionUse
	needle := []byte(`"ver":"` + ver + `"`)
	for _, p := range []string{path + ".1", path} {
		if err := scanVersionFile(p, needle, &use); err != nil {
			return use, err
		}
	}
	return use, nil
}

func scanVersionFile(path string, needle []byte, use *VersionUse) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	var last []byte
	for {
		raw, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(raw)) > 0 {
			last = raw
			if bytes.Contains(raw, needle) {
				if _, ts, ok := decodeEvent(raw); ok {
					use.Calls++
					if ts.After(use.Last) {
						use.Last = ts
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	if e, ts, ok := decodeEvent(last); ok && !ts.Before(use.NewestAt) {
		use.NewestVer, use.NewestAt = e.Ver, ts
	}
	return nil
}

func decodeEvent(raw []byte) (Event, time.Time, bool) {
	var e Event
	if len(raw) == 0 || json.Unmarshal(raw, &e) != nil || e.V < 1 || e.V > SchemaVersion {
		return Event{}, time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, e.TS)
	if err != nil {
		return Event{}, time.Time{}, false
	}
	return e, ts, true
}
