// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/logging"
)

// -migrate-data moves a deployment's data directory from wherever it is
// now to somewhere else -- in practice, between a bind mount and a named
// Docker volume, in either direction (#537).
//
// The move is a copy, and the source is never deleted. An operator runs
// this with both mounts attached, checks the summary, switches the
// compose file over, and deletes the old one when they are satisfied.
// Deleting here would make the failure mode "the data is gone and the
// new mount turned out to be wrong", which is the one outcome worth
// spending a duplicated directory to avoid. persist.AdoptFile made the
// same call for the same reason.
//
// Why this is a mikroview command rather than a documented `cp -a`:
//
//   - Ownership. The destination has to end up owned by the uid
//     mikroview runs as (65532 in the shipped image), and that is the
//     step operators get wrong -- a root-owned copy looks fine until the
//     next start refuses with #536's preflight. Copying from inside the
//     mikroview container means every file is created *by* that uid, so
//     the ownership is right by construction rather than by a chown
//     someone has to remember.
//   - The TLS store. `-backup` leaves it out on purpose (#372: generated
//     key material, wrong to restore blindly onto a different host), but
//     this is the same host keeping the same identity, which is exactly
//     the case where carrying the CA over is right. Not carrying it means
//     every browser and every router has to re-trust.
//   - The recovery pepper, likewise excluded from `-backup` so a stolen
//     backup cannot verify the digests it carries. Leaving it behind
//     would invalidate every recovery key silently.
//   - The Postgres adoption marker. A deployment that dropped it on the
//     way across would come back up on the JSON files and present a
//     first-run setup screen to whoever reached it first (storage.go's
//     refuseIfPostgresAdopted is what stops that, and it can only stop it
//     if the marker travelled).
//   - Verification. A copy that silently missed a store is worth less
//     than no copy at all, because it is believed. Every file is hashed
//     on the way out and re-hashed off the destination afterwards.

// migrationMountPoint is where the shipped image expects the *new*
// location to be mounted while a move runs.
//
// It is a fixed path rather than the operator's choice because Docker
// seeds a fresh named volume from whatever the image has at the mount
// point, ownership included: a volume mounted at a path the image never
// created arrives owned by root, and mikroview at uid 65532 cannot write
// to it. The Dockerfile creates this one owned by 65532 so that a brand
// new volume is writable the moment it appears, which is what makes the
// bind-mount-to-volume direction work at all.
//
// Nothing enforces it -- any destination is accepted, and a bind mount
// the operator has already chowned works anywhere. It is what the usage
// line and the documentation recommend, and what
// TestDockerfileShipsTheMigrationMountPoint keeps the image providing.
const migrationMountPoint = "/var/lib/mikroview-migrate"

// migratedStore is one named thing the move reports on and verifies.
//
// Dir distinguishes the TLS store, which is a directory of generated key
// material rather than a single JSON document like every other entry.
type migratedStore struct {
	Name string
	Path string
	Dir  bool
}

// migratedStores is everything the move carries, by name.
//
// It is deliberately a superset of backedUpStores: a backup travels to a
// different host and a migration does not, and that difference is the
// entire reason three things `-backup` excludes are carried here. Built
// on top of backedUpStores rather than repeating it, so the shared part
// cannot drift between the two lists.
//
// The copy itself does not consult this list -- it copies the whole
// source tree, so nothing under the data directory can be missed by an
// omission here. What the list is for is naming: the summary reports
// stores rather than paths, the verification pass names the store that
// failed, and any configured store sitting outside the directory being
// moved is called out as not moved rather than quietly left behind.
//
// TestMigrationCoversAllConfigPathFields (migrate_coverage_test.go) is
// the drift guard, the same one TestBackupCoversAllConfigPathFields is
// for backedUpStores -- #372 was a hand-maintained list falling three
// fields behind config.Config with no error and no warning, and a second
// hand-maintained list of the same shape earns the same protection.
func migratedStores(cfg config.Config) []migratedStore {
	backed := backedUpStores(cfg)
	out := make([]migratedStore, 0, len(backed)+4)
	for _, s := range backed {
		out = append(out, migratedStore{Name: s.Name, Path: s.Path})
	}
	return append(out,
		// A directory, not a document -- see migratedStore.Dir.
		migratedStore{Name: "tls", Path: cfg.TLS.StorePath, Dir: true},
		migratedStore{Name: "recovery_pepper", Path: cfg.Auth.RecoveryPepperPath},
		migratedStore{Name: "geoip_db", Path: cfg.GeoIP.DBPath},
		// Not a config field, so no reflection walk would ever find it;
		// named here because losing it is the worst outcome of the whole
		// operation. See this file's header comment.
		migratedStore{Name: "postgres_adopted_marker", Path: markerPath(cfg)},
	)
}

