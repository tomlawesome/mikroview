// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"bufio"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FrameFormat names one sealed, framed, append-only file format.
//
//	magic      len(Magic) bytes
//	version    1 byte    Version
//	salt      16 bytes   random, per file
//	frames     repeated  uint32 big-endian length, then that many bytes
//
// A frame is a random nonce followed by an AES-256-GCM seal of one
// caller-defined plaintext, under a key derived from the file's salt and
// KeyInfo. Each frame's additional data is AADPrefix followed by the
// frame's 0-based sequence number as a big-endian uint64, which binds it
// to its file and its position: a frame moved, dropped from the middle,
// or copied from another file does not open.
//
// Two formats use it: this package's day files ("MVEVT") and the
// watchlist match log ("MVMLG", internal/matchlog, #1470). Not detected,
// in either: frames cut off the end (indistinguishable from a crash tail
// and handled as one) and the whole file replaced by an older valid copy.
type FrameFormat struct {
	Magic   string
	Version byte
	// KeyInfo is the HKDF info string the per-file key is derived under.
	KeyInfo string
	// AADPrefix precedes the sequence number in every frame's
	// additional data.
	AADPrefix []byte
	// MaxFrameBytes bounds what a reader will allocate for one frame. A
	// corrupt or hostile length prefix otherwise asks for an arbitrary
	// allocation on the strength of four bytes off disk. It is a
	// ceiling, not a target. A length above it ends the walk.
	MaxFrameBytes int
	// Description names the file in errors ("retained event file").
	Description string
}

const saltBytes = 16

// ErrFrameDidNotOpen is the failure of a whole frame to open: the wrong
// key, or a file somebody has altered (a frame moved, dropped, copied in
// or changed). Unlike a short tail it is never passed over silently.
var ErrFrameDidNotOpen = errors.New("did not open -- wrong key, or the file has been altered")

func (ff FrameFormat) headerBytes() int { return len(ff.Magic) + 1 + saltBytes }

func (ff FrameFormat) aad(seq uint64) []byte {
	aad := make([]byte, 0, len(ff.AADPrefix)+8)
	aad = append(aad, ff.AADPrefix...)
	return binary.BigEndian.AppendUint64(aad, seq)
}

// HasMagic reports whether b begins with this format's magic.
func (ff FrameFormat) HasMagic(b []byte) bool {
	return len(b) >= len(ff.Magic) && string(b[:len(ff.Magic)]) == ff.Magic
}

// FrameLog is one open framed file, held by its writer.
type FrameLog struct {
	path   string
	f      *os.File
	key    *Key
	format FrameFormat
	aead   cipher.AEAD
	// frames counts frames already written, and is the sequence number
	// mixed into the next frame's additional data. It is recovered by
	// counting existing frames when an existing file is reopened, so a
	// restart continues the sequence rather than repeating numbers a
	// reader would then accept out of order.
	frames uint64
	// end is the offset the last whole frame ends at: where the next
	// frame is written, and where a failed append is cut back to.
	end int64
	// broken is set when a failed append could not be cut back, so the
	// file's tail is unknown; every later Append refuses with it.
	broken error
}

// OpenFrameLog opens (or creates) the framed file at path for appending.
//
// Reopening an existing file has to recover two things before it can
// append: the salt, because the file's key is derived from it, and the
// frame count, because that is the next frame's sequence number. Both
// come from walking the frames already there by length only, which also
// finds a truncated tail from a previous crash -- the file is truncated
// back to the last whole frame before anything new is appended, so a
// partial frame never sits in the middle of a file. Frames are not
// opened here; a caller that must vouch for every frame reads them with
// ReadFrames.
//
// The handle is read-write, not append-mode, so truncation and Reset
// position writes explicitly.
func OpenFrameLog(path string, key *Key, format FrameFormat) (*FrameLog, error) {
	if key == nil {
		return nil, errors.New("retention: opening a framed file needs a key")
	}
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

	l := &FrameLog{path: path, f: f, key: key, format: format}
	switch {
	case info.Size() == 0:
		if err := l.writeHeader(); err != nil {
			return nil, err
		}
	default:
		salt, frames, end, err := scanFrames(f, format)
		if err != nil {
			return nil, err
		}
		// Drop a partial tail left by a crash mid-write, so the next
		// append lands on a frame boundary.
		if end != info.Size() {
			if err := f.Truncate(end); err != nil {
				return nil, fmt.Errorf("retention: truncating partial frame in %s: %w", path, err)
			}
		}
		if _, err := f.Seek(end, io.SeekStart); err != nil {
			return nil, fmt.Errorf("retention: seeking %s: %w", path, err)
		}
		aead, err := aeadFromInfo(key, salt, format.KeyInfo)
		if err != nil {
			return nil, err
		}
		l.aead, l.frames, l.end = aead, frames, end
	}
	ok = true
	return l, nil
}

