// SPDX-License-Identifier: AGPL-3.0-only

// Package backupvault keeps the RouterOS configuration backups pushed
// over SFTP (#394), encrypted at rest under the same retention key #853
// covers the state store and event history with.
//
// A "generation" is one script run's pair of files -- the binary
// `.backup` that restores a router whole, and the `.rsc` text export
// kept for reading and, later, config scanning (#895/#435). Ten
// generations are kept per router, oldest dropped first, and a
// generation is never overwritten in place: a new arrival either starts
// one (the `.backup`, which is always sent first by the wizard's
// script) or completes the most recently opened one (the `.rsc`).
//
// Nothing here talks to a router, and nothing here decides what may
// write into it -- that is internal/backupsftp's job, which calls
// Store only after its own login and per-device isolation checks.
package backupvault

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// MaxGenerations is how many generations the vault keeps per router
// before the oldest is dropped (#394, owner decision 2026-09-05).
const MaxGenerations = 10

// MaxFileBytes is the per-file cap. A loaded router's `.backup` measured
// at ~460KB (#394 notes); 16MiB is far above any real configuration and
// exists to bound a misbehaving or hostile sender, not to accommodate a
// legitimate one.
const MaxFileBytes = 16 << 20

// Kinds a login may write. Anything else is refused before it reaches
// Store at all -- see internal/backupsftp's filename check -- but Store
// checks again since it is the security boundary, not the SFTP layer.
const (
	KindBackup = "backup"
	KindRsc    = "rsc"
)

var (
	// ErrDisabled reports that no retention key is configured. Per #394's
	// "no key, no backups" rule this is the drop box being closed, not a
	// fault -- the caller (internal/backupsftp) turns it into a refused
	// login.
	ErrDisabled = errors.New("backupvault: no retention key configured -- the drop box is closed")
	// ErrNotABackup is a refusal reason: a file claiming to be a
	// `.backup` whose first bytes match neither RouterOS header.
	ErrNotABackup = errors.New("backupvault: the first bytes are not a RouterOS backup header")
	// ErrOverCap is a refusal reason: the file is larger than
	// MaxFileBytes.
	ErrOverCap = errors.New("backupvault: file exceeds the per-file cap")
	// ErrUnknownKind is a refusal reason: the destination name is
	// neither `.backup` nor `.rsc`.
	ErrUnknownKind = errors.New("backupvault: unrecognised destination file name")
	// ErrNotFound is returned by Open/Download for a router or
	// generation this vault does not hold.
	ErrNotFound = errors.New("backupvault: no such router or generation")
)

// HeaderLabel is what Store found in a `.backup`'s first bytes.
type HeaderLabel string

const (
	// HeaderPlain is `88 ac a1 b1` -- dont-encrypt=yes, the restore copy
	// the wizard's script asks for.
	HeaderPlain HeaderLabel = "plain"
	// HeaderEncrypted is `ef a8 91 xx` -- accepted, but the router's own
	// password is needed to open it; mikroview never holds that.
	HeaderEncrypted HeaderLabel = "encrypted"
	// HeaderText is a `.rsc` export -- no magic bytes, read as text.
	HeaderText HeaderLabel = "text"
)

var (
	plainMagic     = []byte{0x88, 0xac, 0xa1, 0xb1}
	encryptedMagic = []byte{0xef, 0xa8, 0x91} // 4th byte varies: 0x72 rc4, 0x73 aes-sha256
)

// classifyBackup reports what a `.backup` file's header says it is.
func classifyBackup(data []byte) (HeaderLabel, bool) {
	if len(data) >= len(plainMagic) && bytes.Equal(data[:len(plainMagic)], plainMagic) {
		return HeaderPlain, true
	}
	if len(data) >= len(encryptedMagic) && bytes.Equal(data[:len(encryptedMagic)], encryptedMagic) {
		return HeaderEncrypted, true
	}
	return "", false
}

// Generation is one script run's pair, as reported to callers -- the
// vault's own persisted shape (generationMeta) is not exported, so a
// caller can't reach around Store/Open to touch the files directly.
type Generation struct {
	ID              string
	BackupArrivedAt time.Time
	RscArrivedAt    time.Time
	BackupSize      int64
	RscSize         int64
	// Header is the `.backup`'s header label. Empty if the backup half
	// of this generation has not arrived (the `.rsc` came alone, or the
	// backup upload is still pending).
	Header HeaderLabel
}

