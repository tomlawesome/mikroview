// SPDX-License-Identifier: AGPL-3.0-only

// Package backupslice reassembles a router backup delivered as small
// slices over the HTTPS ingest channel (#955) -- the second way (beside
// #394's SFTP push) a router with no open SFTP port gets its backup into
// mikroview. RouterOS forces the slicing: `/file read`'s `chunk-size`
// caps at 32768 bytes (measured 2026-09-05 on RouterOS 7.23.3, #955), so
// a ~460KB backup arrives as roughly 15 POSTs, and a slice refused
// mid-loop aborts the router script -- there is no partial-resume case,
// so any refusal here drops the whole transfer rather than trying to
// salvage it.
//
// This package only reassembles. The HTTP endpoint (wired separately)
// owns authenticating the caller and scoping it to one device; Sink is
// the minimal slice of internal/backupvault this package needs, so a
// finished file can be handed off without this package depending on the
// vault's concrete type or knowing it exists.
package backupslice

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/backupvault"
	"github.com/tomlawesome/mikroview/internal/logging"
)

// maxSliceBytes mirrors RouterOS's own `/file read` `chunk-size` cap
// (#955's measurement note) -- a slice bigger than this could not have
// come from the router script working as designed.
const maxSliceBytes = 32768

// maxInFlightDevices bounds this receiver's worst-case memory: each
// in-flight transfer buffers up to backupvault.MaxFileBytes (16MiB), so
// 32 devices in flight at once is 512MiB, not unbounded growth from an
// arbitrary number of routers all mid-upload.
const maxInFlightDevices = 32

// idleExpiry is how long a transfer may sit with no accepted slice
// before Sweep drops it (rule 7, #955).
const idleExpiry = 15 * time.Minute

var log = logging.New("backupslice")

// Sink is the part of internal/backupvault this package needs -- taking
// the interface rather than *backupvault.Vault keeps this package
// testable without a vault and keeps the vault ignorant of which
// receiver (SFTP or this one) a file came from.
type Sink interface {
	Store(device, kind string, data []byte, now time.Time) error
}

// Begin describes a transfer a device is about to start.
type Begin struct {
	Kind        string // backupvault.KindBackup or backupvault.KindRsc
	TotalBytes  int64
	TotalSlices int
	SHA256      string // hex, OPTIONAL -- may be empty
}

// Sentinel refusal reasons, one per rule this package enforces -- the
// same shape as internal/backupvault/vault.go's own error block, so a
// caller (and this package's own tests) can tell one refusal from
// another with errors.Is.
var (
	// ErrNotFound covers both an unrecognised transfer id and one that
	// belongs to a different device: the same error either way, so a
	// caller cannot use the response to learn whether another device has
	// a transfer in flight.
	ErrNotFound = errors.New("backupslice: no such transfer")
	// ErrOutOfOrder is a slice whose index is not the next expected one.
	ErrOutOfOrder = errors.New("backupslice: slice index out of order")
	// ErrSliceTooLarge is a single slice over maxSliceBytes.
	ErrSliceTooLarge = errors.New("backupslice: slice exceeds the 32KiB cap")
	// ErrBadTotalBytes is a Begin whose TotalBytes is not in [1, backupvault.MaxFileBytes].
	ErrBadTotalBytes = errors.New("backupslice: TotalBytes out of range")
	// ErrBadTotalSlices is a Begin whose TotalSlices does not match
	// ceil(TotalBytes / maxSliceBytes).
	ErrBadTotalSlices = errors.New("backupslice: TotalSlices disagrees with TotalBytes")
	// ErrTotalExceeded is a slice that would push the accumulated byte
	// count past the Begin's declared TotalBytes.
	ErrTotalExceeded = errors.New("backupslice: accumulated bytes exceed the declared total")
	// ErrUnknownKind is a Begin whose Kind is neither backupvault.KindBackup
	// nor backupvault.KindRsc.
	ErrUnknownKind = errors.New("backupslice: unrecognised kind")
	// ErrNotABackup is slice 0 of a KindBackup transfer whose first bytes
	// are not a RouterOS backup header.
	ErrNotABackup = errors.New("backupslice: the first bytes are not a RouterOS backup header")
	// ErrBusy is a Begin from a device not already in flight, arriving
	// when maxInFlightDevices devices already have one.
	ErrBusy = errors.New("backupslice: too many devices already have a transfer in flight")
	// ErrCorrupt is a completed transfer whose reassembled size or
	// (if supplied) SHA256 does not match what Begin declared.
	ErrCorrupt = errors.New("backupslice: reassembled file failed its integrity check")
)