// excludedFromMigration is every *Path or *File field on config.Config
// that migratedStores deliberately does not carry, keyed by its dotted
// field path, with the reason.
//
// All three name files the operator supplies and mounts themselves,
// rather than state mikroview writes into its data directory. Moving the
// data directory does not move them and must not claim to.
var excludedFromMigration = map[string]string{
	"TLS.CertFile": "an operator-supplied certificate, mounted read-only from wherever they keep it " +
		"(deploy/docker-compose.yml mounts such files under /etc/mikroview). mikroview never writes it, " +
		"so it is not part of the data directory and does not move with it. The TLS store mikroview " +
		"does generate -- tls.storePath -- is carried.",
	"TLS.KeyFile": "the private key paired with TLS.CertFile, and the same reasoning: supplied and " +
		"mounted by the operator, not written by mikroview.",
	"Postgres.DSNFile": "a mounted secret carrying a database password. It is deliberately not in the " +
		"data directory (storage.go's readDSNFile explains why it is a file at all), and copying a " +
		"credential into a freshly created volume during a migration would spread it, not move it.",
}

// migrationPlan is everything the move needs, resolved and checked
// before a single byte is written.
type migrationPlan struct {
	Source  string
	Dest    string
	Stores  []migratedStore
	Outside []migratedStore // configured, but not under Source: not moved
	Files   int
	Bytes   int64
}

func runMigrateData(args []string) int {
	logger := logging.New("migrate-data")
	dest, ok := firstNonFlag(args)
	if !ok {
		logger.Error("usage: mikroview -migrate-data <destination-directory> [--force]")
		logger.Error(fmt.Sprintf("in a container, mount the new location at %s and pass that -- "+
			"a fresh named volume mounted anywhere else arrives owned by root and cannot be written to. "+
			"See docs/configuration.md, \"Moving the data directory\"", migrationMountPoint))
		return 2
	}

	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), nil)
	if err != nil {
		logger.Error(fmt.Sprintf("config: %v", err))
		return 1
	}

	plan, err := planMigration(cfg, dest, hasFlag(args, "--force"))
	if err != nil {
		logger.Error(err.Error())
		var unusable *storeUnusable
		if errors.As(err, &unusable) {
			for _, line := range migrationFailureAdvice(unusable) {
				logger.Error(line)
			}
		}
		return 1
	}

	logger.Info(fmt.Sprintf("copying %d file(s), %s from %s to %s",
		plan.Files, humanBytes(plan.Bytes), plan.Source, plan.Dest))

	digests, err := copyTree(plan.Source, plan.Dest)
	if err != nil {
		logger.Error(err.Error())
		logger.Error(fmt.Sprintf("%s may now hold a partial copy. Nothing under %s was changed -- "+
			"empty the destination and run this again", plan.Dest, plan.Source))
		return 1
	}

	if err := verifyMigration(plan, digests); err != nil {
		logger.Error(err.Error())
		logger.Error(fmt.Sprintf("the copy at %s cannot be trusted. %s is untouched -- keep using it, "+
			"empty the destination and run this again", plan.Dest, plan.Source))
		return 1
	}

	for _, line := range migrationSummary(plan, digests) {
		logger.Info(line)
	}
	for _, s := range plan.Outside {
		logger.Warn(fmt.Sprintf("%s (%s) is configured outside %s, so it is on a different mount and "+
			"has NOT been moved -- it stays where it is, and the new deployment needs it mounted there too",
			s.Name, s.Path, plan.Source))
	}
	if cfg.Postgres.DSNFile != "" {
		logger.Info("this deployment keeps its stores in Postgres, so most of what moved is unused there. " +
			"The move is still worth doing for the TLS store, the recovery pepper and the adoption marker, " +
			"which live on this directory whatever the backend. The database itself is untouched")
	}

	fmt.Println()
	fmt.Printf("Copied, and every file re-read from %s and checked byte for byte.\n", plan.Dest)
	fmt.Println()
	fmt.Println("Next:")
	fmt.Println("  1. Stop mikroview.")
	fmt.Printf("  2. Point the deployment's %s mount at the new location.\n", plan.Source)
	fmt.Println("  3. Start it, and sign in.")
	fmt.Printf("  4. Once you are satisfied, delete %s.\n", plan.Source)
	fmt.Println()
	fmt.Println("The old directory is still there and still complete -- nothing was deleted.")
	fmt.Println("It holds your accounts and recovery-key digests, so delete it rather than")
	fmt.Println("leaving it lying around once the move is confirmed.")
	return 0
}

