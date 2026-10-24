package ci

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

const ptyHelperEnv = "CI_TEST_PTY_HELPER"

// The detached runner lives in a tmux window, so the tmux pty is its
// controlling terminal. A command that wants the terminal (an interactive
// shell) must not stop on SIGTTOU/SIGTTIN there: CI commands get no
// controlling terminal at all.
func TestRunCommandUnderATerminalGetsNoControllingTerminal(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	pts := openPty(t)
	var out bytes.Buffer
	helper := exec.Command(os.Args[0], "-test.run=^TestRunCommandPtyHelper$", "-test.v")
	helper.Env = append(os.Environ(), ptyHelperEnv+"=1")
	helper.Stdin, helper.Stdout, helper.Stderr = pts, &out, &out
	// The helper is a session leader with the pty as its controlling
	// terminal and itself in the foreground, as the runner is in tmux.
	helper.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- helper.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("helper: %v\n%s", err, out.String())
		}
	case <-time.After(60 * time.Second):
		_ = helper.Process.Kill()
		t.Fatalf("helper hung\n%s", out.String())
	}
}

func TestRunCommandPtyHelper(t *testing.T) {
	if os.Getenv(ptyHelperEnv) == "" {
		t.Skip("runs only as the pty test's helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	// bash -i takes the terminal if it has one; opening /dev/tty only
	// fails when there is no controlling terminal.
	command := `bash --norc -ic true && if (: </dev/tty) 2>/dev/null; then echo "has a controlling terminal"; exit 3; fi`
	exit, timedOut, err := runCommand(ctx, t.TempDir(), command, 0, "", &out)
	if err != nil || timedOut || exit != 0 {
		t.Fatalf("exit %d, timed out %v, err %v\n%s", exit, timedOut, err, out.String())
	}
}

// A timeout kills what the command started, not just sh; so does the
// cleanup after a command that leaves something running.
func TestRunCommandKillsTheWholeTree(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		timeout       time.Duration
	}{
		{"timeout", `sleep 300 & echo $! > "$CI_TEST_PIDFILE"; wait`, time.Second},
		{"left running", `sleep 300 >/dev/null 2>&1 & echo $! > "$CI_TEST_PIDFILE"`, time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pidFile := t.TempDir() + "/pid"
			t.Setenv("CI_TEST_PIDFILE", pidFile)
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()
			var out bytes.Buffer
			if _, _, err := runCommand(ctx, t.TempDir(), tc.command, 0, "", &out); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(pidFile)
			if err != nil {
				t.Fatalf("%v\n%s", err, out.String())
			}
			pid, err := strconv.Atoi(string(bytes.TrimSpace(data)))
			if err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(5 * time.Second); alive(pid); time.Sleep(20 * time.Millisecond) {
				if time.Now().After(deadline) {
					_ = syscall.Kill(pid, syscall.SIGKILL)
					t.Fatalf("background sleep %d still running", pid)
				}
			}
		})
	}
}

// alive is false once pid is gone or a zombie waiting for its reaper.
func alive(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	_, rest, _ := bytes.Cut(stat, []byte(") "))
	return len(rest) > 0 && rest[0] != 'Z'
}

// openPty opens a new pty pair and returns the slave end; both close with
// the test.
func openPty(t *testing.T) *os.File {
	t.Helper()
	ptm, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	t.Cleanup(func() { ptm.Close() })
	var unlock int32
	if err := ioctl(ptm, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		t.Fatal(err)
	}
	var n uint32
	if err := ioctl(ptm, syscall.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		t.Fatal(err)
	}
	pts, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pts.Close() })
	return pts
}

func ioctl(f *os.File, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(arg)); errno != 0 {
		return fmt.Errorf("ioctl %#x: %w", req, errno)
	}
	return nil
}