// transfer is one device's in-progress reassembly.
type transfer struct {
	device      string
	kind        string
	totalBytes  int64
	totalSlices int
	sha256      string // lowercase hex, or "" if none was supplied
	nextIndex   int
	buf         []byte
	lastActive  time.Time
}

// Receiver reassembles slices into whole files and hands them to a Sink.
// The zero value is not usable -- construct with New.
type Receiver struct {
	sink Sink

	mu        sync.Mutex
	transfers map[string]*transfer
	// byDevice enforces "one in-flight transfer per device" (rule 6) and
	// doubles as the count Begin checks maxInFlightDevices against --
	// it is always in 1:1 correspondence with the devices holding an
	// entry in transfers.
	byDevice map[string]string
}

// New returns a Receiver that hands finished files to sink.
func New(sink Sink) *Receiver {
	return &Receiver{
		sink:      sink,
		transfers: map[string]*transfer{},
		byDevice:  map[string]string{},
	}
}

// newTransferID mints a server-assigned id -- rule 1: never taken from
// the caller, so a client cannot pick or predict another device's id.
func newTransferID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("backupslice: generating a transfer id: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// Begin starts a new transfer for device, returning the server-assigned
// transfer id a matching sequence of Slice calls must present.
func (r *Receiver) Begin(device string, b Begin, now time.Time) (string, error) {
	switch b.Kind {
	case backupvault.KindBackup, backupvault.KindRsc:
	default:
		log.Warn(fmt.Sprintf("refused a transfer from %s: unrecognised kind %q", device, b.Kind))
		return "", ErrUnknownKind
	}
	if b.TotalBytes < 1 || b.TotalBytes > backupvault.MaxFileBytes {
		log.Warn(fmt.Sprintf("refused a transfer from %s: TotalBytes %d out of range", device, b.TotalBytes))
		return "", ErrBadTotalBytes
	}
	// TotalBytes is already bounded above, so this division cannot
	// overflow int before the comparison below.
	wantSlices := int((b.TotalBytes + maxSliceBytes - 1) / maxSliceBytes)
	if b.TotalSlices != wantSlices {
		log.Warn(fmt.Sprintf("refused a transfer from %s: TotalSlices %d disagrees with TotalBytes %d (want %d)",
			device, b.TotalSlices, b.TotalBytes, wantSlices))
		return "", ErrBadTotalSlices
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, already := r.byDevice[device]; !already && len(r.byDevice) >= maxInFlightDevices {
		log.Warn(fmt.Sprintf("refused a transfer from %s: %d devices already in flight", device, maxInFlightDevices))
		return "", ErrBusy
	}

	// A new Begin discards this device's previous unfinished transfer
	// (rule 6) -- the router script aborts and restarts from scratch on
	// any refusal, so there is never a reason to keep the old one around.
	if old, ok := r.byDevice[device]; ok {
		delete(r.transfers, old)
	}

	id, err := newTransferID()
	if err != nil {
		return "", err
	}
	r.transfers[id] = &transfer{
		device:      device,
		kind:        b.Kind,
		totalBytes:  b.TotalBytes,
		totalSlices: b.TotalSlices,
		sha256:      strings.ToLower(b.SHA256),
		buf:         make([]byte, 0, b.TotalBytes),
		lastActive:  now,
	}
	r.byDevice[device] = id
	return id, nil
}

// Slice accepts one slice of an in-progress transfer. done reports
// whether this was the final slice and the whole file was handed to the
// sink successfully.
func (r *Receiver) Slice(device, transferID string, index int, data []byte, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tr, ok := r.transfers[transferID]
	if !ok || tr.device != device {
		// Same error either way (rule 2): a wrong device must not be
		// able to tell "not yours" from "doesn't exist".
		return false, ErrNotFound
	}

	if index != tr.nextIndex {
		log.Warn(fmt.Sprintf("refused a slice from %s: index %d, want %d -- dropping the transfer", device, index, tr.nextIndex))
		r.dropLocked(transferID)
		return false, ErrOutOfOrder
	}
	if len(data) > maxSliceBytes {
		log.Warn(fmt.Sprintf("refused a slice from %s: %d bytes over the %d-byte cap -- dropping the transfer", device, len(data), maxSliceBytes))
		r.dropLocked(transferID)
		return false, ErrSliceTooLarge
	}
	if int64(len(tr.buf)+len(data)) > tr.totalBytes {
		log.Warn(fmt.Sprintf("refused a slice from %s: accumulated bytes would exceed the declared total -- dropping the transfer", device))
		r.dropLocked(transferID)
		return false, ErrTotalExceeded
	}
	// Header check on slice 0 of a backup: refuse before the operator
	// uploads the rest of a ~460KB file the vault would reject anyway.
	if index == 0 && tr.kind == backupvault.KindBackup && !looksLikeRouterOSBackup(data) {
		log.Warn(fmt.Sprintf("refused a transfer from %s: the first bytes are not a RouterOS backup header -- dropping the transfer", device))
		r.dropLocked(transferID)
		return false, ErrNotABackup
	}

	tr.buf = append(tr.buf, data...)
	tr.nextIndex++
	tr.lastActive = now

	if tr.nextIndex < tr.totalSlices {
		return false, nil
	}

	// Final slice: verify size and (if supplied) checksum before the
	// sink is ever called (rule 8) -- a mismatch here must never reach
	// the vault.
	if int64(len(tr.buf)) != tr.totalBytes {
		log.Warn(fmt.Sprintf("refused a transfer from %s: reassembled %d bytes, want %d -- dropping the transfer", device, len(tr.buf), tr.totalBytes))
		r.dropLocked(transferID)
		return false, ErrCorrupt
	}
	if tr.sha256 != "" {
		sum := sha256.Sum256(tr.buf)
		// Not a secret-comparison: the router reports its own hash of
		// bytes it also chose to send, so a timing side channel here
		// would leak nothing an attacker sending the bytes doesn't
		// already know. Constant-time compare is for secrets, not for
		// checking a sender's own integrity claim about its own data.
		if hex.EncodeToString(sum[:]) != tr.sha256 {
			log.Warn(fmt.Sprintf("refused a transfer from %s: SHA256 mismatch -- dropping the transfer", device))
			r.dropLocked(transferID)
			return false, ErrCorrupt
		}
	}

	whole, kind := tr.buf, tr.kind
	r.dropLocked(transferID)
	if err := r.sink.Store(device, kind, whole, now); err != nil {
		log.Warn(fmt.Sprintf("a transfer from %s reassembled but the sink refused it: %v", device, err))
		return false, err
	}
	return true, nil
}

// dropLocked removes a transfer and its device's in-flight marker.
// Callers must hold r.mu.
func (r *Receiver) dropLocked(id string) {
	tr, ok := r.transfers[id]
	if !ok {
		return
	}
	delete(r.transfers, id)
	if r.byDevice[tr.device] == id {
		delete(r.byDevice, tr.device)
	}
}

// Sweep drops transfers idle for more than idleExpiry, judged against
// now rather than time.Now() so tests can drive the clock (rule 7).
// Returns how many were dropped.
func (r *Receiver) Sweep(now time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int
	for id, tr := range r.transfers {
		if now.Sub(tr.lastActive) > idleExpiry {
			r.dropLocked(id)
			n++
		}
	}
	return n
}

// InFlight reports how many transfers are currently in progress -- for
// tests and the settings surface.
func (r *Receiver) InFlight() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.transfers)
}

// looksLikeRouterOSBackup deliberately mirrors
// internal/backupvault/vault.go's classifyBackup magic-byte check (that
// function is unexported, so it can't be called from here) -- refusing
// slice 0 of a bad upload here saves the other ~14 POSTs of a 461KB
// backup the vault would reject anyway (#955's measurement note). Should
// be unified with classifyBackup when the two packages are next touched.
func looksLikeRouterOSBackup(data []byte) bool {
	plainMagic := []byte{0x88, 0xac, 0xa1, 0xb1}
	encryptedMagic := []byte{0xef, 0xa8, 0x91} // 4th byte varies: rc4 vs aes-sha256
	if len(data) >= len(plainMagic) && bytes.Equal(data[:len(plainMagic)], plainMagic) {
		return true
	}
	if len(data) >= len(encryptedMagic) && bytes.Equal(data[:len(encryptedMagic)], encryptedMagic) {
		return true
	}
	return false
}
