// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// ReadLines reads the sealed match log at path under key and returns its
// lines as plain JSON lines, each ending in a newline, oldest first --
// the shape -backup carries in its envelope (#1452). The #853 amendment
// keeps the backup envelope plain, readable JSON, with the tools running
// with the key; -restore seals the lines again under the restoring
// deployment's key (WriteSealed), so a backup moves between deployments
// with different keys.
//
// An absent, empty or header-only log reads as nil. The file is checked
// as Open checks it: a plaintext log is refused with
// persist.ErrPlaintextDocument, and a whole frame that does not open is
// an error, never fewer lines. A short tail ends the read, as for any
// reader.
func ReadLines(path string, key *retention.Key) ([]byte, error) {
	if key == nil {
		return nil, errors.New("matchlog: reading the match log needs history.keyFile: it is sealed")
	}
	size, err := sniff(path)
	if err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, nil
	}
	var out []byte
	err = retention.ReadFrames(path, key, Format, func(_ uint64, plain []byte) error {
		out = append(out, plain...)
		out = append(out, '\n')
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("matchlog: reading %s: %w", path, err)
	}
	return out, nil
}

// WriteSealed replaces the file at path with a sealed match log holding
// lines (JSON lines, as ReadLines returns them and as a v0.6 backup
// carries them), in order, under key. Every non-empty line must decode
// as a record or an update line; otherwise nothing is written. The whole
// file is built in memory -- the restore envelope already held it all --
// and written atomically with mode 0600.
//
// Restoring lines is not sealing a plaintext file in place: it writes
// what -restore was given under the restoring deployment's key, as every
// other store's restore does.
func WriteSealed(path string, key *retention.Key, lines []byte) error {
	if key == nil {
		return errors.New("matchlog: writing the match log needs history.keyFile: it is sealed, and there is no unsealed mode")
	}
	var plains [][]byte
	for i, line := range bytes.Split(lines, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var l fileLine
		if err := json.Unmarshal(line, &l); err != nil {
			return fmt.Errorf("matchlog: line %d is not a match log line: %w", i+1, err)
		}
		if l.Kind != kindRecord && l.Kind != kindUpdate {
			return fmt.Errorf("matchlog: line %d has kind %q, want %q or %q", i+1, l.Kind, kindRecord, kindUpdate)
		}
		if len(line) > maxLineBytes {
			return fmt.Errorf("matchlog: line %d is %d bytes, over the %d-byte line limit", i+1, len(line), maxLineBytes)
		}
		plains = append(plains, line)
	}
	data, err := retention.SealFrames(key, Format, plains)
	if err != nil {
		return fmt.Errorf("matchlog: %w", err)
	}
	if err := persist.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("matchlog: writing %s: %w", path, err)
	}
	return nil
}
