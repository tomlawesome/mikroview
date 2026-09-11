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
	"context"
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

// maxInFlightBytes bounds what every in-flight transfer may reserve
// between them, counted from the sizes they declared rather than from
// what has arrived so far.
//
// The device count above is not a memory bound on its own: 32 devices
// each declaring MaxFileBytes is half a gigabyte of buffers, which OOMs
// any memory-limited container -- the same failure internal/auth bounds
// its Argon2id calls against. A real backup measured ~460KB (#394), so
// 64MiB is well over a hundred routers pushing at once and only refuses
// the shapes nobody legitimate produces.
const maxInFlightBytes = 64 << 20

// idleExpiry is how long a transfer may sit with no accepted slice
// before Sweep drops it (rule 7, #955).
const idleExpiry = 15 * time.Minute

// sweepInterval is how often RunPeriodicSweep looks for those. A third
// of idleExpiry: a router that aborts mid-loop at 03:00 has its buffer
// back within five minutes of the expiry, rather than holding it until
// some other router happens to push (#1121) -- which, on a one-router
// install, could be the next night.
const sweepInterval = 5 * time.Minute

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
	//
	// This text, and ErrBadTotalSlices' below, reach the router that sent
	// the push, so they name the JSON fields it sent (totalBytes,
	// totalSlices) rather than this package's own Go field names -- an
	// operator reading a failure on the router has the request in front
	// of them, not this struct (#1122).
	ErrBadTotalBytes = errors.New("backupslice: totalBytes out of range")
	// ErrBadTotalSlices is a Begin whose TotalSlices does not match
	// ceil(TotalBytes / maxSliceBytes).
	ErrBadTotalSlices = errors.New("backupslice: totalSlices disagrees with totalBytes")
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
	// ErrSink wraps whatever the Sink returned for a file that
	// reassembled cleanly. It is not a refusal: the caller's request was
	// valid and mikroview could not keep its side of it, so an HTTP
	// caller answers a wrapped error as a server fault rather than
	// echoing the sink's own text -- a vault write failure names
	// mikroview's filesystem, which is nobody's business at the far end
	// of an ingest token (#1122).
	ErrSink = errors.New("backupslice: the sink refused a reassembled file")
)

// transfer is one device's in-progress reassembly.
type transfer struct {
	device      string
	kind        string
	totalBytes  int64
	totalSlices int
	sha256      string // lowercase hex, or "" if none was supplied
	nextIndex   int
	// chunks holds the slices accepted so far, one entry each, rather
	// than one buffer sized from the declaration: nothing is allocated
	// for bytes that have not arrived (#1121 -- a token could declare
	// 16MiB, send nothing, and repeat). received is their total length,
	// and totalBytes is the ceiling it may not pass.
	chunks     [][]byte
	received   int64
	lastActive time.Time
}

// Receiver reassembles slices into whole files and hands them to a Sink.
// The zero value is not usable -- construct with New.
type Receiver struct {
	sink Sink
	// now is the clock RunPeriodicSweep judges idleness by -- time.Now
	// everywhere but in this package's own tests, which drive the
	// background sweep without sleeping for a quarter of an hour.
	now func() time.Time

	mu        sync.Mutex
	transfers map[string]*transfer
	// byDevice enforces "one in-flight transfer per device" (rule 6) and
	// doubles as the count Begin checks maxInFlightDevices against --
	// it is always in 1:1 correspondence with the devices holding an
	// entry in transfers.
	byDevice map[string]string
}

// reservedBytesLocked is what every in-flight transfer except except's
// has declared. Caller holds r.mu.
func (r *Receiver) reservedBytesLocked(except string) int64 {
	var total int64
	for _, t := range r.transfers {
		if t.device == except {
			continue
		}
		total += t.totalBytes
	}
	return total
}

