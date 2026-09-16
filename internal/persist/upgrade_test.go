// SPDX-License-Identifier: AGPL-3.0-only

// upgrade_test.go is the proof half of #1239
// (docs/decisions/upgrade-framework.md, "Proof is recordings, not
// booted images"): for every recorded release under .upgrade-fixtures/
// (scripts/fetch-upgrade-fixtures.sh), unpack it, open it with the
// current build's own persist path, and check that what
// scripts/record-upgrade-fixture.sh actually wrote is still there --
// against the committed testdata/upgrade/<version>/manifest.json, never
// against the tarball's own claims about itself.
//
// package persist_test, not persist: this needs internal/auth,
// internal/flags, internal/entities, internal/coverage, internal/hosts
// and internal/engine, every one of which already imports
// internal/persist, so importing them from inside package persist would
// be a cycle.
package persist_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/coverage"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/hosts"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// fixtureAdminPassword is the one password every recording uses for
// both its admin and its viewer account -- scripts/record-upgrade-
// fixture.sh's FIXTURE_PASSWORD. An obvious placeholder, not a secret
// (docs/upgrades.md), so it is safe to hold in source rather than in
// the committed manifest.
const fixtureAdminPassword = "fixture-only-not-real"

// upgradeManifest mirrors what scripts/record-upgrade-fixture.sh writes
// to testdata/upgrade/<version>/manifest.json -- names and values only,
// never a hash, token or key. The optional sections are nil for a
// version whose API did not have that feature yet (v0.1.0/v0.2.0 have
// no Watchlist; only v0.5.0/v0.5.1 have Coverage and HostMark).
type upgradeManifest struct {
	Version             string `json:"version"`
	AdminUsername       string `json:"adminUsername"`
	ViewerUsername      string `json:"viewerUsername"`
	HistoryKeyEncrypted bool   `json:"historyKeyEncrypted"`
	Entity              struct {
		Type  string `json:"type"`
		Key   string `json:"key"`
		Label string `json:"label"`
	} `json:"entity"`
	Watchlist *struct {
		Name  string `json:"name"`
		Ports []int  `json:"ports"`
	} `json:"watchlist"`
	Flag struct {
		Type   string `json:"type"`
		Target string `json:"target"`
	} `json:"flag"`
	Coverage *struct {
		Key    string `json:"key"`
		Reason string `json:"reason"`
	} `json:"coverage"`
	HostMark *struct {
		Key    string `json:"key"`
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	} `json:"hostMark"`
}

// repoRoot finds the repository root from this test file's own
// location (internal/persist/upgrade_test.go), two directories up --
// robust against `go test` being invoked from any working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not resolve this test file's own path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// TestUpgradeFixtures opens every recorded, fetched release with the
// current build and checks it against its manifest. It skips -- naming
// scripts/fetch-upgrade-fixtures.sh -- when .upgrade-fixtures/ is absent
// or empty, which is the ordinary state of a local checkout: CI fetches
// first (see .gitlab-ci.yml's test:go job).
//
// The recordings themselves are what it iterates, not the committed
// manifests under testdata/upgrade/. That is the difference #1247 needed:
// a release recorded by the tag job has a recording in the registry
// before anyone has committed its manifest, and iterating testdata would
// silently never test it. loadUpgradeManifest resolves the manifest for
// each -- committed first, the fetched registry copy otherwise.
func TestUpgradeFixtures(t *testing.T) {
	root := repoRoot(t)
	fixtureDir := filepath.Join(root, ".upgrade-fixtures")

	tarballs, err := filepath.Glob(filepath.Join(fixtureDir, "upgrade-fixture-*.tar.gz"))
	if err != nil {
		t.Fatalf("looking for recorded fixtures: %v", err)
	}
	if len(tarballs) == 0 {
		t.Skip("no recorded fixtures found under .upgrade-fixtures/ -- run scripts/fetch-upgrade-fixtures.sh first")
	}

	for _, tarPath := range tarballs {
		base := filepath.Base(tarPath)
		version := strings.TrimSuffix(strings.TrimPrefix(base, "upgrade-fixture-"), ".tar.gz")
		t.Run(version, func(t *testing.T) {
			testOneFixture(t, root, version, tarPath)
		})
	}
}

