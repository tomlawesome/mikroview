// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	saltBytes     = 16
	headerBytes   = len(magic) + 1 + saltBytes
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

// dayFile is one open day's file, held by the writer between flushes.
type dayFile struct {
	day  string
	path string
	f    *os.File
	aead cipher.AEAD
	// frames counts frames already written, and is the sequence number
	// mixed into the next frame's additional data. It is recovered by
	// counting existing frames when an existing file is reopened, so a
	// restart mid-day continues the sequence rather than repeating
	// numbers a reader would then accept out of order.
	frames uint64
}

// openDayFile opens (or creates) the file for day under dir.
//
// Reopening an existing file has to recover two things before it can
// append: the salt, because the file's key is derived from it, and the
// frame count, because that is the next frame's sequence number. Both
// come from walking the frames already there, which also has the useful
// side effect of finding a truncated tail from a previous crash -- the
// file is truncated back to the last whole frame before anything new is
// appended, so a partial frame never sits in the middle of a file.
func openDayFile(dir, day string, key *Key) (*dayFile, error) {
	path := filepath.Join(dir, fileNameFor(day))
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("retention: opening %s: %w", path, err)
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
		}
	}()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("retention: stat %s: %w", path, err)
	}

	var salt []byte
	var frames uint64
	switch {
	case info.Size() == 0:
		salt = make([]byte, saltBytes)
		if _, err := rand.Read(salt); err != nil {
			return nil, fmt.Errorf("retention: generating salt: %w", err)
		}
		header := make([]byte, 0, headerBytes)
		header = append(header, magic...)
		header = append(header, formatVersion)
		header = append(header, salt...)
		if _, err := f.Write(header); err != nil {
			return nil, fmt.Errorf("retention: writing header to %s: %w", path, err)
		}
	default:
		var end int64
		salt, frames, end, err = scanFrames(f)
		if err != nil {
			return nil, err
		}
		// Drop a partial tail left by a crash mid-write, so the append
		// below lands on a frame boundary.
		if end != info.Size() {
			if err := f.Truncate(end); err != nil {
				return nil, fmt.Errorf("retention: truncating partial frame in %s: %w", path, err)
			}
		}
		if _, err := f.Seek(end, io.SeekStart); err != nil {
			return nil, fmt.Errorf("retention: seeking %s: %w", path, err)
		}
	}

	aead, err := aeadFor(key, salt, day)
	if err != nil {
		return nil, err
	}
	ok = true
	return &dayFile{day: day, path: path, f: f, aead: aead, frames: frames}, nil
}

// aeadFor builds the AES-256-GCM cipher for one file.
func aeadFor(key *Key, salt []byte, day string) (cipher.AEAD, error) {
	derived, err := key.fileKey(salt, day)
	if err != nil {
		return nil, fmt.Errorf("retention: deriving file key: %w", err)
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

// frameAAD binds a frame to its file and its position in it.
//
// Without this a frame is a self-contained sealed blob: valid wherever
// it is put. With it, moving a frame to another day's file or reordering
// two frames within one file makes them fail to open, so the window a
// replay reports cannot be quietly rearranged by someone with write
// access to the directory but not the key. They can still delete, and
// deletion is visible in the window the replay reports rather than
// invisible in its contents.
func frameAAD(day string, seq uint64) []byte {
	aad := make([]byte, 0, len(magic)+1+len(day)+8)
	aad = append(aad, magic...)
	aad = append(aad, formatVersion)
	aad = append(aad, day...)
	return binary.BigEndian.AppendUint64(aad, seq)
}

// appendFrame compresses, seals and appends one batch of events.
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

	nonce := make([]byte, d.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("retention: generating nonce: %w", err)
	}
	sealed := d.aead.Seal(nil, nonce, plain.Bytes(), frameAAD(d.day, d.frames))

	frame := make([]byte, 0, 4+len(nonce)+len(sealed))
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(nonce)+len(sealed)))
	frame = append(frame, nonce...)
	frame = append(frame, sealed...)
	if _, err := d.f.Write(frame); err != nil {
		return fmt.Errorf("retention: appending to %s: %w", d.path, err)
	}
	// Sync so that what the reader can see matches what the writer has
	// been told it wrote. A retained corpus that loses its last minutes
	// to the page cache on a hard reset would still report the window it
	// believed it held, which is the one thing a receipt must never do.
	if err := d.f.Sync(); err != nil {
		return fmt.Errorf("retention: syncing %s: %w", d.path, err)
	}
	d.frames++
	return nil
}