// planMigration resolves both ends and refuses everything refusable
// before any file is created.
//
// Everything here is a check that is cheaper to fail now than halfway
// through a copy: at this point the destination is either untouched or an
// empty directory this command just made, so a refusal costs nothing.
func planMigration(cfg config.Config, dest string, force bool) (*migrationPlan, error) {
	source, err := filepath.Abs(dataDir(cfg))
	if err != nil {
		return nil, fmt.Errorf("resolving the current data directory: %w", err)
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", dest, err)
	}

	fi, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("the current data directory %s cannot be read: %v -- "+
			"run this with the same config and the same mounts the deployment uses",
			source, unwrapPathErr(err))
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory, so there is no data directory to move", source)
	}

	// Both orders matter. Copying a directory into itself never
	// terminates, and copying a directory over its own parent destroys
	// the source it is reading.
	if source == destAbs {
		return nil, fmt.Errorf("%s is already the data directory -- there is nothing to move", destAbs)
	}
	if underDir(source, destAbs) {
		return nil, fmt.Errorf("%s is inside the data directory %s. Copying a directory into itself "+
			"does not terminate. Choose a destination on the other mount", destAbs, source)
	}
	if underDir(destAbs, source) {
		return nil, fmt.Errorf("the data directory %s is inside %s. Copying over a parent of the source "+
			"would overwrite the data being read. Choose a destination on the other mount", source, destAbs)
	}

	plan := &migrationPlan{Source: source, Dest: destAbs}
	for _, s := range migratedStores(cfg) {
		if s.Path == "" {
			continue // this store's persistence is switched off
		}
		abs, err := filepath.Abs(s.Path)
		if err != nil {
			return nil, fmt.Errorf("resolving the %s store path %s: %w", s.Name, s.Path, err)
		}
		s.Path = abs
		if _, err := os.Stat(abs); err != nil {
			continue // never written; there is nothing of it to move
		}
		if underDir(source, abs) {
			plan.Stores = append(plan.Stores, s)
		} else {
			plan.Outside = append(plan.Outside, s)
		}
	}

	if err := prepareDest(destAbs, force); err != nil {
		return nil, err
	}

	// The scan is separate from the copy so that an entry the copy could
	// not faithfully reproduce -- a symlink, a socket, a device node --
	// is refused while the destination is still empty, rather than after
	// half the stores have landed.
	plan.Files, plan.Bytes, err = scanTree(source)
	if err != nil {
		return nil, err
	}
	if plan.Files == 0 {
		return nil, fmt.Errorf("%s holds no files -- there is nothing to move", source)
	}
	return plan, nil
}

// prepareDest makes sure the destination exists, is empty, and is
// genuinely writable by the uid this process runs as.
//
// Writable by trying it rather than by reading permission bits, for the
// reason checkPathUsable gives: ownership, ACLs, read-only mounts and uid
// remapping all change the answer and only an attempt accounts for all of
// them. This is the check that catches the common failure -- a
// root-owned host directory bind-mounted in, which mikroview at uid 65532
// cannot write a thing to.
func prepareDest(dest string, force bool) error {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return &storeUnusable{Store: "destination", Path: dest, Dir: dest, Err: unwrapPathErr(err)}
	}
	if err := checkDirUsable(dest); err != nil {
		return err
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		return &storeUnusable{Store: "destination", Path: dest, Dir: dest, Err: unwrapPathErr(err)}
	}
	if len(entries) > 0 && !force {
		return fmt.Errorf("%s is not empty (%d entr(y/ies)) -- refusing to merge two deployments' data "+
			"into one directory, where a leftover users.json would silently outlive the move. "+
			"Empty it, choose another destination, or pass --force if you are certain it is a "+
			"partial copy of this same deployment", dest, len(entries))
	}
	return nil
}