// HasBackup/HasRsc report which half of the pair has arrived.
func (g Generation) HasBackup() bool { return !g.BackupArrivedAt.IsZero() }
func (g Generation) HasRsc() bool    { return !g.RscArrivedAt.IsZero() }

// ArrivedAt is the generation's own timestamp for ordering and display:
// whichever file arrived, most recently.
func (g Generation) ArrivedAt() time.Time {
	if g.RscArrivedAt.After(g.BackupArrivedAt) {
		return g.RscArrivedAt
	}
	return g.BackupArrivedAt
}

// generationMeta is the persisted shape of one Generation.
type generationMeta struct {
	ID              string      `json:"id"`
	BackupArrivedAt time.Time   `json:"backupArrivedAt,omitzero"`
	RscArrivedAt    time.Time   `json:"rscArrivedAt,omitzero"`
	BackupSize      int64       `json:"backupSize,omitempty"`
	RscSize         int64       `json:"rscSize,omitempty"`
	Header          HeaderLabel `json:"header,omitempty"`
}

func (g *generationMeta) toGeneration() Generation {
	return Generation{
		ID:              g.ID,
		BackupArrivedAt: g.BackupArrivedAt,
		RscArrivedAt:    g.RscArrivedAt,
		BackupSize:      g.BackupSize,
		RscSize:         g.RscSize,
		Header:          g.Header,
	}
}

// routerMeta is one router's generations, oldest first.
type routerMeta struct {
	Generations []*generationMeta `json:"generations"`
}

// vaultMeta is the whole persisted document, sealed under the retention
// key as a single blob (metaFileName) beside the per-generation files it
// describes.
type vaultMeta struct {
	Routers map[string]*routerMeta `json:"routers"`
}

const metaFileName = "meta.enc"

// sealInfoPrefix namespaces this package's use of retention.Key.
// SealDocument/OpenDocument, per that method's own doc comment: a
// document sealed for one purpose must not silently open for another.
const sealInfoPrefix = "backupvault/v1/"

// Vault is the router-backup store. A nil *retention.Key means no key is
// configured, and Store/Open refuse accordingly (ErrDisabled) -- the
// zero-value contract every other nil-means-disabled dependency in this
// codebase uses (Server.NetClass, Server.History, ...).
type Vault struct {
	dir string
	key *retention.Key
	log *slog.Logger

	mu   sync.Mutex
	meta vaultMeta
	// seq disambiguates two generations opened within the same
	// nanosecond -- possible on a fast filesystem or in a test driving
	// the clock by hand.
	seq atomic.Uint64
}

// Open loads (or prepares to create) the vault at dir. key nil means no
// retention key is configured -- the returned Vault is still safe to
// hold and query (Enabled() reports false, Routers()/Generations() report
// empty), but Store and Open both return ErrDisabled. This mirrors
// #853's "no key, no storage" rule: the drop box exists as a concept
// (so Settings can say so) but holds nothing and accepts nothing.
func Open(dir string, key *retention.Key) (*Vault, error) {
	v := &Vault{dir: dir, key: key, log: logging.New("backupvault"), meta: vaultMeta{Routers: map[string]*routerMeta{}}}
	if key == nil {
		return v, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("backupvault: creating %s: %w", dir, err)
	}
	sealed, err := os.ReadFile(filepath.Join(dir, metaFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return v, nil
		}
		return nil, fmt.Errorf("backupvault: reading %s: %w", metaFileName, err)
	}
	plain, err := key.OpenDocument(sealInfoPrefix+"meta", sealed)
	if err != nil {
		return nil, fmt.Errorf("backupvault: opening the vault index: %w", err)
	}
	if err := json.Unmarshal(plain, &v.meta); err != nil {
		return nil, fmt.Errorf("backupvault: parsing the vault index: %w", err)
	}
	if v.meta.Routers == nil {
		v.meta.Routers = map[string]*routerMeta{}
	}
	return v, nil
}

// Enabled reports whether a retention key is configured -- whether the
// drop box a login reaches is open at all.
func (v *Vault) Enabled() bool { return v != nil && v.key != nil }

// dirNameFor is the on-disk directory a device's files live under: a
// fixed-length hash of the device name, never the name itself. Device
// names pass internal/auth's validDeviceID (printable, <=64 chars,
// control characters refused) but that still permits "..", "/" and
// friends, and this is the one place a login-scoped write turns into a
// filesystem path -- worth being unconditionally safe about rather than
// trusting a sibling package's validation to keep doing so.
func dirNameFor(device string) string {
	sum := sha256.Sum256([]byte(device))
	return hex.EncodeToString(sum[:16])
}

