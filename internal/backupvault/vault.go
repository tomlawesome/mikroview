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

	"golang.org/x/sys/unix"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
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

// The free-space numbers low-space mode turns on (#1125). Both were
// confirmed by the owner on 2026-09-11.
//
// lowSpaceFloorFiles and lowSpaceFloorPercent make the floor: whichever
// is larger of two whole files' worth of headroom and a twentieth of
// the filesystem. Below it the vault stops growing and starts cycling.
// lowSpaceExitPercent is how far above the floor free space must climb
// before normal retention resumes, so a disk sitting on the line does
// not flap in and out of the mode.
const (
	lowSpaceFloorFiles   = 2 * MaxFileBytes
	lowSpaceFloorPercent = 5
	lowSpaceExitPercent  = 25
)

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
	// Anchor is the generation this router had newest when low-space
	// mode was entered -- the safe copy from before anything was wrong.
	// It is never the one cycled out while the mode lasts, and it is
	// cleared when the mode ends. Persisted so a restart keeps it.
	Anchor string `json:"anchor,omitempty"`
}

// vaultMeta is the whole persisted document, sealed under the retention
// key as a single blob (metaFileName) beside the per-generation files it
// describes.
type vaultMeta struct {
	Routers map[string]*routerMeta `json:"routers"`
	// LowSpace is true while the vault is cycling generations rather
	// than growing the set (#1125). Persisted alongside the anchors it
	// goes with, so a restart in the middle of the trouble does not
	// forget which copies were the safe ones.
	LowSpace bool `json:"lowSpace,omitempty"`
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
	// spaceUnmeasured is true once a free-space check has come back
	// unusable and has been logged, so the next hundred pushes on the
	// same filesystem do not repeat it. Cleared by the first usable
	// measurement.
	spaceUnmeasured bool

	// lockMu guards the optional admin passphrase's state (lock.go). Its
	// own mutex rather than mu: a read path takes mu only to consult the
	// index, then seals or opens outside it, and the lock is consulted in
	// both halves.
	lockMu sync.RWMutex
	lock   *lockState
	// seq disambiguates two generations opened within the same
	// nanosecond -- possible on a fast filesystem or in a test driving
	// the clock by hand.
	seq atomic.Uint64

	// writeFile commits one file; nil means persist.WriteFileAtomic.
	// statfs measures the vault filesystem's free and total bytes; nil
	// means the real unix.Statfs. Both are fields so a test can fail a
	// write or fake a full disk without having to make either happen
	// for real.
	writeFile func(path string, data []byte, perm os.FileMode) error
	statfs    func(dir string) (free, total int64, err error)

	// notifyMu guards onLowSpace, which is set once at wiring time and
	// read on whichever goroutine is storing.
	notifyMu   sync.Mutex
	onLowSpace func(low bool, detail string)
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
	if err := v.loadLock(); err != nil {
		return nil, err
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
	v.reconcile()
	return v, nil
}

// reconcile puts the index and the directories back into agreement at
// start-up, both ways round (#1125). The live paths clean up after
// themselves, but a killed process cannot, so a crash or a failed index
// write can leave either side holding something the other does not.
//
// An index entry whose file is gone is dropped: the vault would
// otherwise list it, offer it and 404 on the download. A file no
// generation refers to is deleted: nothing but this package ever
// reaches into these directories, so it is dead weight -- up to 16MiB
// a time on the disk whose fullness most likely caused it. Both are
// logged; deleting or forgetting a backup silently is not something
// this package does. The repaired index is written once, at the end,
// and only if something changed.
func (v *Vault) reconcile() {
	if v.repairIndexAgainstDisk() {
		if err := v.persistMetaLocked(); err != nil {
			v.log.Error(fmt.Sprintf("could not commit the repaired vault index: %v", err))
		}
	}
	v.removeUnreferencedFiles()
}

// repairIndexAgainstDisk drops generations whose files are not on disk
// and reports whether it changed anything. Only the halves the index
// claims arrived are looked for: a generation whose `.rsc` never came
// is complete as it stands.
func (v *Vault) repairIndexAgainstDisk() bool {
	var changed bool
	for device, rm := range v.meta.Routers {
		dir := v.routerDir(device)
		kept := rm.Generations[:0]
		for _, g := range rm.Generations {
			missing := ""
			for kind, arrived := range map[string]time.Time{KindBackup: g.BackupArrivedAt, KindRsc: g.RscArrivedAt} {
				if arrived.IsZero() {
					continue
				}
				if _, err := os.Stat(filepath.Join(dir, v.fileName(g.ID, kind))); err != nil {
					missing = kind
				}
			}
			if missing == "" {
				kept = append(kept, g)
				continue
			}
			changed = true
			v.log.Warn(fmt.Sprintf("repaired the vault index: dropped %s's generation %s, whose %s file is not on disk",
				device, g.ID, missing))
		}
		rm.Generations = kept
		if len(rm.Generations) == 0 {
			// Nothing left to hold: the router should not appear in
			// Settings' list with an empty strip.
			delete(v.meta.Routers, device)
			changed = true
			continue
		}
		if rm.Anchor != "" && !generationsHave(rm.Generations, rm.Anchor) {
			// The anchor went with a dropped generation. The newest
			// copy the vault still has becomes the one low-space mode
			// cycles around.
			rm.Anchor = rm.Generations[len(rm.Generations)-1].ID
			changed = true
		}
	}
	return changed
}

func generationsHave(gens []*generationMeta, id string) bool {
	for _, g := range gens {
		if g.ID == id {
			return true
		}
	}
	return false
}

// removeUnreferencedFiles is reconcile's other half: every file under a
// router directory that no generation in the (already repaired) index
// refers to, including half-written temp files from a crashed
// persist.WriteFileAtomic.
func (v *Vault) removeUnreferencedFiles() {
	referenced := make(map[string]map[string]bool, len(v.meta.Routers))
	for device, rm := range v.meta.Routers {
		names := make(map[string]bool, 2*len(rm.Generations))
		for _, g := range rm.Generations {
			names[v.fileName(g.ID, KindBackup)] = true
			names[v.fileName(g.ID, KindRsc)] = true
		}
		referenced[dirNameFor(device)] = names
	}
	entries, err := os.ReadDir(v.dir)
	if err != nil {
		v.log.Warn(fmt.Sprintf("could not read %s to check for unreferenced files: %v", v.dir, err))
		return
	}
	for _, entry := range entries {
		// Only the per-router directories: metaFileName and
		// lockFileName are files, and sit beside them.
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(v.dir, entry.Name())
		// A directory with no entry in the index at all belongs to a
		// router the vault no longer holds anything for: all of it is
		// unreferenced.
		names := referenced[entry.Name()]
		files, err := os.ReadDir(dir)
		if err != nil {
			v.log.Warn(fmt.Sprintf("could not read %s to check for unreferenced files: %v", dir, err))
			continue
		}
		for _, f := range files {
			if f.IsDir() || names[f.Name()] {
				continue
			}
			path := filepath.Join(dir, f.Name())
			if err := os.Remove(path); err != nil {
				v.log.Warn(fmt.Sprintf("could not remove the unreferenced file %s: %v", path, err))
				continue
			}
			v.log.Info(fmt.Sprintf("removed %s: no generation in the vault index refers to it", path))
		}
	}
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
//
// Store never refuses an arrival because the disk is filling up (owner
// ruling, #1125): when free space falls below lowSpaceFloor the vault
// enters low-space mode and each new arrival replaces the oldest
// ordinary generation instead of adding one, so the router's data keeps
// two guaranteed points -- the anchor from before the trouble, and the
// latest. The refusals above are about the file itself (no key, over
// the cap, not a backup), which is a different thing entirely.
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

	// Measured before the lock: statfs is a syscall against a
	// filesystem that may already be in trouble, and every reader of the
	// index would queue behind it (#1125).
	space := v.measureSpace()
	v.mu.Lock()
	change, err := v.storeLocked(device, kind, header, data, now, space)
	v.mu.Unlock()
	if change != nil {
		v.notifyLowSpace(change.low, change.detail)
	}
	return err
}

// lowSpaceChange is one crossing of the free-space floor, reported to
// the caller's callback once the vault's own lock is released.
type lowSpaceChange struct {
	low    bool
	detail string
}

// storeLocked is Store's body with v.mu held. It commits the file
// before it touches the index (#1125): a write that fails leaves the
// index exactly as it was, rather than keeping a generation with no
// file behind it that the next successful push would persist, count
// towards MaxGenerations and 404 on download.
func (v *Vault) storeLocked(device, kind string, header HeaderLabel, data []byte, now time.Time, space spaceReading) (*lowSpaceChange, error) {
	change := v.updateSpaceModeLocked(space)
	// dirty tracks whether the index has already changed independently
	// of this arrival -- the mode flip, or a generation cycled out to
	// make room -- so a failed write still persists what did happen.
	dirty := change != nil

	rm := v.meta.Routers[device]
	if rm == nil {
		rm = &routerMeta{}
	}

	// Work out what this arrival is without changing anything yet.
	var (
		newGen *generationMeta // a generation to add, nil when completing one
		attach *generationMeta // the open generation this `.rsc` completes
	)
	switch kind {
	case KindBackup:
		newGen = &generationMeta{ID: v.nextGenerationID(now), BackupArrivedAt: now, BackupSize: int64(len(data)), Header: header}
	case KindRsc:
		if n := len(rm.Generations); n > 0 && rm.Generations[n-1].RscArrivedAt.IsZero() {
			attach = rm.Generations[n-1]
		} else {
			newGen = &generationMeta{ID: v.nextGenerationID(now)}
		}
	}
	gen := newGen
	if gen == nil {
		gen = attach
	}

	// fail reports err after persisting whatever the vault itself
	// changed. The arrival is not in the index either way, so nothing
	// phantom is kept. The router does hear about it -- the error
	// reaches it as a 503 -- but as a fault on mikroview's side, which
	// is what it is. What the router is never told is that its backup
	// was refused for want of space: a disk this full is mikroview's
	// problem, not the router's (#1125).
	fail := func(err error) (*lowSpaceChange, error) {
		v.log.Error(fmt.Sprintf("could not keep %s's %s: %v", device, kind, err))
		if dirty {
			if perr := v.persistMetaLocked(); perr != nil {
				v.log.Error(fmt.Sprintf("could not commit the vault index after a failed write: %v", perr))
			}
		}
		return change, err
	}

	sealed, err := v.sealBody(sealInfoPrefix+device+"/"+gen.ID+"/"+kind, data)
	if err != nil {
		return fail(fmt.Errorf("backupvault: sealing %s: %w", kind, err))
	}
	dir := v.routerDir(device)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(fmt.Errorf("backupvault: creating %s: %w", dir, err))
	}

	// In low-space mode the set does not grow: the arrival replaces the
	// oldest ordinary generation. The anchor is never the one dropped,
	// so every router keeps the safe copy from before the trouble as
	// well as the latest. If the anchor is all that is left the arrival
	// is written beside it -- two files per router is the floor, and
	// the disk is still not the vault's to refuse.
	//
	// The replacement is written before the old generation is dropped.
	// The other order destroys a generation and then discovers it
	// cannot store the new one, so a run of failing pushes erodes every
	// router to its anchor. persist.WriteFileAtomic writes through a
	// temp file in this same directory, so the room the old generation
	// would free is not available to it either way.
	replacing := v.meta.LowSpace && newGen != nil
	path := filepath.Join(dir, v.fileName(gen.ID, kind))
	err = v.write(path, sealed, 0o600)
	if err != nil && replacing && errors.Is(err, unix.ENOSPC) {
		// Last resort, and only here: the disk genuinely has no room
		// for the new file beside the old one, so the only way to keep
		// the router storing anything at all is to free the old one
		// first and try again. This is the case that can cost a
		// generation for nothing, which is why it needs the filesystem
		// itself to have said ENOSPC rather than any write failure.
		if dropped := v.dropOldestNonAnchorLocked(rm, dir); dropped != nil {
			dirty = true
			replacing = false
			v.log.Warn(fmt.Sprintf("no space for %s's %s beside its oldest generation: dropped %s and retried",
				device, newGen.ID, dropped.ID))
			err = v.write(path, sealed, 0o600)
		}
	}
	if err != nil {
		return fail(fmt.Errorf("backupvault: writing %s: %w", path, err))
	}

	// The file is on disk: now the generation it replaces can go.
	if replacing {
		if dropped := v.dropOldestNonAnchorLocked(rm, dir); dropped != nil {
			dirty = true
			v.log.Info(fmt.Sprintf("low on disk space: dropped %s's generation %s now that %s is written",
				device, dropped.ID, newGen.ID))
		}
	}

	// The file is committed: only now does the index learn about it.
	// prevAnchor and hadRouter are what a failed index write rolls back
	// to, below.
	prevAnchor := rm.Anchor
	_, hadRouter := v.meta.Routers[device]
	if newGen != nil {
		rm.Generations = append(rm.Generations, newGen)
	} else {
		attach.RscArrivedAt = now
		attach.RscSize = int64(len(data))
	}
	v.meta.Routers[device] = rm

	// A router that first appears while the mode is on has no anchor
	// from before the trouble, so its first kept generation becomes
	// one: it is the copy every later arrival is cycled around, and
	// without it the router would never have a second guaranteed point.
	if v.meta.LowSpace && rm.Anchor == "" && len(rm.Generations) > 0 {
		rm.Anchor = rm.Generations[len(rm.Generations)-1].ID
	}

	// Evict the oldest generations beyond the cap. Their files are
	// deleted outright -- there is no undo, matching "the eleventh pair
	// lets the oldest go" (round 44). In low-space mode the set never
	// grows, so this does not run and cannot reach an anchor.
	if !v.meta.LowSpace {
		for len(rm.Generations) > MaxGenerations {
			oldest := rm.Generations[0]
			rm.Generations = rm.Generations[1:]
			for _, k := range []string{KindBackup, KindRsc} {
				_ = os.Remove(filepath.Join(dir, v.fileName(oldest.ID, k)))
			}
		}
	}

	if err := v.persistMetaLocked(); err != nil {
		// The index write is the likely failure on a disk this full,
		// and the file it would have named is now referenced by
		// nothing on disk or in memory -- up to 16MiB of dead weight
		// per push (#1125). Take the arrival back out and remove it.
		if newGen != nil {
			rm.Generations = rm.Generations[:len(rm.Generations)-1]
		} else if attach != nil {
			attach.RscArrivedAt = time.Time{}
			attach.RscSize = 0
		}
		rm.Anchor = prevAnchor
		if !hadRouter && len(rm.Generations) == 0 {
			delete(v.meta.Routers, device)
		}
		if rerr := os.Remove(path); rerr != nil && !os.IsNotExist(rerr) {
			v.log.Error(fmt.Sprintf("could not remove %s after the vault index failed to commit: %v", path, rerr))
		}
		return change, err
	}
	return change, nil
}