// scanTree counts what will be copied and refuses anything that cannot
// be reproduced faithfully.
//
// Symlinks are refused rather than followed or recreated: followed, a
// link pointing off the mount silently pulls unrelated data in; recreated,
// it dangles the moment the old mount goes away. Neither is a move, and
// guessing which one the operator meant is worse than saying so.
func scanTree(root string) (int, int64, error) {
	var files int
	var bytes int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("reading %s: %v", path, unwrapPathErr(err))
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is a %s, not a regular file. mikroview will not guess how that "+
				"should be reproduced on the destination -- remove it, or move this directory by hand",
				path, describeMode(d.Type()))
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("reading %s: %v", path, unwrapPathErr(err))
		}
		files++
		bytes += info.Size()
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return files, bytes, nil
}

// copyTree copies every file under src into dst, returning the SHA-256
// of each file keyed by its path relative to src.
//
// The digest is taken from the bytes as they are written, so
// verifyMigration comparing it against a fresh read of the destination
// checks the round trip rather than restating the same read twice.
//
// Modes are set rather than copied: 0700 for directories, 0600 for
// files, which is what every store in this codebase already writes
// itself with. A source that had drifted more permissive -- a bind mount
// someone chmod'd to get it working -- is not a permission decision worth
// carrying onto a fresh volume.
func copyTree(src, dst string) (map[string]string, error) {
	digests := map[string]string{}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("reading %s: %v", path, unwrapPathErr(err))
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fmt.Errorf("creating %s: %v", target, unwrapPathErr(err))
			}
			return nil
		}
		sum, err := copyFile(path, target)
		if err != nil {
			return err
		}
		digests[filepath.ToSlash(rel)] = sum
		return nil
	})
	if err != nil {
		return nil, err
	}
	return digests, nil
}

// copyFile writes one file and returns the SHA-256 of what it wrote.
//
// O_EXCL, so a destination that turned out to hold this file already --
// a --force run over a partial copy -- fails loudly instead of leaving
// half of one version and half of another.
func copyFile(src, dst string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("reading %s: %v", src, unwrapPathErr(err))
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%s already exists on the destination -- refusing to write half of "+
				"one copy over half of another. Empty the destination and run this again", dst)
		}
		return "", fmt.Errorf("creating %s: %v", dst, unwrapPathErr(err))
	}
	defer out.Close()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), in); err != nil {
		return "", fmt.Errorf("copying %s to %s: %v", src, dst, unwrapPathErr(err))
	}
	// Explicitly, not on the deferred close: a write failing at flush --
	// a full disk is the realistic one -- has to be an error the caller
	// sees, not a discarded return value on a copy reported as good.
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("finishing %s: %v", dst, unwrapPathErr(err))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyMigration re-reads the destination and proves the move, rather
// than reporting success because no step returned an error.
//
// Three separate questions, because they fail separately:
//
//  1. Is every file there, byte for byte? Re-read and re-hashed off the
//     destination, so a short write or a filesystem that accepted bytes
//     it did not keep is caught here.
//  2. Is there anything extra? A --force run over an older copy can
//     leave a stale file the move never wrote, and on the accounts store
//     that is an account list coming back from the dead.
//  3. Can mikroview actually use it? Ownership is right by construction
//     -- this process created every file -- but "by construction" is an
//     argument, and #536 exists because an argument like that was wrong.
func verifyMigration(plan *migrationPlan, digests map[string]string) error {
	seen := map[string]bool{}
	err := filepath.WalkDir(plan.Dest, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("re-reading %s: %v", path, unwrapPathErr(err))
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(plan.Dest, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		want, ok := digests[rel]
		if !ok {
			return fmt.Errorf("%s exists on the destination but was not part of this move -- the "+
				"destination is not a clean copy of %s", path, plan.Source)
		}
		got, err := hashFile(path)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s does not match the source: wrote %s, read back %s", path, want, got)
		}
		seen[rel] = true
		return nil
	})
	if err != nil {
		return err
	}

	missing := make([]string, 0)
	for rel := range digests {
		if !seen[rel] {
			missing = append(missing, rel)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%d file(s) were copied but are not on the destination now: %s",
			len(missing), strings.Join(missing, ", "))
	}

	for _, s := range plan.Stores {
		rel, err := filepath.Rel(plan.Source, s.Path)
		if err != nil {
			return err
		}
		target := filepath.Join(plan.Dest, rel)
		if s.Dir {
			if err := checkDirUsable(target); err != nil {
				return fmt.Errorf("the migrated %s store: %w", s.Name, err)
			}
			continue
		}
		if err := checkPathUsable(target); err != nil {
			return fmt.Errorf("the migrated %s store: %w", s.Name, err)
		}
	}
	return nil
}