func testOneFixture(t *testing.T, root, version, tarPath string) {
	t.Helper()
	ctx := context.Background()

	manifest := loadUpgradeManifest(t, root, version)

	unpackDir := t.TempDir()
	extractTarGz(t, tarPath, unpackDir)
	dataDir := filepath.Join(unpackDir, "data")
	if info, err := os.Stat(dataDir); err != nil || !info.IsDir() {
		t.Fatalf("%s did not contain a data/ directory", tarPath)
	}

	var key *retention.Key
	if manifest.HistoryKeyEncrypted {
		loaded, err := retention.LoadKey(filepath.Join(unpackDir, "history.key"))
		if err != nil {
			t.Fatalf("loading history.key: %v", err)
		}
		key = loaded
	}
	backendFor := func(name string, encrypted bool) persist.Backend {
		realPath := filepath.Join(dataDir, name)
		if encrypted {
			// Every recording was made by a real container with no
			// custom store paths configured, so its ciphertext is bound
			// to the file backend's fixed default data directory --
			// never to this test's own t.TempDir(). See
			// NewEncryptedFileBackendForPath's doc comment.
			logicalPath := config.DefaultDataDir + "/" + name
			return persist.NewEncryptedFileBackendForPath(realPath, logicalPath, key)
		}
		return persist.NewFileBackend(realPath)
	}

	// 1. Migrations reach the current schema -- the real path every
	// boot takes (storage.upgradeDataDirSchema in main.go), against
	// data schema-0 recordings actually wrote (these tags predate
	// schema.json entirely, per #1238).
	res, err := persist.MigrateFileSchema(ctx, dataDir, "upgrade_test")
	if err != nil {
		t.Fatalf("MigrateFileSchema: %v", err)
	}
	if res.To != persist.CurrentSchema() {
		t.Errorf("schema after migration = %d, want CurrentSchema() = %d", res.To, persist.CurrentSchema())
	}
	if stored, _, err := persist.ReadFileSchema(dataDir); err != nil {
		t.Errorf("ReadFileSchema after migration: %v", err)
	} else if stored != persist.CurrentSchema() {
		t.Errorf("schema.json records %d, want %d", stored, persist.CurrentSchema())
	}

	// 2. The admin and viewer accounts exist and their passwords verify.
	// #853 rule 6 only exempts accounts/tokens/recovery-keys from going
	// *memory-only* when no history.keyFile is configured at all
	// (storage.backendFor's hashedStores branch); once a key exists,
	// every file-backed store -- these included -- is encrypted under
	// it, hashes or not.
	authStore, err := auth.OpenWithBackend(backendFor("users.json", manifest.HistoryKeyEncrypted))
	if err != nil {
		t.Fatalf("opening the accounts store: %v", err)
	}
	if _, err := authStore.Authenticate(manifest.AdminUsername, fixtureAdminPassword, time.Now()); err != nil {
		t.Errorf("admin login (%s): %v", manifest.AdminUsername, err)
	}
	if _, err := authStore.Authenticate(manifest.ViewerUsername, fixtureAdminPassword, time.Now()); err != nil {
		t.Errorf("viewer login (%s): %v", manifest.ViewerUsername, err)
	}

	// 3. The named entity.
	entityStore, err := entities.OpenWithBackend(backendFor("entities.json", manifest.HistoryKeyEncrypted))
	if err != nil {
		t.Fatalf("opening the entities store: %v", err)
	}
	if !entityHasLabel(entityStore.List(), manifest.Entity.Type, manifest.Entity.Key, manifest.Entity.Label) {
		t.Errorf("entity %+v not found (or label mismatch) in entities.json", manifest.Entity)
	}

	// 4. The flag (present on every version: new_device is deterministic
	// since v0.1.0).
	flagsStore, err := flags.OpenWithBackend(backendFor("flags.json", manifest.HistoryKeyEncrypted))
	if err != nil {
		t.Fatalf("opening the flags store: %v", err)
	}
	defer func() { _ = flagsStore.Close(ctx) }()
	if !hasFlag(flagsStore.List(), manifest.Flag.Type, manifest.Flag.Target) {
		t.Errorf("flag %+v not found in flags.json", manifest.Flag)
	}

	// 5. The watchlist entry, from v0.3.0 on.
	if manifest.Watchlist != nil {
		defsStore, err := engine.OpenDefinitionsStoreWithBackend(backendFor("definitions.json", manifest.HistoryKeyEncrypted))
		if err != nil {
			t.Fatalf("opening the definitions store: %v", err)
		}
		defer func() { _ = defsStore.Close(ctx) }()
		if err := checkWatchlist(defsStore.List(), manifest.Watchlist.Name, manifest.Watchlist.Ports); err != nil {
			t.Error(err)
		}
	}

	// 6. The coverage declaration and the host mark, from v0.5.0 on.
	if manifest.Coverage != nil {
		covStore, err := coverage.OpenWithBackend(backendFor("coverage.json", manifest.HistoryKeyEncrypted))
		if err != nil {
			t.Fatalf("opening the coverage store: %v", err)
		}
		if !hasDeclaration(covStore.List(), manifest.Coverage.Key, manifest.Coverage.Reason) {
			t.Errorf("coverage declaration %+v not found in coverage.json", manifest.Coverage)
		}
	}
	if manifest.HostMark != nil {
		hostRegister, err := hosts.OpenWithBackend(backendFor("hosts.json", manifest.HistoryKeyEncrypted))
		if err != nil {
			t.Fatalf("opening the hosts store: %v", err)
		}
		defer func() { _ = hostRegister.Close(ctx) }()
		h, ok := hostRegister.Get(manifest.HostMark.Key)
		if !ok {
			t.Errorf("host %q not found in hosts.json", manifest.HostMark.Key)
		} else if h.Mark == nil {
			t.Errorf("host %q has no mark", manifest.HostMark.Key)
		} else if string(h.Mark.Kind) != manifest.HostMark.Kind || h.Mark.Reason != manifest.HostMark.Reason {
			t.Errorf("host %q mark = %+v, want kind=%s reason=%s", manifest.HostMark.Key, h.Mark, manifest.HostMark.Kind, manifest.HostMark.Reason)
		}
	}
}

