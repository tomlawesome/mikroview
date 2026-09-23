// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// clearFactorFixture is an isolated deployment's worth of auth and
// recovery-key storage, on its own temp directory and its own
// MIKROVIEW_CONFIG -- so runClearSecondFactor, driven exactly as the
// operator would drive it, opens the same files this test seeded rather
// than sharing state (or a lock) with any other test.
type clearFactorFixture struct {
	authPath     string
	recoveryPath string
	pepperPath   string
	store        *auth.Store
	// keys is the recovery-key set committed for this fixture. Real
	// deployments generate these with -generate-recovery-keys; seeding
	// them directly here is the same operation without a terminal to
	// drive.
	keys []string
}

func newClearFactorFixture(t *testing.T) *clearFactorFixture {
	t.Helper()
	dir := t.TempDir()
	f := &clearFactorFixture{
		authPath:     filepath.Join(dir, "users.json"),
		recoveryPath: filepath.Join(dir, "recovery-keys.json"),
		pepperPath:   filepath.Join(dir, "recovery-pepper.key"),
	}

	// RecoveryKeysPath has no environment-variable override (only
	// RecoveryPepperPath does, via MIKROVIEW_RECOVERY_PEPPER_FILE), so a
	// minimal YAML config is what points every path at this test's own
	// temp directory instead of the compiled-in default under
	// DefaultDataDir -- which a sandboxed test has no business writing
	// to, and would silently share across every test that didn't
	// override it.
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := fmt.Sprintf("auth:\n  storePath: %q\n  recoveryKeysPath: %q\n  recoveryPepperPath: %q\n",
		f.authPath, f.recoveryPath, f.pepperPath)
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIKROVIEW_CONFIG", cfgPath)
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")

	store, err := auth.Open(f.authPath)
	if err != nil {
		t.Fatalf("auth.Open: %v", err)
	}
	f.store = store

	recovery, err := auth.OpenRecovery(f.recoveryPath, f.pepperPath)
	if err != nil {
		t.Fatalf("auth.OpenRecovery: %v", err)
	}
	keys, err := recovery.Generate()
	if err != nil {
		t.Fatalf("recovery.Generate: %v", err)
	}
	if err := recovery.Commit(); err != nil {
		t.Fatalf("recovery.Commit: %v", err)
	}
	f.keys = keys

	return f
}

// openRecovery re-opens the fixture's recovery-key store from disk, the
// same way a fresh CLI invocation would -- used after running the
// command under test to check what actually got persisted, independent
// of whatever the command's own (already-discarded) *auth.RecoveryStore
// believes happened.
func (f *clearFactorFixture) openRecovery(t *testing.T) *auth.RecoveryStore {
	t.Helper()
	recovery, err := auth.OpenRecovery(f.recoveryPath, f.pepperPath)
	if err != nil {
		t.Fatalf("auth.OpenRecovery: %v", err)
	}
	return recovery
}

// seedUserWithActiveTOTP creates an ordinary account already carrying a
// confirmed authenticator-app factor and its ten recovery codes --
// exactly the state a real account reaches after enrolling
// (SetPendingTOTPSecret then ConfirmTOTP) and confirming
// (GenerateRecoveryCodes), so ClearTOTP has real state to remove.
func seedUserWithActiveTOTP(t *testing.T, store *auth.Store, username string) *auth.User {
	t.Helper()
	now := time.Now()
	u, err := store.CreateUser(username, "correct horse battery staple", auth.RoleUser, now)
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", username, err)
	}
	if err := store.SetPendingTOTPSecret(u.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("SetPendingTOTPSecret: %v", err)
	}
	if err := store.ConfirmTOTP(u.ID, now, 1); err != nil {
		t.Fatalf("ConfirmTOTP: %v", err)
	}
	if _, err := store.GenerateRecoveryCodes(u.ID, now); err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	fresh, ok := store.ByUsername(username)
	if !ok {
		t.Fatalf("seeded user %q vanished", username)
	}
	if !fresh.HasActiveTOTP() || len(fresh.RecoveryCodes) == 0 {
		t.Fatalf("test setup: %q does not have an active factor with recovery codes, got HasActiveTOTP=%t codes=%d",
			username, fresh.HasActiveTOTP(), len(fresh.RecoveryCodes))
	}
	return fresh
}

// withStdin feeds input to readRecoveryKey and confirmSaved, both of
// which read from os.Stdin. A pipe rather than a terminal, same as
// TestReadRecoveryKeyStillReadsFromAPipe in recovery_prompt_test.go --
// readRecoveryKey's non-terminal branch is exercised deliberately, since
// driving the real echo-suppressing prompt needs a pty this package
// already has a separate test for.
func withStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
}

// runClearSecondFactorCapture runs the command and returns its exit code
// together with whatever it printed to stdout -- captureStdout is
// recovery_output_test.go's helper, reused here rather than duplicated.
func runClearSecondFactorCapture(t *testing.T, args []string) (code int, stdout string) {
	t.Helper()
	stdout = captureStdout(t, func() { code = runClearSecondFactor(args) })
	return code, stdout
}