// New returns a Receiver that hands finished files to sink.
func New(sink Sink) *Receiver {
	return &Receiver{
		sink:      sink,
		now:       time.Now,
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
		log.Warn(fmt.Sprintf("refused a transfer from %s: totalBytes %d out of range", device, b.TotalBytes))
		return "", ErrBadTotalBytes
	}
	// TotalBytes is already bounded above, so this division cannot
	// overflow int before the comparison below.
	wantSlices := int((b.TotalBytes + maxSliceBytes - 1) / maxSliceBytes)
	if b.TotalSlices != wantSlices {
		log.Warn(fmt.Sprintf("refused a transfer from %s: totalSlices %d disagrees with totalBytes %d (want %d)",
			device, b.TotalSlices, b.TotalBytes, wantSlices))
		return "", ErrBadTotalSlices
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, already := r.byDevice[device]; !already && len(r.byDevice) >= maxInFlightDevices {
		log.Warn(fmt.Sprintf("refused a transfer from %s: %d devices already in flight", device, maxInFlightDevices))
		return "", ErrBusy
	}
	// The device's own previous transfer is about to be discarded, so it
	// does not count against the budget it is being replaced within.
	if r.reservedBytesLocked(device)+b.TotalBytes > maxInFlightBytes {
		log.Warn(fmt.Sprintf("refused a transfer from %s: %d bytes already reserved by transfers in flight", device, r.reservedBytesLocked(device)))
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
	// Nothing is allocated for the file here: TotalBytes is a
	// declaration, not bytes in hand, and sizing a buffer from it let one
	// token churn gigabytes by declaring 16MiB repeatedly and sending
	// nothing (#1121). The buffers arrive with the slices instead, and
	// TotalBytes stays what it always was -- the ceiling Slice refuses to
	// let the accumulated bytes pass, and the figure the budget above
	// reserves against.
	r.transfers[id] = &transfer{
		device:      device,
		kind:        b.Kind,
		totalBytes:  b.TotalBytes,
		totalSlices: b.TotalSlices,
		sha256:      strings.ToLower(b.SHA256),
		lastActive:  now,
	}
	r.byDevice[device] = id
	return id, nil
}

// Completed describes a transfer that a Slice call finished: the kind
// and size of the file that actually reached the sink. The zero value --
// what every slice but the last returns -- means nothing completed.
//
// The caller gets these back rather than reading them off the slice
// request because the router's push script sends neither on a slice
// (internal/routeros's backupPushHTTPSBlock), so an audit line built
// from the request recorded every completed backup as "kind= bytes=0"
// (#1122).
type Completed struct {
	Done  bool
	Kind  string
	Bytes int64
}

// Slice accepts one slice of an in-progress transfer. The returned
// Completed is non-zero only when this was the final slice and the whole
// file was handed to the sink successfully.
func (r *Receiver) Slice(device, transferID string, index int, data []byte, now time.Time) (Completed, error) {
	// Copied before the lock is taken. The bytes belong to the caller
	// (an HTTP handler's decoded body, reusable after it returns), so
	// they have to be copied somewhere -- and doing it here keeps the one
	// per-slice allocation off the critical section every other router's
	// begin/slice is queued behind.
	chunk := bytes.Clone(data)

	tr, err := r.acceptSlice(device, transferID, index, chunk, now)
	if err != nil {
		return Completed{}, err
	}
	if tr == nil {
		// More slices to come.
		return Completed{}, nil
	}

	// Everything below runs with r.mu released: acceptSlice took the
	// finished transfer out of the map, so nothing else can reach it and
	// the lock would buy nothing. That matters because this is the
	// expensive half -- one allocation of the whole file, a SHA-256 over
	// it, then the vault's seal plus two atomic writes and their fsyncs.
	// Holding the receiver lock across that blocked every other device's
	// POST behind one slow disk, and a blocked POST is a /tool fetch
	// timeout, which aborts that router's whole transfer (#1121).
	if tr.received != tr.totalBytes {
		log.Warn(fmt.Sprintf("refused a transfer from %s: reassembled %d bytes, want %d -- dropping the transfer", device, tr.received, tr.totalBytes))
		return Completed{}, ErrCorrupt
	}
	// Exactly one allocation, at the size already proven to equal the
	// declared total.
	whole := make([]byte, 0, tr.received)
	for _, c := range tr.chunks {
		whole = append(whole, c...)
	}
	if tr.sha256 != "" {
		sum := sha256.Sum256(whole)
		// Not a secret-comparison: the router reports its own hash of
		// bytes it also chose to send, so a timing side channel here
		// would leak nothing an attacker sending the bytes doesn't
		// already know. Constant-time compare is for secrets, not for
		// checking a sender's own integrity claim about its own data.
		if hex.EncodeToString(sum[:]) != tr.sha256 {
			log.Warn(fmt.Sprintf("refused a transfer from %s: SHA256 mismatch -- dropping the transfer", device))
			return Completed{}, ErrCorrupt
		}
	}

	if err := r.sink.Store(device, tr.kind, whole, now); err != nil {
		// The transfer is already gone, and nothing here puts it back:
		// the reassembled bytes are lost when the sink refuses them.
		// That is the intended outcome, not an oversight -- the router
		// script has no resume and starts from "begin" on its next
		// scheduled run anyway (#955), so keeping the buffer resident
		// would hold memory for a retry that never comes.
		log.Warn(fmt.Sprintf("a transfer from %s reassembled but the sink refused it: %v", device, err))
		return Completed{}, fmt.Errorf("%w: %w", ErrSink, err)
	}
	return Completed{Done: true, Kind: tr.kind, Bytes: tr.totalBytes}, nil
}

// acceptSlice validates one slice and records it against its transfer,
// holding r.mu for that and nothing else. It returns the finished
// transfer -- already removed from the map, so the caller owns it
// outright -- when this was the last slice, and nil when more are due.
func (r *Receiver) acceptSlice(device, transferID string, index int, chunk []byte, now time.Time) (*transfer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tr, ok := r.transfers[transferID]
	if !ok || tr.device != device {
		// Same error either way (rule 2): a wrong device must not be
		// able to tell "not yours" from "doesn't exist".
		return nil, ErrNotFound
	}

	if index != tr.nextIndex {
		log.Warn(fmt.Sprintf("refused a slice from %s: index %d, want %d -- dropping the transfer", device, index, tr.nextIndex))
		r.dropLocked(transferID)
		return nil, ErrOutOfOrder
	}
	if len(chunk) > maxSliceBytes {
		log.Warn(fmt.Sprintf("refused a slice from %s: %d bytes over the %d-byte cap -- dropping the transfer", device, len(chunk), maxSliceBytes))
		r.dropLocked(transferID)
		return nil, ErrSliceTooLarge
	}
	if tr.received+int64(len(chunk)) > tr.totalBytes {
		log.Warn(fmt.Sprintf("refused a slice from %s: accumulated bytes would exceed the declared total -- dropping the transfer", device))
		r.dropLocked(transferID)
		return nil, ErrTotalExceeded
	}
	// Header check on slice 0 of a backup: refuse before the operator
	// uploads the rest of a ~460KB file the vault would reject anyway.
	if index == 0 && tr.kind == backupvault.KindBackup && !looksLikeRouterOSBackup(chunk) {
		log.Warn(fmt.Sprintf("refused a transfer from %s: the first bytes are not a RouterOS backup header -- dropping the transfer", device))
		r.dropLocked(transferID)
		return nil, ErrNotABackup
	}

	tr.chunks = append(tr.chunks, chunk)
	tr.received += int64(len(chunk))
	tr.nextIndex++
	tr.lastActive = now

	if tr.nextIndex < tr.totalSlices {
		return nil, nil
	}
	// Final slice. The transfer leaves the map before its size and
	// checksum are verified, exactly as it did when both ran inline: a
	// file that fails either check is refused and dropped, and the sink
	// never sees it (rule 8).
	r.dropLocked(transferID)
	return tr, nil
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

// RunPeriodicSweep drops idle transfers on a fixed cadence until ctx is
// done -- the same ticker/select/recover shape as
// suggest.Store.RunPeriodicSync, started the same way from main.go.
//
// Sweep also runs from the ingest handler, which is cheap and reclaims a
// buffer the moment the next router pushes. It is not enough on its own:
// a router that aborts mid-loop leaves its buffer resident until some
// router pushes again, which on a quiet install is the next night, and
// meanwhile it holds part of the 64MiB budget other devices' begins are
// refused against (#1121).
func (r *Receiver) RunPeriodicSweep(ctx context.Context) {
	r.runPeriodicSweep(ctx, sweepInterval)
}

// runPeriodicSweep is RunPeriodicSweep with the cadence as a parameter,
// so a test can drive the loop without waiting minutes for a tick.
func (r *Receiver) runPeriodicSweep(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweepOnceRecovered()
		}
	}
}

// sweepOnceRecovered isolates panic recovery to a single pass rather
// than the whole loop's lifetime -- a defer in runPeriodicSweep itself
// would end background sweeping for good on the first bad pass, the
// reasoning suggest.Store.syncOnceRecovered's doc comment gives for its
// identical shape.
func (r *Receiver) sweepOnceRecovered() {
	defer logging.Recover(log)
	if n := r.Sweep(r.now()); n > 0 {
		log.Info(fmt.Sprintf("dropped %d abandoned backup transfer(s) idle for more than %s", n, idleExpiry))
	}
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
// backup the vault would reject anyway (#955's measurement note).
// Unifying it with classifyBackup is tracked on #1124.
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