func (v *Vault) routerDir(device string) string {
	return filepath.Join(v.dir, dirNameFor(device))
}

func (v *Vault) fileName(generationID, kind string) string {
	return generationID + "." + kind + ".enc"
}

// nextGenerationID mints an id for a new generation, ordered by arrival
// and unique even within the same nanosecond.
func (v *Vault) nextGenerationID(now time.Time) string {
	n := v.seq.Add(1)
	return fmt.Sprintf("%s-%06d", now.UTC().Format("20060102T150405.000000000Z"), n%1000000)
}

// Store commits one uploaded file to the vault. kind is KindBackup or
// KindRsc, derived by the caller from the SFTP destination name's
// extension. now is passed in rather than read here so tests -- and the
// missed-push arithmetic that shares this clock -- stay deterministic.
//
// A `.backup` always starts a new generation: the wizard's script always
// sends it first (backup save, export, then two fetches), so treating it
// as "a new run has started" is exactly what it means. A `.rsc` attaches
// to the most recently opened generation that has no `.rsc` yet, or
// starts a bare one of its own if none is open -- the case where mikroview
// restarted between the two fetches of one run.
func (v *Vault) Store(device, kind string, data []byte, now time.Time) error {
	if v == nil || v.key == nil {
		return ErrDisabled
	}
	if len(data) > MaxFileBytes {
		v.log.Warn(fmt.Sprintf("refused a push from %s: %s is %d bytes, over the %d-byte cap -- nothing kept",
			device, kind, len(data), MaxFileBytes))
		return ErrOverCap
	}

	var header HeaderLabel
	switch kind {
	case KindBackup:
		label, ok := classifyBackup(data)
		if !ok {
			v.log.Warn(fmt.Sprintf("refused a push from %s: the first bytes are not a RouterOS backup header -- nothing kept", device))
			return ErrNotABackup
		}
		header = label
	case KindRsc:
		header = HeaderText
	default:
		v.log.Warn(fmt.Sprintf("refused a push from %s: unrecognised destination name kind %q", device, kind))
		return ErrUnknownKind
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	rm := v.meta.Routers[device]
	if rm == nil {
		rm = &routerMeta{}
		v.meta.Routers[device] = rm
	}

	var gen *generationMeta
	switch kind {
	case KindBackup:
		gen = &generationMeta{ID: v.nextGenerationID(now), BackupArrivedAt: now, BackupSize: int64(len(data)), Header: header}
		rm.Generations = append(rm.Generations, gen)
	case KindRsc:
		if n := len(rm.Generations); n > 0 && rm.Generations[n-1].RscArrivedAt.IsZero() {
			gen = rm.Generations[n-1]
		} else {
			gen = &generationMeta{ID: v.nextGenerationID(now)}
			rm.Generations = append(rm.Generations, gen)
		}
		gen.RscArrivedAt = now
		gen.RscSize = int64(len(data))
	}

	sealed, err := v.key.SealDocument(sealInfoPrefix+device+"/"+gen.ID+"/"+kind, data)
	if err != nil {
		return fmt.Errorf("backupvault: sealing %s: %w", kind, err)
	}
	dir := v.routerDir(device)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("backupvault: creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, v.fileName(gen.ID, kind))
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		return fmt.Errorf("backupvault: writing %s: %w", path, err)
	}

	// Evict the oldest generations beyond the cap. Their files are
	// deleted outright -- there is no undo, matching "the eleventh pair
	// lets the oldest go" (round 44).
	for len(rm.Generations) > MaxGenerations {
		oldest := rm.Generations[0]
		rm.Generations = rm.Generations[1:]
		for _, k := range []string{KindBackup, KindRsc} {
			_ = os.Remove(filepath.Join(dir, v.fileName(oldest.ID, k)))
		}
	}

	if err := v.persistMetaLocked(); err != nil {
		return err
	}
	return nil
}

func (v *Vault) persistMetaLocked() error {
	plain, err := json.Marshal(v.meta)
	if err != nil {
		return fmt.Errorf("backupvault: encoding the vault index: %w", err)
	}
	sealed, err := v.key.SealDocument(sealInfoPrefix+"meta", plain)
	if err != nil {
		return fmt.Errorf("backupvault: sealing the vault index: %w", err)
	}
	tmp := filepath.Join(v.dir, metaFileName+".tmp")
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return fmt.Errorf("backupvault: writing the vault index: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(v.dir, metaFileName)); err != nil {
		return fmt.Errorf("backupvault: committing the vault index: %w", err)
	}
	return nil
}

// Routers lists every router the vault holds at least one generation
// for, sorted for a stable Settings render.
func (v *Vault) Routers() []string {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	names := make([]string, 0, len(v.meta.Routers))
	for name := range v.meta.Routers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Generations returns device's kept generations, oldest first -- the
// order round 44's strip draws them in, newest at the right.
func (v *Vault) Generations(device string) []Generation {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	rm := v.meta.Routers[device]
	if rm == nil {
		return nil
	}
	out := make([]Generation, 0, len(rm.Generations))
	for _, g := range rm.Generations {
		out = append(out, g.toGeneration())
	}
	return out
}

// Stats is the backups group's "kept" row: pairs (generations, whether
// or not both halves have arrived), the number of routers holding at
// least one, and total bytes across every kept file.
type Stats struct {
	Generations int
	Routers     int
	Bytes       int64
}

func (v *Vault) Stats() Stats {
	if v == nil {
		return Stats{}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	var s Stats
	for _, rm := range v.meta.Routers {
		if len(rm.Generations) == 0 {
			continue
		}
		s.Routers++
		for _, g := range rm.Generations {
			s.Generations++
			s.Bytes += g.BackupSize + g.RscSize
		}
	}
	return s
}

// Open decrypts and returns one generation's file. The caller (the
// download handler) is responsible for the audit entry -- this package
// only ever reads/writes, it never knows who is asking.
func (v *Vault) Open(device, generationID, kind string) ([]byte, error) {
	if v == nil || v.key == nil {
		return nil, ErrDisabled
	}
	if kind != KindBackup && kind != KindRsc {
		return nil, ErrUnknownKind
	}
	v.mu.Lock()
	rm := v.meta.Routers[device]
	var found bool
	if rm != nil {
		for _, g := range rm.Generations {
			if g.ID == generationID {
				found = true
				break
			}
		}
	}
	v.mu.Unlock()
	if !found {
		return nil, ErrNotFound
	}

	path := filepath.Join(v.routerDir(device), v.fileName(generationID, kind))
	sealed, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("backupvault: reading %s: %w", path, err)
	}
	plain, err := v.key.OpenDocument(sealInfoPrefix+device+"/"+generationID+"/"+kind, sealed)
	if err != nil {
		return nil, fmt.Errorf("backupvault: opening %s: %w", path, err)
	}
	return plain, nil
}

// Missed reports what mikroview can say about a router's push schedule
// from the arrivals themselves (owner decision, 2026-09-05: the interval
// is learned, not read off the scheduler line an admin could change).
type Missed struct {
	// IntervalKnown is false until a router has at least two arrivals --
	// one push carries no interval and no missed count (#394's build
	// note).
	IntervalKnown bool
	Interval      time.Duration
	LastArrival   time.Time
	// Count is the number of expected pushes since LastArrival. One
	// missed interval is enough to report (build note): Count >= 1 means
	// amber.
	Count int
}

// Missed computes the interval and missed-push count for device as of
// now. The interval is the median gap between consecutive `.backup`
// arrivals -- median rather than mean so one long gap (a router held
// off, or a generation whose backup upload failed) does not itself
// distort the expectation used to judge every gap after it.
func (v *Vault) Missed(device string, now time.Time) Missed {
	if v == nil {
		return Missed{}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	rm := v.meta.Routers[device]
	if rm == nil {
		return Missed{}
	}
	var arrivals []time.Time
	for _, g := range rm.Generations {
		if !g.BackupArrivedAt.IsZero() {
			arrivals = append(arrivals, g.BackupArrivedAt)
		}
	}
	sort.Slice(arrivals, func(i, j int) bool { return arrivals[i].Before(arrivals[j]) })
	if len(arrivals) == 0 {
		return Missed{}
	}
	last := arrivals[len(arrivals)-1]
	if len(arrivals) < 2 {
		return Missed{LastArrival: last}
	}

	gaps := make([]time.Duration, 0, len(arrivals)-1)
	for i := 1; i < len(arrivals); i++ {
		gaps = append(gaps, arrivals[i].Sub(arrivals[i-1]))
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	interval := medianDuration(gaps)
	if interval <= 0 {
		return Missed{IntervalKnown: true, Interval: interval, LastArrival: last}
	}

	sinceLast := now.Sub(last)
	count := int(sinceLast / interval)
	return Missed{IntervalKnown: true, Interval: interval, LastArrival: last, Count: count}
}

func medianDuration(sorted []time.Duration) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