// A wrong key, and no key at all (stdin closed before one is typed),
// must both leave the account and the recovery-key set exactly as they
// were. This is the property the issue asks for by name: the CLI clear
// needs the recovery key, not merely host access.
func TestClearSecondFactorWrongOrAbsentKeyClearsNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stdin string
	}{
		{"wrong key", "WRONGKEYWRONGKEYWRONGKEYWRONGKEY\n"},
		{"no key typed", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newClearFactorFixture(t)
			seedUserWithActiveTOTP(t, f.store, "bilbo")

			withStdin(t, tc.stdin)
			code, out := runClearSecondFactorCapture(t, []string{"bilbo"})
			if code != 1 {
				t.Fatalf("exit code = %d, want 1; output:\n%s", code, out)
			}

			fresh, ok := f.store.ByUsername("bilbo")
			if !ok {
				t.Fatal("the account vanished")
			}
			if !fresh.HasActiveTOTP() {
				t.Error("the factor was cleared despite a wrong or absent recovery key")
			}
			if len(fresh.RecoveryCodes) != 10 {
				t.Errorf("recovery codes changed despite a wrong or absent recovery key: %d remain, want 10", len(fresh.RecoveryCodes))
			}

			// Nothing was consumed either: the original key still
			// redeems. Checked against a fresh RecoveryStore opened from
			// disk, not the command's own (already gone) instance, so
			// this is what actually persisted rather than what the
			// command merely believed.
			if _, err := f.openRecovery(t).Redeem(f.keys[0]); err != nil {
				t.Errorf("the original recovery key no longer redeems after a rejected attempt: %v", err)
			}
		})
	}
}

// A correct key clears the factor and burns its recovery codes in the
// same write (ClearTOTP's own contract), and the recovery key itself is
// spent -- rotated the same way every other gated command rotates on
// success.
func TestClearSecondFactorCorrectKeyClearsFactorAndRecoveryCodes(t *testing.T) {
	f := newClearFactorFixture(t)
	seedUserWithActiveTOTP(t, f.store, "bilbo")

	withStdin(t, f.keys[0]+"\nsaved\n")
	code, out := runClearSecondFactorCapture(t, []string{"bilbo"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "cleared") {
		t.Errorf("output does not report the factor as cleared:\n%s", out)
	}

	fresh, ok := f.store.ByUsername("bilbo")
	if !ok {
		t.Fatal("the account vanished")
	}
	if fresh.HasActiveTOTP() {
		t.Error("the factor is still active after a correctly-keyed clear")
	}
	if len(fresh.RecoveryCodes) != 0 {
		t.Errorf("recovery codes were not cleared: %d remain, want 0", len(fresh.RecoveryCodes))
	}

	// The key that authorised this is spent: it no longer redeems
	// against the store as it now sits on disk.
	if _, err := f.openRecovery(t).Redeem(f.keys[0]); err == nil {
		t.Error("the spent recovery key still redeems after being used")
	}
}

// An unknown username must fail without touching anything -- no
// unrelated account's state changes, and no recovery key is consumed
// looking someone up who was never there. Redeem only prepares a
// rotation; Commit is what actually spends it, and Commit is reached
// only once a real target is found.
func TestClearSecondFactorUnknownUsernameFailsCleanly(t *testing.T) {
	f := newClearFactorFixture(t)
	seedUserWithActiveTOTP(t, f.store, "bilbo")

	withStdin(t, f.keys[0]+"\n")
	code, out := runClearSecondFactorCapture(t, []string{"nobody-by-this-name"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; output:\n%s", code, out)
	}

	fresh, ok := f.store.ByUsername("bilbo")
	if !ok || !fresh.HasActiveTOTP() {
		t.Error("an unrelated account's factor was disturbed by a lookup on an unknown username")
	}

	if _, err := f.openRecovery(t).Redeem(f.keys[0]); err != nil {
		t.Errorf("the recovery key was consumed even though the named account does not exist: %v", err)
	}
}

// A username with no active factor -- never enrolled, or already
// cleared -- is reported honestly rather than as an error the operator
// has to interpret, and the command still runs to completion: the key
// was already proven valid by the time that is known, so the rest of
// the command (printing and confirming the rotated keys) proceeds
// exactly as it would if there had been something to clear.
func TestClearSecondFactorReportsNoFactorHonestly(t *testing.T) {
	f := newClearFactorFixture(t)
	now := time.Now()
	if _, err := f.store.CreateUser("frodo", "correct horse battery staple", auth.RoleUser, now); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	withStdin(t, f.keys[0]+"\nsaved\n")
	code, out := runClearSecondFactorCapture(t, []string{"frodo"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "nothing to clear") {
		t.Errorf("output does not honestly report there was no factor to clear:\n%s", out)
	}

	// Verifying the key is what let this command run at all, so it
	// still rotates -- reaching "nothing to clear" is not the same as
	// never having proven the key.
	if _, err := f.openRecovery(t).Redeem(f.keys[0]); err == nil {
		t.Error("the recovery key still redeems even though verifying it is what let this command run")
	}
}
