// SPDX-License-Identifier: AGPL-3.0-only

// Package backup reads and writes mikroview's whole persisted state as a
// single gzipped JSON document (issue #97).
//
// One document, keyed by store name, with no filenames in it at all. Not
// tar: there is no requirement to interoperate with anything, so the
// format with zero path-traversal surface beats the one that has to
// defend against it. No "..", no absolute paths, no symlink entries, no
// duplicate-entry games -- eliminated by construction rather than
// mitigated. archive/tar never validates Header.Linkname in any mode, and
// the CVE history here is long enough (four rounds on `kubectl cp` alone,
// each bypassed) that avoiding the category is worth more than handling
// it well.
//
// It is still a gzip stream, so the gzip defences remain: see Read.
package backup

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"
)

// FormatVersion is the envelope's own version, independent of the app's.
// A reader that does not recognise it refuses rather than guessing.
const FormatVersion = 1

// MaxDecompressed caps what Read will accept *after* decompression.
//
// 256 MiB is far above any real deployment -- the largest store is the
// flag history, which is bounded and small -- and far below what a
// decompression bomb wants. A few hundred bytes of gzip can expand to
// gigabytes; the cap is what stops that becoming the process's memory.
const MaxDecompressed = 256 << 20

// Envelope is the document. Stores are held as raw JSON so backup never
// has to understand, and therefore never has to keep up with, the shape
// of each store.
type Envelope struct {
	Format     int                        `json:"format"`
	Created    time.Time                  `json:"created"`
	AppVersion string                     `json:"appVersion"`
	Stores     map[string]json.RawMessage `json:"stores"`
}

// ErrUnsupportedFormat is a refusal, not a parse failure: the document is
// well-formed but written by a version whose meaning this build cannot
// vouch for.
var ErrUnsupportedFormat = errors.New("backup: unsupported format version")

// Write serialises stores into w as a gzipped envelope.
func Write(w io.Writer, appVersion string, stores map[string][]byte) error {
	env := Envelope{
		Format:     FormatVersion,
		Created:    time.Now().UTC(),
		AppVersion: appVersion,
		Stores:     make(map[string]json.RawMessage, len(stores)),
	}
	// Sorted for a reproducible document: two backups of identical state
	// should differ only in the timestamp, so a diff is meaningful.
	names := make([]string, 0, len(stores))
	for name := range stores {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		env.Stores[name] = json.RawMessage(stores[name])
	}

	zw := gzip.NewWriter(w)
	enc := json.NewEncoder(zw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		zw.Close()
		return fmt.Errorf("backup: encoding: %w", err)
	}
	return zw.Close()
}

// Read parses a document produced by Write, refusing anything that looks
// like an attack on the reader rather than a backup.
//
// Restore consumes a file an operator may have been handed, so this is a
// parser facing hostile input. Four specific refusals:
//
//   - A decompression bomb. io.LimitReader is set to MaxDecompressed+1 on
//     the *decompressed* side, and the extra byte is what makes the check
//     work: without it, hitting the limit is indistinguishable from a
//     clean end of stream, and a bomb truncated exactly at the cap reads
//     as a valid short backup.
//   - Concatenated gzip members. Multistream(false) stops the reader at
//     the first member, so a second one appended to a legitimate backup
//     is not silently included.
//   - Unknown fields. DisallowUnknownFields means a document carrying
//     something this build does not understand is refused rather than
//     partially applied.
//   - A trailing document. A single Decode leaves anything after the
//     first JSON value unread, so the decoder is asked for a second value
//     and must report EOF.
func Read(r io.Reader) (Envelope, error) {
	var env Envelope

	zr, err := gzip.NewReader(r)
	if err != nil {
		return env, fmt.Errorf("backup: not a gzip stream: %w", err)
	}
	defer zr.Close()
	zr.Multistream(false)

	limited := io.LimitReader(zr, MaxDecompressed+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return env, fmt.Errorf("backup: reading: %w", err)
	}
	if len(data) > MaxDecompressed {
		return env, fmt.Errorf("backup: refusing a backup larger than %d bytes decompressed -- "+
			"a small file that expands without bound is a decompression bomb, not a backup", MaxDecompressed)
	}

	dec := json.NewDecoder(newByteReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		return env, fmt.Errorf("backup: parsing: %w", err)
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return env, errors.New("backup: trailing data after the document")
	}

	if env.Format != FormatVersion {
		return env, fmt.Errorf("%w: found %d, this build writes and reads %d",
			ErrUnsupportedFormat, env.Format, FormatVersion)
	}
	if len(env.Stores) == 0 {
		return env, errors.New("backup: document contains no stores")
	}
	for name, raw := range env.Stores {
		if !json.Valid(raw) {
			return env, fmt.Errorf("backup: store %q is not valid JSON", name)
		}
	}
	return env, nil
}
