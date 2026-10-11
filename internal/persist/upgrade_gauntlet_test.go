// SPDX-License-Identifier: AGPL-3.0-only

// upgrade_gauntlet_test.go is #1202's migration test, file half (build
// notes section C): data written by earlier releases opens, in place,
// through gauntlet.OpenStore and gauntlet.OpenTokenStore over mikroview's
// own backends under a test key.
//
// It sits beside upgrade_test.go (which still opens the same recordings
// through the old internal/auth store and is replaced when that package
// goes) and uses its helpers.
package persist_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/gauntlet"
	gpersist "github.com/tomlawesome/gauntlet/persist"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// gauntletManifest is upgrade_test.go's manifest plus the two fields
// section C reads. Names only, never a hash or a key.
type gauntletManifest struct {
	AdminUsername        string `json:"adminUsername"`
	ViewerUsername       string `json:"viewerUsername"`
	HistoryKeyEncrypted  bool   `json:"historyKeyEncrypted"`
	APITokenName         string `json:"apiTokenName"`
	SecondFactorEnrolled bool   `json:"secondFactorEnrolled"`
}

func loadGauntletManifest(t *testing.T, root, version string) gauntletManifest {
	t.Helper()
	for _, p := range []string{
		filepath.Join(root, "testdata", "upgrade", version, "manifest.json"),
		filepath.Join(root, ".upgrade-fixtures", version, "manifest.json"),
	} {
		raw, err := os.ReadFile(p) // #nosec G304 -- this repo's own layout
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		var m gauntletManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("parsing %s: %v", p, err)
		}
		return m
	}
	t.Fatalf("no manifest for %s", version)
	return gauntletManifest{}
}

// C steps 1-6 against every recorded release, as TestUpgradeFixtures
// does for the old path. Skipped, naming the fetch script, when no
// recordings are fetched; TestLegacyShapedDocumentsOpenOnGauntlet below
// runs the same checks on a stand-in so the path is exercised anyway.
//
// #1202 C.1-C.6.
func TestUpgradeFixturesOpenOnGauntlet(t *testing.T) {
	root := repoRoot(t)
	tarballs, err := filepath.Glob(filepath.Join(root, ".upgrade-fixtures", "upgrade-fixture-*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tarballs) == 0 {
		t.Skip("no recorded fixtures found under .upgrade-fixtures/ -- run scripts/fetch-upgrade-fixtures.sh first")
	}
	for _, tarPath := range tarballs {
		version := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(tarPath), "upgrade-fixture-"), ".tar.gz")
		t.Run(version, func(t *testing.T) {
			m := loadGauntletManifest(t, root, version)
			unpack := t.TempDir()
			extractTarGz(t, tarPath, unpack)
			dataDir := filepath.Join(unpack, "data")

			// C.1: the schema reaches 4 and schema.json says so.
			res, err := persist.MigrateFileSchema(context.Background(), dataDir, "upgrade_test")
			if err != nil {
				t.Fatalf("MigrateFileSchema: %v", err)
			}
			if res.To != 4 || persist.CurrentSchema() != 4 {
				t.Errorf("schema after migration = %d, CurrentSchema() = %d; both must be 4", res.To, persist.CurrentSchema())
			}
			if stored, _, err := persist.ReadFileSchema(dataDir); err != nil || stored != 4 {
				t.Errorf("schema.json = %d, %v; want 4", stored, err)
			}

			var key *retention.Key
			if m.HistoryKeyEncrypted {
				key, err = retention.LoadKey(filepath.Join(unpack, "history.key"))
				if err != nil {
					t.Fatalf("loading the recorded key: %v", err)
				}
			} else {
				key = freshKey(t)
			}
			backend := func(name string) gpersist.Backend {
				// Recordings were made by a container with the default
				// data directory, which the encrypted files are bound to.
				return persist.NewEncryptedFileBackendForPath(
					filepath.Join(dataDir, name), config.DefaultDataDir+"/"+name, name, key)
			}
			if !m.HistoryKeyEncrypted {
				// #1202 review R26 (ruling 19): a recording made without a
				// key holds plaintext accounts and tokens. They are
				// refused and left as they were -- there is no sealing in
				// place.
				runPlaintextRefusalChecks(t, plaintextCase{
					accounts:    func() gpersist.Backend { return backend("users.json") },
					tokens:      func() gpersist.Backend { return backend("tokens.json") },
					rawAccounts: func() []byte { return readFile(t, filepath.Join(dataDir, "users.json")) },
					rawTokens:   func() []byte { return readFile(t, filepath.Join(dataDir, "tokens.json")) },
				})
				return
			}
			runGauntletChecks(t, gauntletCase{
				accounts:         func() gpersist.Backend { return backend("users.json") },
				tokens:           func() gpersist.Backend { return backend("tokens.json") },
				rawAccounts:      func() []byte { return readFile(t, filepath.Join(dataDir, "users.json")) },
				rawTokens:        func() []byte { return readFile(t, filepath.Join(dataDir, "tokens.json")) },
				admin:            m.AdminUsername,
				viewer:           m.ViewerUsername,
				apiTokenName:     m.APITokenName,
				apiTokenKindless: version == "v0.1.0", // #1202 review R12
				totp:             m.SecondFactorEnrolled,
			})
		})
	}
}