// writeHeader writes a header with a fresh salt at the handle's current
// position (the start of an empty file) and keys the log to it.
func (l *FrameLog) writeHeader() error {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("retention: generating salt: %w", err)
	}
	header := make([]byte, 0, l.format.headerBytes())
	header = append(header, l.format.Magic...)
	header = append(header, l.format.Version)
	header = append(header, salt...)
	if _, err := l.f.Write(header); err != nil {
		return fmt.Errorf("retention: writing header to %s: %w", l.path, err)
	}
	aead, err := aeadFromInfo(l.key, salt, l.format.KeyInfo)
	if err != nil {
		return err
	}
	l.aead, l.frames, l.end = aead, 0, int64(len(header))
	return nil
}

// Path is the file this log writes.
func (l *FrameLog) Path() string { return l.path }

// Frames is how many whole frames the file holds.
func (l *FrameLog) Frames() uint64 { return l.frames }

// Append seals plain as the next frame and writes it in one Write, then
// syncs.
//
// Sync so that what a reader can see matches what the writer has been
// told it wrote: a log that loses its last entries to the page cache on
// a hard reset would still report what it believed it held.
//
// A write or sync that fails is cut back to where the frame began, so a
// failed append leaves neither a partial frame mid-file nor a whole one
// the frame count does not know about (whose sequence number the next
// append would reuse). If even that cut fails, the log refuses every
// later append rather than guess at its own tail.
func (l *FrameLog) Append(plain []byte) error {
	if l.broken != nil {
		return l.broken
	}
	nonce := make([]byte, l.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("retention: generating nonce: %w", err)
	}
	sealed := l.aead.Seal(nil, nonce, plain, l.format.aad(l.frames))

	frame := make([]byte, 0, 4+len(nonce)+len(sealed))
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(nonce)+len(sealed)))
	frame = append(frame, nonce...)
	frame = append(frame, sealed...)
	if _, err := l.f.Write(frame); err != nil {
		return l.cutBack(fmt.Errorf("retention: appending to %s: %w", l.path, err))
	}
	if err := l.f.Sync(); err != nil {
		return l.cutBack(fmt.Errorf("retention: syncing %s: %w", l.path, err))
	}
	l.frames++
	l.end += int64(len(frame))
	return nil
}

// cutBack undoes a failed append: truncates the file to l.end and puts
// the write position there. It returns cause, or, when the cut itself
// fails, an error naming both that also marks the log broken.
func (l *FrameLog) cutBack(cause error) error {
	err := l.f.Truncate(l.end)
	if err == nil {
		_, err = l.f.Seek(l.end, io.SeekStart)
	}
	if err == nil {
		err = l.f.Sync()
	}
	if err != nil {
		l.broken = fmt.Errorf("%w; cutting %s back to %d bytes also failed (%v), so it takes no more writes until reopened", cause, l.path, l.end, err)
		return l.broken
	}
	return cause
}

// Reset empties the file and starts it again under a fresh salt, with
// the frame count back at 0. A fresh salt is a fresh derived key, so
// sequence numbers restarting at 0 never repeat a key and additional
// data pair.
func (l *FrameLog) Reset() error {
	if err := l.f.Truncate(0); err != nil {
		return fmt.Errorf("retention: truncating %s: %w", l.path, err)
	}
	if _, err := l.f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("retention: rewinding %s: %w", l.path, err)
	}
	if err := l.writeHeader(); err != nil {
		return err
	}
	l.broken = nil // the tail is known again: there is none
	return nil
}

// Close closes the file. Closing a closed or nil log is a no-op.
func (l *FrameLog) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// SealFrames builds a whole framed file in memory: a header with a fresh
// salt, then one frame per entry of plains, sequence from 0. For a writer
// that replaces a file at once (restore) rather than appending to it.
func SealFrames(key *Key, format FrameFormat, plains [][]byte) ([]byte, error) {
	if key == nil {
		return nil, errors.New("retention: sealing a framed file needs a key")
	}
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("retention: generating salt: %w", err)
	}
	aead, err := aeadFromInfo(key, salt, format.KeyInfo)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, format.headerBytes())
	out = append(out, format.Magic...)
	out = append(out, format.Version)
	out = append(out, salt...)
	for seq, plain := range plains {
		nonce := make([]byte, aead.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return nil, fmt.Errorf("retention: generating nonce: %w", err)
		}
		sealed := aead.Seal(nil, nonce, plain, format.aad(uint64(seq)))
		out = binary.BigEndian.AppendUint32(out, uint32(len(nonce)+len(sealed)))
		out = append(out, nonce...)
		out = append(out, sealed...)
	}
	return out, nil
}

