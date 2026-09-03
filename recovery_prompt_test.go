// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openPTY returns the two ends of a fresh pseudo-terminal. The slave is
// what a program sees as its controlling terminal; the master is what a
// terminal emulator would render, so anything the kernel echoes back
// shows up there. That is exactly the channel this test is about.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()

	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx available: %v", err)
	}
	if err := unix.IoctlSetPointerInt(int(m.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		m.Close()
		t.Fatalf("unlocking the pty: %v", err)
	}
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN)
	if err != nil {
		m.Close()
		t.Fatalf("resolving the pty number: %v", err)
	}
	s, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		t.Fatalf("opening the pty slave: %v", err)
	}
	t.Cleanup(func() {
		s.Close()
		m.Close()
	})
	return m, s
}

// echoEnabled reports whether the terminal is still echoing typed input
// back to the screen.
func echoEnabled(t *testing.T, f *os.File) bool {
	t.Helper()
	tio, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatalf("reading termios: %v", err)
	}
	return tio.Lflag&unix.ECHO != 0
}

// A recovery key is the credential that can hand the admin role to any
// account on the system, and it is typed by a human at a terminal --
// often a shared screen, a recorded incident call, or a session that
// scrolls back. Passwords have always been read with echo suppressed
// here; recovery keys were not, and the key appeared verbatim in the
// scrollback of every transfer.
//
// This test drives the real prompt over a real pty and reads back what
// the terminal would have displayed. It fails if the key is visible.
func TestReadRecoveryKeyIsNotEchoedToTheTerminal(t *testing.T) {
	const key = "TESTRECOVERYKEYPROMPTAAAAAAAAAAA"

	master, slave := openPTY(t)

	// The prompt goes to stdout and the key is read from stdin; on a
	// real terminal both are the same device, so point both at the pty.
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = slave, slave
	t.Cleanup(func() { os.Stdin, os.Stdout = oldIn, oldOut })

	var mu sync.Mutex
	var seen strings.Builder
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				mu.Lock()
				seen.Write(buf[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	type result struct {
		key string
		err error
	}
	done := make(chan result, 1)
	go func() {
		got, err := readRecoveryKey()
		done <- result{got, err}
	}()

	// Type only once the prompt has suppressed echo -- which is what a
	// human does, since they wait to see the prompt. If suppression
	// never happens we still type, so the regression is measured rather
	// than merely timing out.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && echoEnabled(t, slave) {
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := master.WriteString(key + "\n"); err != nil {
		t.Fatalf("typing the key: %v", err)
	}

	var got result
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("readRecoveryKey never returned")
	}
	if got.err != nil {
		t.Fatalf("readRecoveryKey: %v", got.err)
	}
	if got.key != key {
		t.Fatalf("readRecoveryKey returned %q, want %q", got.key, key)
	}

	// Give the line discipline a moment to flush anything it was going
	// to echo, so a pass means "not echoed" rather than "not yet".
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	transcript := seen.String()
	mu.Unlock()

	if !strings.Contains(transcript, "Recovery key:") {
		t.Errorf("the prompt never reached the terminal; transcript: %q", transcript)
	}
	if strings.Contains(transcript, key) {
		t.Errorf("the recovery key was echoed to the terminal and would sit in the operator's scrollback:\n%q", transcript)
	}
}

// The non-terminal path exists so piped automation keeps working. A key
// already sitting in a pipe is the caller's exposure, not ours, but the
// path still has to actually read the key.
func TestReadRecoveryKeyStillReadsFromAPipe(t *testing.T) {
	const key = "TESTRECOVERYKEYPROMPTBBBBBBBBBBB"

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	oldIn := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldIn })

	go func() {
		fmt.Fprintln(w, key)
		w.Close()
	}()

	got, err := readRecoveryKey()
	if err != nil {
		t.Fatalf("readRecoveryKey: %v", err)
	}
	if got != key {
		t.Fatalf("readRecoveryKey returned %q, want %q", got, key)
	}
}

// Echo suppression is a property of the one helper. It stops being a
// property of the program the moment a second prompt is hand-rolled
// somewhere else, which is precisely how this defect arrived the first
// time -- the password prompts were correct and the key prompts were
// written separately.
func TestOnlyOneRecoveryKeyPromptExists(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), `"Recovery key: "`); n != 1 {
		t.Errorf("found %d recovery-key prompts in main.go, want exactly 1 (readRecoveryKey); "+
			"a second prompt is a second chance to forget echo suppression -- call readRecoveryKey instead", n)
	}
}
