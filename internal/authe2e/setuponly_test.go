// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// Build notes B9.43 (ruling 12): the no-key refusal applies to setup-only
// mode too, with the same message; with the key found in the app folder or
// through MIKROVIEW_HISTORY_KEY_FILE, setup-only mode starts and the admin
// can sign in.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const brokenConfig = "listen: [this is not: valid yaml\n  : : :\n"

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	must(t, os.MkdirAll(filepath.Dir(to), 0o700))
	must(t, os.WriteFile(to, b, 0o600))
}

// setupOnlyAdminCanSignIn checks a running setup-only instance: its health
// route says so, the admin signs in with a recovery code, and a route that
// is not part of setup-only mode answers 503.
func setupOnlyAdminCanSignIn(t *testing.T, a *app, f *legacyInstall) {
	t.Helper()
	hz := a.client().get("/api/healthz")
	wantStatus(t, "healthz", hz, 200)
	if !strings.Contains(string(hz.Body), "setup-only") {
		t.Fatalf("healthz does not say setup-only: %s\n%s", hz.Body, a.log())
	}
	w := &world{a: a, admin: f.admin}
	c, r := w.signIn(t, f.admin, f.admin.recovery(t))
	wantStatus(t, "admin sign-in in setup-only mode", r, 200)
	if s := c.session(t); !s.Authenticated || s.Username != f.admin.name {
		t.Errorf("session = %+v, want the admin signed in", s)
	}
	wantStatus(t, "an ordinary route in setup-only mode", c.get("/api/events"), 503)
}

// #1202 B9.43: setup-only mode (a config file that does not parse) with no
// key is refused with the same CFG code and message as B1.3. Paired with
// the key supplied each of the two ways.
func TestSetupOnlyModeRefusesWithNoKeyAndStartsWithOne(t *testing.T) {
	// The reference refusal.
	_, normal, _ := refused(t, Opts{NoKey: true})
	wantCode := cfgCodeRe.FindString(normal)
	if wantCode == "" {
		t.Fatalf("test setup: start-up refusal has no CFG code:\n%s", normal)
	}

	t.Run("no key", func(t *testing.T) {
		t.Parallel()
		f := newLegacyInstallSealed(t)
		code, out, a := refused(t, Opts{Root: f.root, RawConfig: brokenConfig, NoKey: true})
		if code == 0 {
			t.Errorf("exit status 0\n%s", out)
		}
		if got := cfgCodeRe.FindString(out); got != wantCode {
			t.Errorf("CFG code = %q, want the normal start-up refusal's %s:\n%s", got, wantCode, out)
		}
		for _, want := range []string{"history.keyFile", "history.key"} {
			if !strings.Contains(out, want) {
				t.Errorf("the refusal does not mention %q:\n%s", want, out)
			}
		}
		if setupCodeRe.MatchString(out) {
			t.Errorf("a setup code was logged:\n%s", out)
		}
		_ = a
	})

	t.Run("key named by MIKROVIEW_HISTORY_KEY_FILE", func(t *testing.T) {
		t.Parallel()
		f := newLegacyInstallSealed(t)
		a := mustStart(t, Opts{Root: f.root, RawConfig: brokenConfig, KeyFromEnv: true, KeyFile: f.keyPath})
		setupOnlyAdminCanSignIn(t, a, f)
	})

	t.Run("key at keys/history.key in the app folder", func(t *testing.T) {
		t.Parallel()
		f := newLegacyInstallSealed(t)
		folderKey := filepath.Join(f.root, "appfolder", "keys", "history.key")
		copyFile(t, f.keyPath, folderKey)
		a := mustStart(t, Opts{Root: f.root, AppFolder: true, RawConfig: brokenConfig})
		setupOnlyAdminCanSignIn(t, a, f)
	})
}
