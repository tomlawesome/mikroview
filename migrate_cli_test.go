// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
)

// seedDataDir builds a populated data directory of the shape a real
// deployment has: a few JSON stores, the append-only match log, the TLS
// store as a directory rather than a document, the recovery pepper, and
// the Postgres adoption marker.
//
// The last three are the point of most of this file -- they are exactly
// what `-backup` leaves out, so a migration that quietly behaved like a
// backup would still pass a test that only looked at users.json.
func seedDataDir(t *testing.T) (string, config.Config) {
	t.Helper()
	dir := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("users.json", `{"users":[{"name":"admin"}]}`)
	write("tokens.json", `{"tokens":[]}`)
	write("matchlog.jsonl", "{\"seq\":1}\n{\"seq\":2}\n")
	// Placeholder text, deliberately not PEM-shaped: this exercises the
	// TLS store as a *directory of files*, and nothing here needs to be
	// real key material.
	write("tls/ca.pem", "placeholder certificate authority, not real material\n")
	write("tls/issued.pem", "placeholder issued certificate, not real material\n")
	write("recovery-pepper.key", "placeholder pepper, not real material")
	write(postgresAdoptedMarker, "This deployment stores its state in Postgres.\n")

	var cfg config.Config
	cfg.Auth.StorePath = filepath.Join(dir, "users.json")
	cfg.Auth.TokensStorePath = filepath.Join(dir, "tokens.json")
	cfg.Auth.RecoveryPepperPath = filepath.Join(dir, "recovery-pepper.key")
	cfg.Watchlist.MatchLogPath = filepath.Join(dir, "matchlog.jsonl")
	cfg.TLS.StorePath = filepath.Join(dir, "tls")
	return dir, cfg
}