// The same checks on a data directory built in the shape earlier
// releases wrote (unversioned accounts object, bare token list), so the
// path runs without recordings. Two halves (#1202 review R26, ruling 19):
// the documents as a keyless release left them are plaintext and are
// refused; the same documents sealed the way the previous release sealed
// them (mikroview's encrypted file backend) open and pass every check.
// #1202 C.1-C.6, B1.8, B8.35.
func TestLegacyShapedDocumentsOpenOnGauntlet(t *testing.T) {
	for _, withTOTP := range []bool{false, true} {
		name := "no second factor"
		if withTOTP {
			name = "authenticator app enrolled"
		}
		t.Run(name, func(t *testing.T) {
			t.Run("plaintext is refused", func(t *testing.T) {
				l := buildLegacy(t, withTOTP)
				dir := t.TempDir()
				usersPath := filepath.Join(dir, "users.json")
				tokensPath := filepath.Join(dir, "tokens.json")
				writeFile(t, usersPath, l.accountsJSON)
				writeFile(t, tokensPath, l.tokensJSON)
				key := freshKey(t)

				if _, err := persist.MigrateFileSchema(context.Background(), dir, "upgrade_test"); err != nil {
					t.Fatalf("MigrateFileSchema: %v", err)
				}
				backend := func(path string) gpersist.Backend {
					return persist.NewEncryptedFileBackend(path, filepath.Base(path), key)
				}
				runPlaintextRefusalChecks(t, plaintextCase{
					accounts:    func() gpersist.Backend { return backend(usersPath) },
					tokens:      func() gpersist.Backend { return backend(tokensPath) },
					rawAccounts: func() []byte { return readFile(t, usersPath) },
					rawTokens:   func() []byte { return readFile(t, tokensPath) },
				})
				// The documents are byte for byte what the keyless release wrote.
				if got := readFile(t, usersPath); string(got) != string(l.accountsJSON) {
					t.Error("the plaintext accounts document differs from what was written")
				}
				if got := readFile(t, tokensPath); string(got) != string(l.tokensJSON) {
					t.Error("the plaintext tokens document differs from what was written")
				}
			})

			t.Run("sealed by the previous release opens", func(t *testing.T) {
				l := buildLegacy(t, withTOTP)
				dir := t.TempDir()
				usersPath := filepath.Join(dir, "users.json")
				tokensPath := filepath.Join(dir, "tokens.json")
				key := freshKey(t)
				for path, doc := range map[string][]byte{usersPath: l.accountsJSON, tokensPath: l.tokensJSON} {
					if _, err := persist.NewEncryptedFileBackend(path, filepath.Base(path), key).Save(context.Background(), doc, 0); err != nil {
						t.Fatalf("sealing %s as the previous release did: %v", path, err)
					}
				}
				sealedUsers := readFile(t, usersPath)

				res, err := persist.MigrateFileSchema(context.Background(), dir, "upgrade_test")
				if err != nil {
					t.Fatalf("MigrateFileSchema: %v", err)
				}
				if res.To != 4 {
					t.Errorf("schema = %d, want 4", res.To)
				}
				// A stamp, not a conversion: the documents are as they were.
				if got := readFile(t, usersPath); string(got) != string(sealedUsers) {
					t.Error("the schema migration rewrote the accounts document; migration 4 is a stamp with no data change")
				}

				backend := func(path string) gpersist.Backend {
					return persist.NewEncryptedFileBackend(path, filepath.Base(path), key)
				}
				runGauntletChecks(t, gauntletCase{
					accounts:     func() gpersist.Backend { return backend(usersPath) },
					tokens:       func() gpersist.Backend { return backend(tokensPath) },
					rawAccounts:  func() []byte { return readFile(t, usersPath) },
					rawTokens:    func() []byte { return readFile(t, tokensPath) },
					admin:        l.admin,
					viewer:       l.viewer,
					apiTokenName: "upgrade-fixture-token",
					totp:         withTOTP,
				})

				// B1.8: all three raw values (32 hex, no prefix) still
				// authenticate, each on its own kind and no other.
				ts := openTokens(t, backend(tokensPath))
				now := time.Now()
				for _, c := range []struct {
					raw  string
					kind gauntlet.TokenKind
				}{
					{l.apiRaw, gauntlet.TokenKindAPI},
					{l.ingestRaw, gauntlet.TokenKindIngest},
					{l.dropRaw, gauntlet.TokenKind("droplist-pull")},
				} {
					if _, ok := ts.Authenticate(c.raw, c.kind, now); !ok {
						t.Errorf("the legacy %s token (%d hex, no prefix) no longer authenticates", c.kind, len(c.raw))
					}
				}
				if _, ok := ts.Authenticate(l.apiRaw, gauntlet.TokenKindIngest, now); ok {
					t.Error("an api token authenticated as an ingest token")
				}
			})
		})
	}
}

