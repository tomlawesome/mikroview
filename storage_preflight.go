// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/tomlawesome/mikroview/internal/config"
)

// checkStoresUsable stops startup when a configured store cannot be read
// or written, instead of starting anyway and losing the operator's work
// a change at a time (#536).
//
// The old behaviour was to log an error per store and carry on: "this
// change exists only in memory and will be lost on restart". Everything
// then worked -- you could add a watchlist entry, see it in the
// interface, restart, and find it gone. The app knew, said so once in a
// startup log nobody was reading, and kept pretending.
//
// The operator chose where their data lives when they set the deployment
// up, with a volume or a bind mount. If that choice has stopped working,
// the honest response is to stop and say which path and why, not to
// quietly carry on without it. A mikroview that refuses to start gets
// fixed in minutes; one that silently forgets is discovered weeks later,
// when whatever it forgot was needed.
//
// Deliberately NOT checked:
//
//   - empty paths. Leaving a store path unset is a supported choice --
//     the store just doesn't persist -- and honouring the operator's
//     configuration is the whole point of this check, not overriding it.
//   - anything, on a Postgres deployment. Those stores live in the
//     database, so the file paths are unused and their permissions mean
//     nothing.
func checkStoresUsable(cfg config.Config) error {
	if cfg.Postgres.DSNFile != "" {
		return nil
	}

	seen := make(map[string]bool)
	for _, s := range backedUpStores(cfg) {
		if s.Path == "" || seen[s.Path] {
			continue
		}
		seen[s.Path] = true
		if err := checkPathUsable(s.Path); err != nil {
			var unusable *storeUnusable
			if errors.As(err, &unusable) {
				unusable.Store = s.Name
				return unusable
			}
			return fmt.Errorf("the %s store at %w", s.Name, err)
		}
	}
	return nil
}

// storeUnusable carries the directory that actually has to change, so
// the advice printed alongside it can name that path and its current
// ownership rather than guessing at the default data directory.
type storeUnusable struct {
	Store string
	Path  string
	Dir   string
	Err   error
}

func (e *storeUnusable) Error() string {
	return fmt.Sprintf("the %s store at %s is not usable: %v", e.Store, e.Path, e.Err)
}

func (e *storeUnusable) Unwrap() error { return e.Err }

// checkPathUsable answers "could mikroview actually use this path" by
// trying it, rather than reading permission bits and reasoning about
// them. Ownership, group membership, ACLs, read-only mounts and
// container uid remapping all change the answer, and only an attempt
// accounts for every one of them.
func checkPathUsable(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err == nil {
		f.Close()
		return nil
	}
	if !os.IsNotExist(err) {
		return &storeUnusable{Path: path, Dir: filepath.Dir(path), Err: unwrapPathErr(err)}
	}

	// Not there yet: a first run, or a store that has never been
	// written. What matters then is whether the directory it goes in
	// will accept it.
	//
	// Created rather than merely required, because that is what the
	// stores themselves do on their first write (internal/persist's
	// file.go, internal/auth's recovery.go, and servertls). A check
	// stricter than the code it guards would refuse deployments that
	// actually work.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &storeUnusable{Path: path, Dir: dir, Err: unwrapPathErr(err)}
	}
	probe, err := os.CreateTemp(dir, ".mikroview-preflight-*")
	if err != nil {
		// Report the directory, not the throwaway probe file: its
		// random name is an implementation detail and only muddies an
		// error the operator has to act on.
		return &storeUnusable{Path: path, Dir: dir, Err: unwrapPathErr(err)}
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return nil
}

// unwrapPathErr strips the *os.PathError wrapper so the message reads
// "permission denied" rather than repeating a path -- often a throwaway
// probe file -- that the caller is about to name properly anyway.
func unwrapPathErr(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

// storeFailureAdvice turns a refusal into something an operator can act
// on without going looking: the ids mikroview is actually running as,
// what the directory is owned by now, and the exact command that
// reconciles the two.
//
// The ids are read at runtime rather than hardcoded to the image's
// 65532, because --user, a rebuilt image or rootless uid remapping all
// change them, and advice that is confidently wrong about which id to
// chown to is worse than none.
func storeFailureAdvice(e *storeUnusable) []string {
	uid, gid := os.Getuid(), os.Getgid()

	lines := append([]string{
		"Refusing to start: continuing would appear to work while silently discarding",
		"every change to that store on the next restart.",
		"",
	}, ownershipFacts(e.Dir)...)

	lines = append(lines,
		"",
		fmt.Sprintf("Fix it by giving mikroview ownership: sudo chown -R %d:%d %s", uid, gid, e.Dir),
		"In a container, run that against the host directory bind-mounted there, not",
		"the path inside the container. The shipped deploy/docker-compose.yml avoids",
		"this entirely by mounting the mikroview-data volume at "+config.DefaultDataDir+".",
		"",
		"Changing your mind about where the data lives is a supported move rather than",
		"a hand-copy: mikroview -migrate-data <destination> carries every store across",
		"and verifies it. See docs/configuration.md, \"Moving the data directory\".",
	)
	return lines
}

// ownershipFacts states who mikroview is and who owns the directory in
// question, which is the pair an operator needs side by side to
// understand any permission refusal here.
//
// Shared by storeFailureAdvice and -migrate-data's
// migrationFailureAdvice (migrate_cli.go) so the two cannot disagree
// about which uid mikroview is running as. Advice that is confidently
// wrong about the id to chown to is worse than none, and two copies of
// this would eventually be wrong in one of them.
//
// The ids are read at runtime rather than hardcoded to the image's
// 65532, because --user, a rebuilt image or rootless uid remapping all
// change them.
func ownershipFacts(dir string) []string {
	lines := []string{fmt.Sprintf("Mikroview is running as uid %d, gid %d.", os.Getuid(), os.Getgid())}
	fi, err := os.Stat(dir)
	if err != nil {
		return append(lines, fmt.Sprintf("%s could not be read at all: %v", dir, unwrapPathErr(err)))
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return lines
	}
	return append(lines, fmt.Sprintf("%s is owned by uid %d, gid %d with mode %04o.",
		dir, st.Uid, st.Gid, fi.Mode().Perm()))
}
