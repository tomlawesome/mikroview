// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"fmt"
)

// StartupError is returned by Open when a store's document exists but
// could not be loaded or parsed -- see Open's doc comment for the policy
// this enforces (issue #378).
//
// Every OpenWithBackend across the codebase used to construct its store
// with the backend already attached and then `return s, err` on this
// exact failure -- which handed the caller a store that looked usable
// but still held a live, writable backend. main.go logged a warning
// ("continuing with in-memory-only X") and kept it running, so the next
// persist call overwrote the operator's on-disk document with whatever
// the near-empty in-memory store had -- the opposite of what the log
// line claimed. StartupError, and the contract Open enforces around it,
// is the fix: no store is ever constructed on this path, so there is
// nothing left holding the backend that could write over the file.
type StartupError struct {
	// Store names the persisted store in operator-facing terms, e.g.
	// "the flags store" or "the accounts store" -- whatever the caller
	// passed to Open.
	Store string
	// Location is where the document lives -- backend.Describe(), e.g.
	// "file /var/lib/mikroview/flags.json" or "postgres store 'flags'".
	// Always credential-free, so safe straight into a log line.
	Location string
	// Err is the underlying failure: the backend's Load error, or the
	// caller's decode (typically json.Unmarshal) error against a
	// document that did load.
	Err error
}

func (e *StartupError) Error() string {
	return fmt.Sprintf(
		"%s (%s) exists but could not be loaded: %v -- refusing to start with %s in an unknown state. "+
			"This is NOT a fresh install: restore it from a backup, or deliberately move it aside and "+
			"restart to accept starting %s fresh (container/host access is required either way)",
		e.Store, e.Location, e.Err, e.Store, e.Store,
	)
}

func (e *StartupError) Unwrap() error { return e.Err }

// Open loads one store's document under mikroview's fail-closed startup
// policy (#378):
//
//   - A document that has never been written is a normal first boot:
//     Open returns a nil error, existed==false, and never calls decode.
//     The caller builds an empty store with the backend attached, same
//     as always.
//   - A document that exists but cannot be read (Load fails) or cannot
//     be parsed (decode fails) is not a degraded-but-safe state -- it is
//     refused outright, as a *StartupError. The caller MUST NOT
//     construct a store around b in that case. Doing so anyway is
//     exactly the bug #378 fixed: a store built around a backend that
//     failed to load still has a live, writable backend, and the next
//     persist call overwrites the operator's on-disk document with
//     whatever the near-empty in-memory store held.
//
// name identifies the store in the returned error, e.g. "the flags
// store". decode is called with the document's bytes exactly when one
// exists -- ordinarily a json.Unmarshal into the caller's on-disk shape,
// populating whatever local variables the caller closed over. Its error
// is wrapped identically to a Load failure: to an operator, "the file
// exists but won't parse" and "the file exists but won't read" call for
// the same response.
func Open(ctx context.Context, b Backend, name string, decode func([]byte) error) (version int64, existed bool, err error) {
	data, version, err := LoadDocument(ctx, b)
	if err != nil {
		return 0, false, &StartupError{Store: name, Location: b.Describe(), Err: err}
	}
	if data == nil {
		return 0, false, nil
	}
	if err := decode(data); err != nil {
		return 0, false, &StartupError{Store: name, Location: b.Describe(), Err: err}
	}
	return version, true, nil
}