// dropOldestNonAnchorLocked deletes the oldest generation that is not
// this router's anchor -- files first, then the index entry -- and
// returns it. nil means there was nothing to drop: the anchor is all
// the router has left.
func (v *Vault) dropOldestNonAnchorLocked(rm *routerMeta, dir string) *generationMeta {
	for i, g := range rm.Generations {
		if g.ID == rm.Anchor {
			continue
		}
		for _, k := range []string{KindBackup, KindRsc} {
			_ = os.Remove(filepath.Join(dir, v.fileName(g.ID, k)))
		}
		rm.Generations = append(rm.Generations[:i:i], rm.Generations[i+1:]...)
		return g
	}
	return nil
}

// LowSpace reports whether the vault is cycling generations because the
// filesystem it lives on is nearly full (#1125). The API surfaces it as
// `lowSpace` on GET /api/router-backups so Settings can warn.
func (v *Vault) LowSpace() bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.meta.LowSpace
}

// OnLowSpaceChange registers the callback fired when the vault enters
// or leaves low-space mode. The vault has no audit log of its own -- it
// never knows who is asking, and says so of downloads too -- so the
// caller that does holds the accountability entry. The callback runs on
// the storing goroutine with no vault lock held, so it may call back
// into the vault.
func (v *Vault) OnLowSpaceChange(fn func(low bool, detail string)) {
	if v == nil {
		return
	}
	v.notifyMu.Lock()
	defer v.notifyMu.Unlock()
	v.onLowSpace = fn
}

