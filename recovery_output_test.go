// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected, and returns whatever
// it wrote. This is the channel the defect was in, so the tests measure
// it directly rather than inspecting the code that feeds it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	os.Stdout = old
	w.Close()
	out := <-done
	r.Close()
	return out
}

var testKeys = []string{
	"TESTRECOVERYKEYAAAAAAAAAAAAAAAAA",
	"TESTRECOVERYKEYBBBBBBBBBBBBBBBBB",
	"TESTRECOVERYKEYCCCCCCCCCCCCCCCCC",
}

// The operator has to be able to read the keys, or the command achieves
// nothing. Showing them is not the hazard; which stream they are shown
// on is.
func TestPrintRecoveryKeysShowsEveryKeyExactlyOnce(t *testing.T) {
	out := captureStdout(t, func() { printRecoveryKeys(testKeys) })

	for _, k := range testKeys {
		if n := strings.Count(out, k); n != 1 {
			t.Errorf("key %s appears %d times, want exactly 1:\n%s", k, n, out)
		}
	}
	if !strings.Contains(out, "never again") {
		t.Errorf("nothing tells the operator this is their only chance to capture them:\n%s", out)
	}
}

// A container's stdout is its log. `docker run -t` allocates a pty, so a
// terminal check passes while the log driver still writes every byte to
// disk -- keys printed that way were recovered from the on-disk
// container log and used to take over the admin account. Under
// `docker exec` the same output reaches the operator's terminal and
// nothing else.
//
// PID 1 is what separates the two, so it is the entire control.
func TestKeyPrintingIsRefusedAsTheContainerEntrypoint(t *testing.T) {
	if !isContainerMainProcess(1) {
		t.Error("PID 1 is the container entrypoint; its stdout goes to the container log")
	}
	if isContainerMainProcess(4242) {
		t.Error("an ordinary process was treated as the entrypoint, which would refuse every host install")
	}
	// The test binary is not PID 1, so this is the allowed path.
	if err := refuseIfContainerMainProcess("-generate-recovery-keys"); err != nil {
		t.Errorf("refused an ordinary process: %v", err)
	}
}

// The refusal is the only thing between a recovery key and the container
// log, so it has to leave the operator able to act. Someone told "no"
// without being told what to run instead reaches for whatever works,
// which is the thing that was just refused.
func TestTheRefusalNamesTheCommandToRunInstead(t *testing.T) {
	msg := containerMainProcessRefusal("-generate-recovery-keys").Error()
	for _, want := range []string{
		"docker compose exec",     // what to do
		"docker compose run",      // what they just did
		"container log",           // why
		"-generate-recovery-keys", // which command was refused
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal message is missing %q:\n%s", want, msg)
		}
	}
}

// Every command that prints keys has to refuse before it does anything,
// not at the point of printing. -transfer-admin and
// -recover-admin-account both change state before they have keys to
// show; refusing halfway would leave the operator having completed the
// dangerous half of an operation they were told they could not perform.
func TestEveryKeyPrintingCommandChecksBeforeDoingWork(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)

	for _, fn := range []string{"runGenerateRecoveryKeys", "runTransferAdmin", "runRecoverAdminAccount"} {
		start := strings.Index(body, "func "+fn+"(")
		if start < 0 {
			t.Fatalf("%s not found", fn)
		}
		guard := strings.Index(body[start:], "refuseIfContainerMainProcess")
		if guard < 0 {
			t.Errorf("%s can print recovery keys but never checks whether stdout is the container log", fn)
			continue
		}
		// It must come before the store is opened -- that is the first
		// thing any of these do that matters.
		open := strings.Index(body[start:], "ForCLI(")
		if open >= 0 && guard > open {
			t.Errorf("%s checks only after opening the store; the refusal has to come first", fn)
		}
	}
}

// There must be no file in this path at all. Writing the keys out first
// was tried and was worse: the operator has to read the file to use it,
// so the keys reach a terminal regardless, and the file adds plaintext
// keys on the data volume -- caught by any backup taken during that
// window, and left behind entirely if the process dies before it can
// clean up. It moved the exposure and charged a disk copy for it.
func TestNoKeyMaterialIsWrittenToDisk(t *testing.T) {
	dir := t.TempDir()
	captureStdout(t, func() { printRecoveryKeys(testKeys) })

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("printing recovery keys created %d file(s); it must not touch disk", len(entries))
	}

	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"recovery-keys.txt", "deliverRecoveryKeys", "-show-recovery-keys"} {
		if strings.Contains(string(src), gone) {
			t.Errorf("%q is back in main.go -- the hand-over file was removed on purpose", gone)
		}
	}
}

// -transfer-admin must not name the admin before the recovery key has
// been proven.
//
// Its own comment says so ("the key is asked for BEFORE any account is
// named or listed, so nothing about who holds an account is disclosed to
// someone without one") and the code did the opposite: the username was
// printed above that comment, so anyone able to run the binary learned
// who the admin is by starting the command and pressing Ctrl-C.
//
// Source-level, like TestEveryKeyPrintingCommandChecksBeforeDoingWork
// above, because the property is an ordering within one function and
// driving these commands needs a terminal.
//
// -recover-admin-account is deliberately not held to this: the SSO-only
// check it performs first already has to name the account to explain
// why it cannot help. That difference is stated at the call site.
func TestTransferAdminDoesNotNameTheAdminBeforeTheKey(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)

	start := strings.Index(body, "func runTransferAdmin(")
	if start < 0 {
		t.Fatal("runTransferAdmin not found")
	}
	fn := body[start:]
	if end := strings.Index(fn, "\nfunc "); end > 0 {
		fn = fn[:end]
	}

	key := strings.Index(fn, "readRecoveryKey()")
	if key < 0 {
		t.Fatal("runTransferAdmin never reads a recovery key")
	}
	named := strings.Index(fn, "current.Username")
	if named < 0 {
		t.Fatal("runTransferAdmin never names the current admin -- if that was removed on purpose, this test should go with it")
	}
	if named < key {
		t.Error("runTransferAdmin names the current admin before asking for the recovery key, so anyone able to run the binary learns who the admin is by starting the command and abandoning it")
	}
}