// checkFrameLength is the one rule for a frame's declared length n at
// offset off in a file of size bytes. A torn tail -- fewer than 4 bytes
// left, or a declared length running past the end of the file -- is
// reported as torn, for the caller to stop at (and the writer to cut
// back). A length of zero, over the format's ceiling, or too short to
// hold a nonce, with the whole of it inside the file, is an error: it is
// damage, not a crash tail, and treating it as one would cut off every
// whole frame after it.
//
// The past-the-end test comes first, so a tail whose length bytes are
// not a length at all (the start of a JSON line from before v0.7.0, say)
// is still a torn tail rather than a refusal.
func checkFrameLength(name string, seq uint64, off, size, n int64, format FrameFormat) (torn bool, err error) {
	if off+4+n > size {
		return true, nil
	}
	if n <= nonceBytes || n > int64(format.MaxFrameBytes) {
		return false, fmt.Errorf("retention: %s frame %d at offset %d declares length %d: %w", name, seq, off, n, ErrFrameDidNotOpen)
	}
	return false, nil
}

// nonceBytes is the AES-GCM nonce size every frame starts with.
const nonceBytes = 12

// scanFrames walks a file's frames without decrypting them, reporting
// its salt, how many whole frames it holds, and the offset the last
// whole frame ends at.
//
// It reads lengths only, so reopening a large file costs seeks rather
// than decryption. A torn tail ends the walk and the offset before it is
// treated as the end; a damaged length inside the file is an error (see
// checkFrameLength).
func scanFrames(f *os.File, format FrameFormat) (salt []byte, frames uint64, end int64, err error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, 0, 0, fmt.Errorf("retention: seeking to header: %w", err)
	}
	header := make([]byte, format.headerBytes())
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, 0, 0, fmt.Errorf("retention: reading header: %w", err)
	}
	if !format.HasMagic(header) {
		return nil, 0, 0, fmt.Errorf("retention: not a %s", format.Description)
	}
	if header[len(format.Magic)] != format.Version {
		return nil, 0, 0, fmt.Errorf("retention: unsupported file format version %d", header[len(format.Magic)])
	}
	salt = header[len(format.Magic)+1:]

	off := int64(format.headerBytes())
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("retention: sizing file: %w", err)
	}
	var lenBuf [4]byte
	for off+4 <= size {
		if _, err := f.ReadAt(lenBuf[:], off); err != nil {
			return nil, 0, 0, fmt.Errorf("retention: reading frame %d length: %w", frames, err)
		}
		n := int64(binary.BigEndian.Uint32(lenBuf[:]))
		torn, err := checkFrameLength(filepath.Base(f.Name()), frames, off, size, n, format)
		if err != nil {
			return nil, 0, 0, err
		}
		if torn {
			break
		}
		off += 4 + n
		frames++
	}
	return salt, frames, off, nil
}

// ReadFrames visits every frame of the framed file at path, oldest
// first, through its own read handle.
//
// A torn tail -- fewer than 4 bytes left, or a declared length running
// past the end of the file as it stood when the read began -- ends the
// walk silently and keeps everything before it: a crash mid-write, or a
// write still in progress. A damaged length inside the file, or a frame
// of good length that does not open, is an error naming the frame,
// because it means either the wrong key or a file somebody has altered,
// and neither should pass silently. An error from visit ends the walk
// and is returned.
func ReadFrames(path string, key *Key, format FrameFormat, visit func(seq uint64, plain []byte) error) error {
	if key == nil {
		return errors.New("retention: reading a framed file needs a key")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("retention: opening %s: %w", path, err)
	}
	defer f.Close()

	salt, _, _, err := scanFrames(f, format)
	if err != nil {
		return err
	}
	aead, err := aeadFromInfo(key, salt, format.KeyInfo)
	if err != nil {
		return err
	}
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("retention: sizing %s: %w", path, err)
	}
	off := int64(format.headerBytes())
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return fmt.Errorf("retention: seeking %s: %w", path, err)
	}

	r := bufio.NewReader(io.LimitReader(f, size-off))
	var lenBuf [4]byte
	for seq := uint64(0); off+4 <= size; seq++ {
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return fmt.Errorf("retention: reading %s frame %d: %w", filepath.Base(path), seq, err)
		}
		n := int64(binary.BigEndian.Uint32(lenBuf[:]))
		torn, err := checkFrameLength(filepath.Base(path), seq, off, size, n, format)
		if err != nil {
			return err
		}
		if torn {
			return nil
		}
		frame := make([]byte, n)
		if _, err := io.ReadFull(r, frame); err != nil {
			return fmt.Errorf("retention: reading %s frame %d: %w", filepath.Base(path), seq, err)
		}
		plain, err := aead.Open(nil, frame[:aead.NonceSize()], frame[aead.NonceSize():], format.aad(seq))
		if err != nil {
			return fmt.Errorf("retention: %s frame %d %w", filepath.Base(path), seq, ErrFrameDidNotOpen)
		}
		if err := visit(seq, plain); err != nil {
			return err
		}
		off += 4 + n
	}
	return nil
}