func (v *Vault) notifyLowSpace(low bool, detail string) {
	v.notifyMu.Lock()
	fn := v.onLowSpace
	v.notifyMu.Unlock()
	if fn != nil {
		fn(low, detail)
	}
}

// write commits one file, through the field so a test can fail a write
// without filling a disk.
func (v *Vault) write(path string, data []byte, perm os.FileMode) error {
	if v.writeFile != nil {
		return v.writeFile(path, data, perm)
	}
	return persist.WriteFileAtomic(path, data, perm)
}

// SetSpaceProbeForTest replaces the free-space measurement with one a
// test controls. It exists because low-space mode (#1125) cannot be
// driven from outside this package any other way -- no test may fill a
// real filesystem -- and the flag it sets is reported by
// internal/api's router-backups list, which has its own tests to write.
// Production code never calls this, and it must be called before the
// vault is handed to anything that stores: the probe is read without a
// lock, exactly as the in-package tests set it.
func (v *Vault) SetSpaceProbeForTest(measure func(dir string) (free, total int64, err error)) {
	if v == nil {
		return
	}
	v.statfs = measure
}

// statfsBytes is the real free/total measurement of the filesystem dir
// lives on. Bavail rather than Bfree: the blocks an unprivileged
// process may actually use, which is what the vault has.
func statfsBytes(dir string) (free, total int64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize), nil
}