// Wrong key on a sealed legacy install: it fails, never an empty store,
// and gauntlet is not given a way to start over (B8.35).
func TestSealedAccountsUnderAnotherKeyFailToOpenOnGauntlet(t *testing.T) {
	l := buildLegacy(t, false)
	path := filepath.Join(t.TempDir(), "users.json")

	keyA, keyB := freshKey(t), freshKey(t)
	// Sealed the way the previous release sealed it (#1202 review R26: no
	// open seals a plaintext file any more).
	if _, err := persist.NewEncryptedFileBackend(path, "auth", keyA).Save(context.Background(), l.accountsJSON, 0); err != nil {
		t.Fatalf("sealing the legacy accounts: %v", err)
	}
	opts := gauntlet.Options{OnSetupCode: gauntlet.SetupCodeFunc(func(string) {}), ProductName: "MikroView"}
	first, err := gauntlet.OpenStore(persist.NewEncryptedFileBackend(path, "auth", keyA), opts)
	if err != nil {
		t.Fatalf("first open under the right key: %v", err)
	}
	if first.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", first.Count())
	}
	before := readFile(t, path)

	var announced bool
	other := persist.NewEncryptedFileBackend(path, "auth", keyB)
	st, err := gauntlet.OpenStore(other, gauntlet.Options{
		OnSetupCode: gauntlet.SetupCodeFunc(func(string) { announced = true }), ProductName: "MikroView",
	})
	if err == nil {
		t.Fatalf("OpenStore under a different key succeeded with %d accounts", st.Count())
	}
	if announced {
		t.Error("a wrong key announced a setup code, as if the store were empty")
	}
	if got := readFile(t, path); string(got) != string(before) {
		t.Error("a wrong-key open changed the file")
	}
}

// Gauntlet refuses a plaintext backend and mikroview never sets
// AllowPlaintextAtRest (ruling 2): the plain file backend must read as
// plaintext to gauntlet. Paired with the sealed backend opening fine.
func TestGauntletRefusesMikroviewsPlainFileBackendButOpensTheEncryptedOne(t *testing.T) {
	dir := t.TempDir()
	opts := gauntlet.Options{OnSetupCode: gauntlet.SetupCodeFunc(func(string) {}), ProductName: "MikroView"}

	if _, err := gauntlet.OpenStore(persist.NewFileBackend(filepath.Join(dir, "plain.json")), opts); err == nil {
		t.Error("gauntlet.OpenStore accepted mikroview's plain file backend; it must be seen as plaintext at rest")
	}
	if _, err := gauntlet.OpenStore(persist.NewEncryptedFileBackend(filepath.Join(dir, "sealed.json"), "auth", freshKey(t)), opts); err != nil {
		t.Errorf("gauntlet.OpenStore refused mikroview's encrypted file backend: %v", err)
	}
}