// migrate runs the whole operation the way runMigrateData does, minus
// config.Load and the logging, so a test can assert on the plan.
func migrate(t *testing.T, cfg config.Config, dest string, force bool) (*migrationPlan, map[string]string) {
	t.Helper()
	plan, err := planMigration(cfg, dest, force)
	if err != nil {
		t.Fatalf("planMigration: %v", err)
	}
	digests, err := copyTree(plan.Source, plan.Dest)
	if err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	if err := verifyMigration(plan, digests); err != nil {
		t.Fatalf("verifyMigration: %v", err)
	}
	return plan, digests
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestMigrationCarriesWhatBackupDeliberatelyLeavesBehind is #537's
// central Done-when: "preserves every store including the TLS
// certificate authority".
//
// It is written as a contrast rather than as a checklist, because the
// realistic way to get this wrong is to reuse backedUpStores and inherit
// its three deliberate exclusions -- which are correct for a backup
// travelling to a different host and wrong for a move on the same one.
// So the test first pins that those three really are absent from the
// backup list, then requires the migration to carry them anyway.
func TestMigrationCarriesWhatBackupDeliberatelyLeavesBehind(t *testing.T) {
	src, cfg := seedDataDir(t)
	dest := filepath.Join(t.TempDir(), "new-data")

	backedUp := map[string]bool{}
	for _, s := range backedUpStores(cfg) {
		backedUp[s.Name] = true
	}
	for _, name := range []string{"tls", "recovery_pepper", "postgres_adopted_marker"} {
		if backedUp[name] {
			t.Fatalf("test premise broken: %q is in backedUpStores now, so this test no longer "+
				"demonstrates the migration carrying something the backup leaves out", name)
		}
	}

	plan, digests := migrate(t, cfg, dest, false)

	migrated := map[string]bool{}
	for _, s := range plan.Stores {
		migrated[s.Name] = true
	}
	for _, name := range []string{"tls", "recovery_pepper", "postgres_adopted_marker", "auth", "match_log"} {
		if !migrated[name] {
			t.Errorf("the %s store was not part of the move -- the operator is told the move is "+
				"complete while that store stays behind", name)
		}
	}

	// The CA in particular, byte for byte: #537 exists because losing it
	// means every browser and every router has to re-trust.
	for _, rel := range []string{
		"users.json", "tokens.json", "matchlog.jsonl",
		"tls/ca.pem", "tls/issued.pem", "recovery-pepper.key", postgresAdoptedMarker,
	} {
		want := mustRead(t, filepath.Join(src, rel))
		got, err := os.ReadFile(filepath.Join(dest, rel))
		if err != nil {
			t.Errorf("%s did not arrive at the destination: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s differs at the destination", rel)
		}
	}

	if len(digests) != 7 {
		t.Errorf("hashed %d file(s), want 7 -- the copy is not covering the whole tree", len(digests))
	}

	// A copy, never a move: the operator has to be able to fall back.
	if _, err := os.Stat(filepath.Join(src, "users.json")); err != nil {
		t.Errorf("the source was modified: %v. Nothing here may delete the operator's only copy", err)
	}
}

// TestMigrationSetsRestrictivePermissionsRatherThanCopyingThem pins that
// a source loosened by hand -- a bind mount someone chmod'd until it
// worked -- does not carry that decision onto a fresh volume.
func TestMigrationSetsRestrictivePermissionsRatherThanCopyingThem(t *testing.T) {
	src, cfg := seedDataDir(t)
	if err := os.Chmod(filepath.Join(src, "users.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "new-data")
	migrate(t, cfg, dest, false)

	fi, err := os.Stat(filepath.Join(dest, "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("the migrated accounts file is mode %04o, want 0600 -- a world-readable accounts "+
			"file is what the move was supposed to leave behind, not reproduce", got)
	}
}

// TestVerificationCatchesADestinationThatDoesNotMatch is the "verified
// afterwards rather than assumed" half of #537.
//
// Each case corrupts the destination *after* a successful copy, which is
// the only way to distinguish a verification pass that reads the
// destination from one that restates what the copy already believed.
func TestVerificationCatchesADestinationThatDoesNotMatch(t *testing.T) {
	cases := []struct {
		name    string
		damage  func(t *testing.T, dest string)
		wantIn  string
		explain string
	}{
		{
			name: "a file that changed under us",
			damage: func(t *testing.T, dest string) {
				if err := os.WriteFile(filepath.Join(dest, "tls", "ca.pem"), []byte("different"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantIn:  "does not match the source",
			explain: "a destination filesystem that accepted bytes it did not keep",
		},
		{
			name: "a file that never landed",
			damage: func(t *testing.T, dest string) {
				if err := os.Remove(filepath.Join(dest, "recovery-pepper.key")); err != nil {
					t.Fatal(err)
				}
			},
			wantIn:  "not on the destination now",
			explain: "a store silently missing from the copy -- #372's failure, moved",
		},
		{
			name: "something the move never wrote",
			damage: func(t *testing.T, dest string) {
				if err := os.WriteFile(filepath.Join(dest, "users.json.old"), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantIn:  "was not part of this move",
			explain: "a stale file from an older copy outliving the one that replaced it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cfg := seedDataDir(t)
			dest := filepath.Join(t.TempDir(), "new-data")
			plan, digests := migrate(t, cfg, dest, false)

			tc.damage(t, dest)

			err := verifyMigration(plan, digests)
			if err == nil {
				t.Fatalf("verification passed on a destination with %s -- it is not reading the "+
					"destination at all", tc.explain)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("verification failed but not usefully: %v (wanted %q)", err, tc.wantIn)
			}
		})
	}
}

// TestMigrationRefusalsHappenBeforeAnythingIsCopied covers every way the
// move declines, and in each case requires the destination to be
// untouched -- a half-finished copy an operator might switch onto is a
// worse outcome than a refusal.
func TestMigrationRefusalsHappenBeforeAnythingIsCopied(t *testing.T) {
	t.Run("the destination is already the data directory", func(t *testing.T) {
		src, cfg := seedDataDir(t)
		_, err := planMigration(cfg, src, false)
		if err == nil || !strings.Contains(err.Error(), "nothing to move") {
			t.Fatalf("planMigration onto the source itself = %v, want a refusal", err)
		}
	})

	t.Run("the destination is inside the source", func(t *testing.T) {
		src, cfg := seedDataDir(t)
		_, err := planMigration(cfg, filepath.Join(src, "nested"), false)
		if err == nil || !strings.Contains(err.Error(), "does not terminate") {
			t.Fatalf("planMigration into a subdirectory of the source = %v, want a refusal", err)
		}
	})

	t.Run("the source is inside the destination", func(t *testing.T) {
		src, cfg := seedDataDir(t)
		_, err := planMigration(cfg, filepath.Dir(src), false)
		if err == nil || !strings.Contains(err.Error(), "overwrite the data being read") {
			t.Fatalf("planMigration onto a parent of the source = %v, want a refusal", err)
		}
	})

	t.Run("the source does not exist", func(t *testing.T) {
		var cfg config.Config
		cfg.Auth.StorePath = filepath.Join(t.TempDir(), "gone", "users.json")
		_, err := planMigration(cfg, filepath.Join(t.TempDir(), "new"), false)
		if err == nil || !strings.Contains(err.Error(), "cannot be read") {
			t.Fatalf("planMigration from a missing data directory = %v, want a refusal", err)
		}
	})

	t.Run("the source holds nothing", func(t *testing.T) {
		empty := t.TempDir()
		var cfg config.Config
		cfg.Auth.StorePath = filepath.Join(empty, "users.json")
		_, err := planMigration(cfg, filepath.Join(t.TempDir(), "new"), false)
		if err == nil || !strings.Contains(err.Error(), "nothing to move") {
			t.Fatalf("planMigration from an empty data directory = %v, want a refusal", err)
		}
	})

	t.Run("the destination already holds something", func(t *testing.T) {
		_, cfg := seedDataDir(t)
		dest := t.TempDir()
		stale := filepath.Join(dest, "users.json")
		if err := os.WriteFile(stale, []byte(`{"users":[{"name":"someone-else"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := planMigration(cfg, dest, false)
		if err == nil || !strings.Contains(err.Error(), "not empty") {
			t.Fatalf("planMigration into a populated destination = %v, want a refusal: merging two "+
				"deployments leaves a stale accounts file outliving the move", err)
		}
		if got := mustRead(t, stale); !strings.Contains(got, "someone-else") {
			t.Error("the refused migration wrote to the destination anyway")
		}

		// --force is the operator saying they know what is there.
		if _, err := planMigration(cfg, dest, true); err != nil {
			t.Fatalf("planMigration --force into a populated destination: %v", err)
		}
	})

	t.Run("the data directory holds a symlink", func(t *testing.T) {
		src, cfg := seedDataDir(t)
		if err := os.Symlink("/etc/passwd", filepath.Join(src, "elsewhere.json")); err != nil {
			t.Skipf("symlinks unavailable here: %v", err)
		}
		dest := filepath.Join(t.TempDir(), "new-data")

		_, err := planMigration(cfg, dest, false)
		if err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("planMigration over a symlink = %v, want a refusal naming it: following it "+
				"pulls in unrelated data, recreating it dangles once the old mount goes", err)
		}
		entries, readErr := os.ReadDir(dest)
		if readErr == nil && len(entries) > 0 {
			t.Errorf("the refused migration copied %d entr(y/ies) to the destination first", len(entries))
		}
	})
}

// TestMigrationRefusesAnUnwritableDestination is the failure #536's
// preflight was built for, met at the other end: a root-owned host
// directory bind-mounted in, which mikroview at uid 65532 cannot write.
//
// Caught up front and reported with the ids and the chown, rather than
// as a permission error partway through the accounts file.
func TestMigrationRefusesAnUnwritableDestination(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which can write to a mode-0500 directory -- the refusal cannot be observed")
	}
	_, cfg := seedDataDir(t)
	dest := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(dest, 0o500); err != nil {
		t.Fatal(err)
	}

	_, err := planMigration(cfg, dest, false)
	if err == nil {
		t.Fatal("planMigration accepted a destination it cannot write to")
	}
	var unusable *storeUnusable
	if !errors.As(err, &unusable) {
		t.Fatalf("the refusal is %T, not *storeUnusable, so the operator gets no ownership advice: %v", err, err)
	}
	advice := strings.Join(migrationFailureAdvice(unusable), "\n")
	for _, want := range []string{"nothing has been copied", "running as uid", "chown", dest} {
		if !strings.Contains(advice, want) {
			t.Errorf("the advice does not mention %q, so the operator cannot act on it:\n%s", want, advice)
		}
	}
}

// TestMigrationNamesStoresItIsNotMoving covers the store configured onto
// a different mount -- the GeoIP database in the shipped compose file is
// mounted under /etc/mikroview, not in the data directory.
//
// Silently not moving it would look identical to moving it, right up
// until the new deployment starts without it.
func TestMigrationNamesStoresItIsNotMoving(t *testing.T) {
	_, cfg := seedDataDir(t)
	elsewhere := t.TempDir()
	cfg.GeoIP.DBPath = filepath.Join(elsewhere, "GeoLite2-Country.mmdb")
	if err := os.WriteFile(cfg.GeoIP.DBPath, []byte("placeholder database"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, _ := migrate(t, cfg, filepath.Join(t.TempDir(), "new-data"), false)

	var outside []string
	for _, s := range plan.Outside {
		outside = append(outside, s.Name)
	}
	if len(outside) != 1 || outside[0] != "geoip_db" {
		t.Fatalf("stores outside the data directory reported as %v, want [geoip_db]", outside)
	}
	for _, s := range plan.Stores {
		if s.Name == "geoip_db" {
			t.Error("geoip_db was reported as moved, but it is on a different mount and was not")
		}
	}
}

// TestRunMigrateDataEndToEnd drives the command itself, through
// config.Load and the argument handling, rather than the pieces.
func TestRunMigrateDataEndToEnd(t *testing.T) {
	src, _ := seedDataDir(t)
	dest := filepath.Join(t.TempDir(), "new-data")

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", filepath.Join(src, "users.json"))
	t.Setenv("MIKROVIEW_TLS_STORE_PATH", filepath.Join(src, "tls"))

	if code := runMigrateData(nil); code != 2 {
		t.Errorf("runMigrateData with no destination = %d, want 2 (usage)", code)
	}
	if code := runMigrateData([]string{dest}); code != 0 {
		t.Fatalf("runMigrateData(%s) = %d, want 0", dest, code)
	}
	for _, rel := range []string{"users.json", "tls/ca.pem", "recovery-pepper.key", postgresAdoptedMarker} {
		if mustRead(t, filepath.Join(dest, rel)) != mustRead(t, filepath.Join(src, rel)) {
			t.Errorf("%s differs after the command ran", rel)
		}
	}

	// Run again onto the same destination: it is populated now, so the
	// command has to decline rather than half-overwrite it.
	if code := runMigrateData([]string{dest}); code != 1 {
		t.Errorf("a second runMigrateData onto a populated destination = %d, want 1", code)
	}
}

// TestDockerfileShipsTheMigrationMountPoint guards the half of #537 that
// no Go test can otherwise reach.
//
// Docker seeds a fresh named volume from whatever the image has at the
// mount point, ownership included. A volume mounted somewhere the image
// never created therefore arrives root-owned, and mikroview at uid 65532
// cannot write to it -- which kills the bind-mount-to-volume direction
// outright. Observed, not reasoned about: `docker run --user 65532 -v
// newvol:/mnt/x busybox touch /mnt/x/probe` gives "Permission denied"
// against a 0755 root:root directory, and the same run against a path
// the image created with --chown=65532:65532 succeeds.
//
// So the image has to ship /var/lib/mikroview-migrate, and deleting
// those two Dockerfile lines as unused would break the feature while
// every test here still passed. This is what stops that.
func TestDockerfileShipsTheMigrationMountPoint(t *testing.T) {
	dockerfile, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"mkdir -p " + migrationMountPoint,
		"--chown=65532:65532 " + migrationMountPoint + " " + migrationMountPoint,
	} {
		if !strings.Contains(string(dockerfile), want) {
			t.Errorf("Dockerfile is missing %q. Without it a fresh named volume mounted there is "+
				"root-owned, and -migrate-data cannot write to it -- the bind-mount-to-volume "+
				"direction of #537 stops working, silently, for everyone", want)
		}
	}

	docs, err := os.ReadFile(filepath.Join("docs", "configuration.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(docs), migrationMountPoint) {
		t.Errorf("docs/configuration.md never mentions %s, so operators will pick their own mount "+
			"point and hit the root-owned-volume failure this directory exists to avoid",
			migrationMountPoint)
	}
}

// TestUnderDirDoesNotConfuseASiblingForAChild guards the string-prefix
// trap: /var/lib/mikroview-old starts with /var/lib/mikroview, and
// treating it as nested would refuse the single most likely destination
// an operator picks.
func TestUnderDirDoesNotConfuseASiblingForAChild(t *testing.T) {
	cases := []struct {
		dir, path string
		want      bool
	}{
		{"/var/lib/mikroview", "/var/lib/mikroview", true},
		{"/var/lib/mikroview", "/var/lib/mikroview/tls/ca.pem", true},
		{"/var/lib/mikroview", "/var/lib/mikroview-old", false},
		{"/var/lib/mikroview", "/var/lib", false},
		{"/var/lib/mikroview", "/mnt/new-data", false},
	}
	for _, tc := range cases {
		if got := underDir(tc.dir, tc.path); got != tc.want {
			t.Errorf("underDir(%q, %q) = %v, want %v", tc.dir, tc.path, got, tc.want)
		}
	}
}
