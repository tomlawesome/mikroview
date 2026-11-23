// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// The on-disk shape of one day's retained events.
//
//	magic      5 bytes   "MVEVT"
//	version    1 byte    formatVersion
//	salt      16 bytes   random, per file
//	frames     repeated  uint32 length, then that many bytes
//
// A frame is a nonce followed by an AES-256-GCM seal of one gzipped
// batch of events. Batches rather than one seal per event because the
// per-frame overhead (12-byte nonce, 16-byte tag, a gzip header) would
// otherwise be a large share of an event that packs to well under a
// hundred bytes, and because gzip has nothing to work with until it has
// seen a run of similar lines -- syslog-shaped rows compress five to
// ten times, but only in bulk.
//
// Appending frames rather than rewriting a file is what makes a crash
// survivable: everything already flushed stays readable, and a partial
// tail is detected and dropped by the reader rather than poisoning the
// file. See replayFile.
const (
	magic         = "MVEVT"
	formatVersion = 1
	// maxFrameBytes bounds what the reader will allocate for a single
	// frame. A corrupt or hostile length prefix otherwise asks for an
	// arbitrary allocation on the strength of four bytes off disk. The
	// writer never produces a frame anywhere near this: it is a ceiling,
	// not a target.
	maxFrameBytes = 64 << 20
)

// record is the wire form of one event.
//
// store.Event's own ReceivedAt is json:"-" -- it is deliberately not
// part of the API's event shape -- but it is the field every replay
// orders and windows by, so it cannot be left out here. Carrying it as
// its own field rather than re-tagging store.Event keeps the API's
// contract and this file's needs from being the same decision.
type record struct {
	Event      store.Event `json:"e"`
	ReceivedAt time.Time   `json:"r"`
}

// dayOf is the day a given instant belongs to, in UTC.
//
// UTC, not the host's zone, so a deployment that changes timezone -- or
// one whose files are read on another machine -- never ends up with two
// files claiming the same day or a day that silently gains or loses an
// hour.
func dayOf(t time.Time) string { return t.UTC().Format("2006-01-02") }

// fileNameFor is the name of the file holding day's events.
func fileNameFor(day string) string { return "events-" + day + ".mvevt" }

// dayFromFileName reports the day a retained file's name claims, and
// whether the name is one of ours at all.
func dayFromFileName(name string) (string, bool) {
	if !bytes.HasPrefix([]byte(name), []byte("events-")) || filepath.Ext(name) != ".mvevt" {
		return "", false
	}
	day := name[len("events-") : len(name)-len(".mvevt")]
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return "", false
	}
	return day, true
}

// dayFormat is the framing of one day's file. The bytes are the ones
// this file wrote before the framing moved to framelog.go (#1470): the
// additional data is magic, version, day, then the sequence number.
func dayFormat(day string) FrameFormat {
	prefix := make([]byte, 0, len(magic)+1+len(day))
	prefix = append(prefix, magic...)
	prefix = append(prefix, formatVersion)
	prefix = append(prefix, day...)
	return FrameFormat{
		Magic:         magic,
		Version:       formatVersion,
		KeyInfo:       keyInfoPrefix + day,
		AADPrefix:     prefix,
		MaxFrameBytes: maxFrameBytes,
		Description:   "retained event file",
	}
}

// dayFile is one open day's file, held by the writer between flushes.
type dayFile struct {
	day string
	log *FrameLog
}

// openDayFile opens (or creates) the file for day under dir. Recovering
// the salt and frame count of an existing file, and cutting back a torn
// tail, is OpenFrameLog's.
func openDayFile(dir, day string, key *Key) (*dayFile, error) {
	l, err := OpenFrameLog(filepath.Join(dir, fileNameFor(day)), key, dayFormat(day))
	if err != nil {
		return nil, err
	}
	return &dayFile{day: day, log: l}, nil
}

// aeadFromInfo builds the AES-256-GCM cipher for a key derived under one
// info string. Shared by FrameLog (framelog.go's framed files) and
// Seal/Open (seal.go's whole-document envelope for other packages holding
// the same master key), so there is exactly one place that turns derived
// key material into a cipher.AEAD.
func aeadFromInfo(key *Key, salt []byte, info string) (cipher.AEAD, error) {
	derived, err := key.Derive(salt, info)
	if err != nil {
		return nil, fmt.Errorf("retention: deriving key: %w", err)
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, fmt.Errorf("retention: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("retention: gcm: %w", err)
	}
	return aead, nil
}

// appendFrame compresses one batch of events and appends it as a frame.
//
// The frame's additional data binds it to its day and position, so
// moving a frame to another day's file or reordering two frames within
// one makes them fail to open: the window a replay reports cannot be
// quietly rearranged by someone with write access to the directory but
// not the key. They can still delete, and deletion is visible in the
// window the replay reports rather than invisible in its contents.
func (d *dayFile) appendFrame(batch []record) error {
	if len(batch) == 0 {
		return nil
	}
	var plain bytes.Buffer
	gz := gzip.NewWriter(&plain)
	enc := json.NewEncoder(gz)
	for i := range batch {
		if err := enc.Encode(&batch[i]); err != nil {
			return fmt.Errorf("retention: encoding event: %w", err)
		}
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("retention: compressing batch: %w", err)
	}
	return d.log.Append(plain.Bytes())
}

func (d *dayFile) Close() error {
	if d == nil {
		return nil
	}
	return d.log.Close()
}

// replayFile visits every event in one day's file, oldest first, and
// reports how many it read.
//
// Two failure modes are deliberately not errors. A truncated tail stops
// the walk and keeps everything before it: the alternative -- refusing
// the whole day because the process was killed mid-flush -- would throw
// away a day of evidence to punish a frame. A frame that fails to open
// stops the walk for the same reason, but is reported, because unlike a
// short tail it means either the wrong key or a file somebody has
// altered, and neither should pass silently. See ReadFrames.
func replayFile(path, day string, key *Key, visit func(store.Event)) (n int, err error) {
	err = ReadFrames(path, key, dayFormat(day), func(_ uint64, plain []byte) error {
		count, err := visitBatch(plain, visit)
		n += count
		return err
	})
	return n, err
}

// visitBatch decompresses one frame's plaintext and hands each event to
// visit.
func visitBatch(plain []byte, visit func(store.Event)) (int, error) {
	gz, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		return 0, fmt.Errorf("retention: decompressing frame: %w", err)
	}
	defer func() { _ = gz.Close() }()
	dec := json.NewDecoder(gz)
	n := 0
	for {
		var rec record
		if err := dec.Decode(&rec); err != nil {
			if errors.Is(err, io.EOF) {
				return n, nil
			}
			return n, fmt.Errorf("retention: decoding event: %w", err)
		}
		rec.Event.ReceivedAt = rec.ReceivedAt
		visit(rec.Event)
		n++
	}
}