// checkDirUsable is checkPathUsable for a path that is a directory: an
// O_RDWR open of a directory fails with EISDIR on Linux whatever its
// permissions, so writability has to be established by writing.
func checkDirUsable(dir string) error {
	probe, err := os.CreateTemp(dir, ".mikroview-migrate-*")
	if err != nil {
		return &storeUnusable{Store: "destination", Path: dir, Dir: dir, Err: unwrapPathErr(err)}
	}
	name := probe.Name()
	probe.Close()
	return os.Remove(name)
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("re-reading %s: %v", path, unwrapPathErr(err))
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("re-reading %s: %v", path, unwrapPathErr(err))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// migrationSummary is what moved, by store name rather than by path,
// because "watchlist" is what the operator chose and
// "/var/lib/mikroview/watchlist.json" is where it happened to land.
func migrationSummary(plan *migrationPlan, digests map[string]string) []string {
	lines := []string{fmt.Sprintf("migrated %d file(s), %s to %s, and verified every one:",
		len(digests), humanBytes(plan.Bytes), plan.Dest)}
	for _, s := range plan.Stores {
		rel, err := filepath.Rel(plan.Source, s.Path)
		if err != nil {
			rel = s.Path
		}
		if s.Dir {
			n := countUnder(digests, filepath.ToSlash(rel))
			lines = append(lines, fmt.Sprintf("  %-24s %s/ (%d file(s))", s.Name, rel, n))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %-24s %s", s.Name, rel))
	}
	return lines
}

// countUnder counts the copied files inside a store that is a directory.
func countUnder(digests map[string]string, rel string) int {
	n := 0
	for path := range digests {
		if path == rel || strings.HasPrefix(path, rel+"/") {
			n++
		}
	}
	return n
}

// migrationFailureAdvice is storeFailureAdvice's counterpart for a move
// that could not start, and shares ownershipFacts with it so the two
// cannot disagree about which uid mikroview is running as.
func migrationFailureAdvice(e *storeUnusable) []string {
	uid, gid := os.Getuid(), os.Getgid()
	lines := append([]string{
		"The destination cannot be written to, so nothing has been copied.",
		"",
	}, ownershipFacts(e.Dir)...)
	return append(lines,
		"",
		fmt.Sprintf("Fix it by giving mikroview ownership: sudo chown -R %d:%d %s", uid, gid, e.Dir),
		"In a container, run that against the host directory bind-mounted there, not",
		"the path inside the container. A named Docker volume needs none of this: Docker",
		"seeds an empty volume from the image and it ends up owned by the container user.",
	)
}

// underDir reports whether path is dir itself or anything beneath it.
// Lexical, on already-absolute cleaned paths: it is guarding against a
// destination nested in the source, and both are mount points the
// operator named.
func underDir(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

// describeMode names what an irregular directory entry actually is, so
// the refusal says "is a symlink" rather than making the operator decode
// a mode bitmask.
func describeMode(m fs.FileMode) string {
	switch {
	case m&fs.ModeSymlink != 0:
		return "symlink"
	case m&fs.ModeSocket != 0:
		return "socket"
	case m&fs.ModeNamedPipe != 0:
		return "named pipe"
	case m&fs.ModeDevice != 0:
		return "device node"
	case m&fs.ModeIrregular != 0:
		return "irregular file"
	}
	return "special file"
}

// humanBytes keeps the summary readable. Decimal units, matching how
// Docker reports volume sizes -- the numbers an operator sees either
// side of this operation should be comparable.
func humanBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}
