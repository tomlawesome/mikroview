// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// Build notes B6 (the command-line recovery commands), test 30, and B8.37
// (the same commands with no key mounted).

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var recoveryKeyRe = regexp.MustCompile(`\b[A-Z0-9]{32}\b`)

// recoveryKeys picks the keys a command printed out of its output.
func recoveryKeys(t *testing.T, out string) []string {
	t.Helper()
	keys := recoveryKeyRe.FindAllString(out, -1)
	if len(keys) < 3 {
		t.Fatalf("found %d recovery keys in the output, want 3:\n%s", len(keys), out)
	}
	return keys[len(keys)-3:]
}

// ptyStep says: wait until the output so far has shown want n times, then
// type send (and a newline).
type ptyStep struct {
	want string
	n    int
	send string
}

// ptyRun runs the binary with a real terminal as its stdin and stdout, as
// an operator at a console would: -recover-admin-account reads its
// passwords with echo off, which a pipe cannot drive.
func ptyRun(t *testing.T, a *app, env []string, steps []ptyStep, args ...string) (int, string) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx available: %v", err)
	}
	defer m.Close()
	must(t, unix.IoctlSetPointerInt(int(m.Fd()), unix.TIOCSPTLCK, 0))
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN)
	must(t, err)
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	must(t, err)
	defer slave.Close()

	cmd := exec.Command(buildBinary(t), args...)
	cmd.Dir = a.root
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	must(t, cmd.Start())

	var mu sync.Mutex
	var seen strings.Builder
	go func() {
		buf := make([]byte, 4096)
		for {
			k, err := m.Read(buf)
			if k > 0 {
				mu.Lock()
				seen.Write(buf[:k])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	for _, st := range steps {
		deadline := time.Now().Add(60 * time.Second)
		for {
			mu.Lock()
			count := strings.Count(seen.String(), st.want)
			mu.Unlock()
			if count >= st.n {
				break
			}
			if time.Now().After(deadline) {
				_ = cmd.Process.Kill()
				mu.Lock()
				defer mu.Unlock()
				t.Fatalf("waited for %dx %q, never appeared:\n%s", st.n, st.want, seen.String())
			}
			select {
			case <-done:
				mu.Lock()
				defer mu.Unlock()
				t.Fatalf("the command ended before showing %dx %q:\n%s", st.n, st.want, seen.String())
			default:
			}
			time.Sleep(50 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
		_, _ = m.WriteString(st.send + "\n")
	}
	var code int
	select {
	case err := <-done:
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the command did not finish")
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	return code, seen.String()
}

// cliOn runs a command-line mode against an existing instance's
// configuration, key and data.
func (w *world) cliOn(t *testing.T, stdin string, args ...string) (int, string) {
	t.Helper()
	code, out, _ := cli(t, Opts{Root: w.a.root}, stdin, args...)
	return code, out
}

// #1202 B6.30: -recover-admin-account with a valid recovery key sets a new
// password and the old sessions are dead; -transfer-admin <user> moves the
// admin role; -clear-second-factor <user> removes TOTP, passkeys and
// recovery codes and the account is held at must-enrol-factor next; none of
// them prints a setup code.
func TestRecoveryCommandsWorkOverTheSealedStores(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})

	// Keys first: -generate-recovery-keys on a fresh install.
	code, out := w.cliOn(t, "saved\n", "-generate-recovery-keys")
	if code != 0 {
		t.Fatalf("-generate-recovery-keys exit %d:\n%s", code, out)
	}
	keys := recoveryKeys(t, out)

	// -recover-admin-account: new password, old sessions dead.
	const newPass = "recovered-passphrase-3Wd!"
	a, env := prepare(t, Opts{Root: w.a.root})
	code, out = ptyRun(t, a, env, []ptyStep{
		{"ecovery key", 1, keys[0]},
		{"assword", 1, newPass},
		{"assword", 2, newPass},
		{"never again", 1, "saved"},
	}, "-recover-admin-account")
	if code != 0 {
		t.Fatalf("-recover-admin-account exit %d:\n%s", code, out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("-recover-admin-account printed a setup code:\n%s", out)
	}
	keys = recoveryKeys(t, out)
	if w.c.session(t).Authenticated {
		t.Error("the admin's old session survived a password recovery")
	}
	old := w.a.client().post("/api/auth/login", map[string]string{"username": w.admin.name, "password": w.admin.pass})
	wantProblem(t, "the old password after recovery", old, 401, "invalid-credentials")
	w.admin.pass = newPass
	c, r := w.signIn(t, w.admin, w.admin.recovery(t))
	wantStatus(t, "sign-in with the recovered password", r, 200)
	w.c = c

	// -transfer-admin: the admin role moves.
	heir := w.newUser(t, "heir", "user")
	code, out = w.cliOn(t, keys[0]+"\nsaved\n", "-transfer-admin", "heir")
	if code != 0 {
		t.Fatalf("-transfer-admin exit %d:\n%s", code, out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("-transfer-admin printed a setup code:\n%s", out)
	}
	keys = recoveryKeys(t, out)
	_, r = w.signIn(t, heir, "")
	wantStatus(t, "heir signs in", r, 200)
	var acc struct{ Username, Role string }
	r.json(t, &acc)
	if acc.Role != "admin" {
		t.Errorf("heir's role after the transfer = %q, want admin", acc.Role)
	}
	_, r = w.signIn(t, w.admin, w.admin.recovery(t))
	wantStatus(t, "the former admin signs in", r, 200)
	r.json(t, &acc)
	if acc.Role == "admin" {
		t.Error("the former admin is still an admin after the transfer")
	}

	// -clear-second-factor: TOTP and recovery codes gone, account held.
	code, out = w.cliOn(t, keys[0]+"\nsaved\n", "-clear-second-factor", w.admin.name)
	if code != 0 {
		t.Fatalf("-clear-second-factor exit %d:\n%s", code, out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("-clear-second-factor printed a setup code:\n%s", out)
	}
	c, r = w.signIn(t, w.admin, "")
	wantStatus(t, "sign-in after clearing the second factor (password alone)", r, 200)
	if strings.Contains(string(r.Body), "secondFactor") {
		t.Errorf("the account still asks for a second factor: %s", r.Body)
	}
	wantProblem(t, "route after clearing", c.get("/api/devices"), 403, "must-enrol-factor")
	if s := c.session(t); s.HasTOTP || !s.MustEnrolSecond {
		t.Errorf("session = %+v, want hasTOTP false and mustEnrolSecondFactor true", s)
	}

	// A wrong key changes nothing (paired with every success above).
	code, out = w.cliOn(t, "WRONGKEYWRONGKEYWRONGKEYWRONGKEY\n", "-clear-second-factor", w.admin.name)
	if code == 0 {
		t.Errorf("-clear-second-factor with a wrong key succeeded:\n%s", out)
	}
}

// #1202 B8.37: the four commands that open the store give the same refusal
// as start-up when no key is mounted -- non-zero exit, a CFG code and how to
// mount a key, no setup code, nothing written. With a key they work (the
// success paths are in the test above and TestRecoveryKeysFileIsSealed).
func TestCommandsRefuseWithNoKeyMountedWithTheSameMessageAsStartUp(t *testing.T) {
	t.Parallel()
	_, startOut, _ := refused(t, Opts{NoKey: true})
	startCode := cfgCodeRe.FindString(startOut)
	if startCode == "" {
		t.Fatalf("start-up refusal carries no CFG code:\n%s", startOut)
	}

	for _, args := range [][]string{
		{"-recover-admin-account"},
		{"-transfer-admin", "heir"},
		{"-clear-second-factor", "someone"},
		{"-generate-recovery-keys"},
	} {
		code, out, a := cli(t, Opts{NoKey: true}, "ANYRECOVERYKEYANYRECOVERYKEY1234\nsaved\n", args...)
		if code == 0 {
			t.Errorf("%v with no key exited 0:\n%s", args, out)
		}
		if got := cfgCodeRe.FindString(out); got != startCode {
			t.Errorf("%v: CFG code = %q, want the start-up refusal's %s:\n%s", args, got, startCode, out)
		}
		for _, want := range []string{"history.keyFile", "history.key"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: the refusal does not mention %q:\n%s", args, want, out)
			}
		}
		if setupCodeRe.MatchString(out) {
			t.Errorf("%v: a setup code was printed:\n%s", args, out)
		}
		if files := dataFiles(t, a); len(files) != 0 {
			t.Errorf("%v: wrote to the data directory despite refusing: %v", args, files)
		}
	}
}