func (d *dayFile) Close() error {
	if d == nil || d.f == nil {
		return nil
	}
	err := d.f.Close()
	d.f = nil
	return err
}

// scanFrames walks a file's frames without decrypting them, reporting
// its salt, how many whole frames it holds, and the offset the last
// whole frame ends at.
//
// It reads lengths only, so reopening a large file costs seeks rather
// than decryption. A short or impossible length prefix ends the walk
// and the offset before it is treated as the end -- that is the crash
// tail case, not corruption worth refusing over.
func scanFrames(f *os.File) (salt []byte, frames uint64, end int64, err error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, 0, 0, fmt.Errorf("retention: seeking to header: %w", err)
	}
	header := make([]byte, headerBytes)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, 0, 0, fmt.Errorf("retention: reading header: %w", err)
	}
	if string(header[:len(magic)]) != magic {
		return nil, 0, 0, errors.New("retention: not a retained event file")
	}
	if header[len(magic)] != formatVersion {
		return nil, 0, 0, fmt.Errorf("retention: unsupported file format version %d", header[len(magic)])
	}
	salt = header[len(magic)+1:]

	off := int64(headerBytes)
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("retention: sizing file: %w", err)
	}
	var lenBuf [4]byte
	for {
		if off+4 > size {
			break
		}
		if _, err := f.ReadAt(lenBuf[:], off); err != nil {
			break
		}
		n := int64(binary.BigEndian.Uint32(lenBuf[:]))
		if n <= 0 || n > maxFrameBytes || off+4+n > size {
			break
		}
		off += 4 + n
		frames++
	}
	return salt, frames, off, nil
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
// altered, and neither should pass silently.
func replayFile(path, day string, key *Key, visit func(store.Event)) (n int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("retention: opening %s: %w", path, err)
	}
	defer f.Close()

	salt, _, _, err := scanFrames(f)
	if err != nil {
		return 0, err
	}
	aead, err := aeadFor(key, salt, day)
	if err != nil {
		return 0, err
	}
	if _, err := f.Seek(int64(headerBytes), io.SeekStart); err != nil {
		return 0, fmt.Errorf("retention: seeking %s: %w", path, err)
	}

	r := bufio.NewReader(f)
	var lenBuf [4]byte
	for seq := uint64(0); ; seq++ {
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return n, nil // clean end, or a tail too short to be a frame
		}
		size := int(binary.BigEndian.Uint32(lenBuf[:]))
		if size <= aead.NonceSize() || size > maxFrameBytes {
			return n, nil
		}
		frame := make([]byte, size)
		if _, err := io.ReadFull(r, frame); err != nil {
			return n, nil // partial frame: the crash tail case
		}
		plain, err := aead.Open(nil, frame[:aead.NonceSize()], frame[aead.NonceSize():], frameAAD(day, seq))
		if err != nil {
			return n, fmt.Errorf("retention: %s frame %d did not open -- wrong key, or the file has been altered", filepath.Base(path), seq)
		}
		count, err := visitBatch(plain, visit)
		n += count
		if err != nil {
			return n, err
		}
	}
}

// visitBatch decompresses one frame's plaintext and hands each event to
// visit.
func visitBatch(plain []byte, visit func(store.Event)) (int, error) {
	gz, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		return 0, fmt.Errorf("retention: decompressing frame: %w", err)
	}
	defer gz.Close()
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