func entityHasLabel(list []entities.Entity, entityType, key, label string) bool {
	for _, e := range list {
		if e.Type == entityType && e.Key == key {
			return e.Label == label
		}
	}
	return false
}

func hasFlag(list []flags.Flag, flagType, target string) bool {
	for _, f := range list {
		if string(f.Type) == flagType && f.Target == target {
			return true
		}
	}
	return false
}

func hasDeclaration(list []coverage.Declaration, key, reason string) bool {
	for _, d := range list {
		if d.Key == key {
			return d.Reason == reason
		}
	}
	return false
}

// checkWatchlist finds the named custom watchlist entry and checks it
// decoded into an *available* definition (see engine.StoredDefinition --
// a decode failure on load surfaces as Available=false rather than an
// error, since the whole point of #404's document shape is that one bad
// entry never blocks the rest of the document) whose Params carry the
// same ports the recording created it with.
func checkWatchlist(list []engine.StoredDefinition, name string, wantPorts []int) error {
	for _, d := range list {
		if d.Definition.Name != name {
			continue
		}
		if !d.Available {
			return fmt.Errorf("watchlist entry %q decoded as unavailable", name)
		}
		got, _ := json.Marshal(d.Definition.Params["ports"])
		want, _ := json.Marshal(wantPorts)
		if string(got) != string(want) {
			return fmt.Errorf("watchlist entry %q ports = %s, want %s", name, got, want)
		}
		return nil
	}
	return fmt.Errorf("watchlist entry %q not found in definitions.json", name)
}

// extractTarGz unpacks src (a gzip'd tar, as scripts/record-upgrade-
// fixture.sh produces) into dir. No symlinks or device files are ever
// packed by that script, so this only handles plain files and
// directories -- anything else is refused rather than silently
// followed.
func extractTarGz(t *testing.T, src, dir string) {
	t.Helper()
	f, err := os.Open(src)
	if err != nil {
		t.Fatalf("opening %s: %v", src, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader for %s: %v", src, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("reading tar entry in %s: %v", src, err)
		}
		// #nosec G305 -- every path in these tarballs is produced by
		// scripts/record-upgrade-fixture.sh from a known, flat layout
		// (data/..., history.key); this test never unpacks an untrusted
		// upload.
		target := filepath.Join(dir, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				t.Fatalf("creating %s: %v", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatalf("creating %s: %v", filepath.Dir(target), err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode&0o777))
			if err != nil {
				t.Fatalf("creating %s: %v", target, err)
			}
			// #nosec G110 -- these tarballs are this project's own
			// recordings (a few tens of KB of JSON/TLS material), not an
			// untrusted upload; no decompression-bomb risk in practice.
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				t.Fatalf("writing %s: %v", target, err)
			}
			out.Close()
		default:
			t.Fatalf("%s: unexpected tar entry type %v for %s", src, hdr.Typeflag, hdr.Name)
		}
	}
}
