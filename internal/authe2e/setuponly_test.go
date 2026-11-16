// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// Build notes B9.43 (ruling 12): the no-key refusal applies to setup-only
// mode too, with the same message; with the key found in the app folder or
// through MIKROVIEW_HISTORY_KEY_FILE, setup-only mode starts and the admin
// can sign in.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// invalidConfig parses, but config validation refuses it (CFG-0002: the
// backup drop box address is not host:port), so mikroview starts setup-only
// mode. Since #1347 setup-only mode refuses a wholly unreadable file, because
// ui.allow cannot then be honoured, so the file must be readable.
const invalidConfig = "backup:\n  listen: not-a-listen-address\n"

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

// #1202 B9.43: setup-only mode (a config file that parses but fails validation) with no
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
		code, out, a := refused(t, Opts{Root: f.root, RawConfig: invalidConfig, NoKey: true})
		if code == 0 {
			t.Errorf("exit status 0\n%s", out)
		}
		// The invalid config is itself reported first (CFG-0002), as it is
		// at a normal start; the no-key refusal is the one setup-only mode
		// adds after it, so that is the one whose code is compared.
		const marker = "refusing to start the config editor"
		i := strings.Index(out, marker)
		if i < 0 {
			t.Fatalf("setup-only mode never reached its no-key refusal (%q missing):\n%s", marker, out)
		}
		if got := cfgCodeRe.FindString(out[i:]); got != wantCode {
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
		a := mustStart(t, Opts{Root: f.root, RawConfig: invalidConfig, KeyFromEnv: true, KeyFile: f.keyPath})
		setupOnlyAdminCanSignIn(t, a, f)
	})

	t.Run("key at keys/history.key in the app folder", func(t *testing.T) {
		t.Parallel()
		f := newLegacyInstallSealed(t)
		folderKey := filepath.Join(f.root, "appfolder", "keys", "history.key")
		copyFile(t, f.keyPath, folderKey)
		a := mustStart(t, Opts{Root: f.root, AppFolder: true, RawConfig: invalidConfig})
		setupOnlyAdminCanSignIn(t, a, f)
	})
}

// PROP-R1: setup-only mode (a config v0.7.0 refuses) opens the accounts store
// but never commits a start, so it leaves a file still sealed under its
// pre-#1450 path binding exactly as the earlier release wrote it: putting the
// previous release back with the previous config still works.
func TestSetupOnlyModeLeavesOldFormatFilesAsTheyWere(t *testing.T) {
	ri := newRichInstall(t)
	key, err := retention.LoadKey(ri.w.a.key)
	must(t, err)

	old := map[string][]byte{}
	for _, name := range ri.files {
		path := ri.w.a.file(name)
		sealed, err := key.Seal(retention.StateStoreKeyInfo, []byte(path), ri.plain[name])
		must(t, err)
		must(t, os.WriteFile(path, sealed, 0o600))
		old[name] = sealed
	}

	a := mustStart(t, Opts{Root: ri.w.a.root, RawConfig: invalidConfig, KeyFromEnv: true, KeyFile: ri.w.a.key})
	hz := a.client().get("/api/healthz")
	wantStatus(t, "healthz", hz, 200)
	if !strings.Contains(string(hz.Body), "setup-only") {
		t.Fatalf("healthz does not say setup-only: %s\n%s", hz.Body, a.log())
	}
	w := &world{a: a, admin: ri.w.admin}
	_, r := w.signIn(t, ri.w.admin, ri.w.admin.recovery(t))
	wantStatus(t, "admin sign-in in setup-only mode", r, 200)
	a.stop()

	// The admin's sign-in may save a store under its old binding, so bytes
	// are not compared for those; the file must still be bound to its path
	// (a copy at another path is refused), which is what the previous
	// release needs. Files never saved are byte for byte as they were.
	for _, name := range ri.files {
		path := ri.w.a.file(name)
		other := filepath.Join(t.TempDir(), filepath.Base(path))
		must(t, os.WriteFile(other, readBytes(t, path), 0o600))
		if _, err := persist.NewEncryptedFileBackend(other, storeNameFor(name), key).Load(context.Background()); err == nil {
			t.Errorf("setup-only mode re-sealed %s under its store name; it must stay bound as the earlier release wrote it", name)
		}
	}
	if got := readBytes(t, ri.w.a.file("tokens.json")); !bytes.Equal(got, old["tokens.json"]) {
		t.Error("setup-only mode rewrote tokens.json")
	}
}
