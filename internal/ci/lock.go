package ci

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Lock is .ci/lock: the run in progress. One run per project root at a time.
type Lock struct {
	PID     int       `json:"pid"`
	Sha     string    `json:"sha,omitempty"`
	Tier    string    `json:"tier"`
	Started time.Time `json:"started"`
}

// ErrBusy is returned when another live run holds the lock.
var ErrBusy = errors.New("a CI run is already in progress")

// LockPath is .ci/lock.
func LockPath(root string) string { return filepath.Join(Dir(root), "lock") }

// pidAlive reports whether a process exists. A var so tests can fake it.
var pidAlive = func(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// ReadLock returns the lock and whether its process is still alive. A
// missing lock is nil, false.
func ReadLock(root string) (*Lock, bool) {
	data, err := os.ReadFile(LockPath(root))
	if err != nil {
		return nil, false
	}
	var l Lock
	if json.Unmarshal(data, &l) != nil {
		return nil, false
	}
	return &l, pidAlive(l.PID)
}

// acquireLock takes .ci/lock for this process. A lock left by a dead process
// is replaced; a live one returns ErrBusy with the holder.
func acquireLock(root string, l Lock) (*Lock, error) {
	if err := ensureDir(root); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(l)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(LockPath(root), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, werr := f.Write(data)
			cerr := f.Close()
			return nil, errors.Join(werr, cerr)
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		held, alive := ReadLock(root)
		if alive {
			return held, ErrBusy
		}
		// Stale (dead pid, or unreadable): remove and try once more.
		os.Remove(LockPath(root))
	}
	held, _ := ReadLock(root)
	return held, fmt.Errorf("%w (could not replace a stale lock)", ErrBusy)
}

// updateLock records the commit the run is on now.
func updateLock(root string, l Lock) {
	data, _ := json.Marshal(l)
	_ = os.WriteFile(LockPath(root), data, 0o644)
}

// releaseLock removes the lock when this process (pid) still holds it.
func releaseLock(root string, pid int) {
	if l, _ := ReadLock(root); l != nil && l.PID == pid {
		os.Remove(LockPath(root))
	}
}