// lowSpaceFloor is the free-space level the mode turns on below.
func lowSpaceFloor(total int64) int64 {
	floor := int64(lowSpaceFloorFiles)
	if share := total / 100 * lowSpaceFloorPercent; share > floor {
		floor = share
	}
	return floor
}

// spaceReading is one free-space measurement of the vault's
// filesystem, taken before v.mu so a slow or sick filesystem does not
// hold up every reader of the index (#1125).
type spaceReading struct {
	free  int64
	total int64
	err   error
}

// measureSpace reads the filesystem the vault lives on. No lock is
// held: the probe field is set at wiring time and never afterwards.
func (v *Vault) measureSpace() spaceReading {
	measure := v.statfs
	if measure == nil {
		measure = statfsBytes
	}
	free, total, err := measure(v.dir)
	return spaceReading{free: free, total: total, err: err}
}

// updateSpaceModeLocked enters or leaves low-space mode on the strength
// of a measurement Store already took. A measurement that says nothing
// changes nothing: the vault carries on as it was, because a backup is
// never lost over the vault's own inability to read a number.
func (v *Vault) updateSpaceModeLocked(space spaceReading) *lowSpaceChange {
	free, total := space.free, space.total
	switch {
	case space.err != nil:
		v.warnUnmeasuredLocked(fmt.Sprintf("could not measure free space on %s: %v -- carrying on unchanged", v.dir, space.err))
		return nil
	case total <= 0:
		// A filesystem claiming no blocks at all has not told us it is
		// full, it has told us nothing. Believing it would put the
		// vault in low-space mode for good: free=0 is under every
		// floor, and it can never climb over the exit margin again.
		v.warnUnmeasuredLocked(fmt.Sprintf("free-space check on %s reported a total of zero bytes -- carrying on unchanged", v.dir))
		return nil
	}
	v.spaceUnmeasured = false
	floor := lowSpaceFloor(total)
	detail := fmt.Sprintf("free=%d floor=%d total=%d", free, floor, total)
	switch {
	case !v.meta.LowSpace && free < floor:
		v.meta.LowSpace = true
		// The anchor is each router's newest generation as things
		// stand. Every generation in the index has its file on disk --
		// the index is only written after the file is -- so the newest
		// one is a fully written copy from before the trouble.
		for _, rm := range v.meta.Routers {
			if n := len(rm.Generations); n > 0 {
				rm.Anchor = rm.Generations[n-1].ID
			}
		}
		v.log.Warn("low on disk space: keeping each router's newest generation and cycling the rest -- " + detail)
		return &lowSpaceChange{low: true, detail: detail}
	case v.meta.LowSpace && free >= floor+floor/100*lowSpaceExitPercent:
		v.meta.LowSpace = false
		for _, rm := range v.meta.Routers {
			rm.Anchor = ""
		}
		v.log.Info("free space recovered: normal retention resumes -- " + detail)
		return &lowSpaceChange{low: false, detail: detail}
	}
	return nil
}

// warnUnmeasuredLocked logs the first unusable measurement and stays
// quiet about those after it: on a filesystem that always answers this
// way, every push would otherwise repeat the same line.
func (v *Vault) warnUnmeasuredLocked(msg string) {
	if v.spaceUnmeasured {
		return
	}
	v.spaceUnmeasured = true
	v.log.Warn(msg)
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
	if err := v.write(filepath.Join(v.dir, metaFileName), sealed, 0o600); err != nil {
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
	plain, err := v.openBody(sealInfoPrefix+device+"/"+generationID+"/"+kind, sealed)
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
