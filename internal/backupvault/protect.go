// SPDX-License-Identifier: AGPL-3.0-only

package backupvault

// The protected pool (#1126, owner ruling 2026-09-11). An admin marks a
// generation as one to hold on to and says why -- "before the 7.16
// upgrade", "last config before the office move" -- and it moves out of
// the ten the vault cycles into a pool of its own, per router, with no
// limit on how many it holds.
//
// Moving it out of Generations rather than flagging it in place is what
// makes every promise true at once, with no rule anywhere else needing
// to learn about the flag: MaxGenerations counts what is in
// Generations, low-space cycling drops from Generations, and both
// halves of reconcile read both lists. Unprotecting puts it back where
// the cap applies to it again, and the oldest may then go, which is
// the sentence the release control on screen says out loud.
//
// The comment lives in the sealed index like everything else here. It
// is an operator's note about their own network, so it never reaches a
// log line or an audit detail: the audit entry names the generation,
// and whoever may read the comment may read the backup itself.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxCommentRunes is how long a kept backup's comment may be. Counted
// in runes, the way MinPassphraseRunes is, so the limit means the same
// thing whatever alphabet it is written in.
const MaxCommentRunes = 120

var (
	// ErrBadComment is a refusal reason: a kept backup's comment is
	// missing, too long, or carries control characters. Saying why is
	// the point of keeping one, so an empty comment is refused rather
	// than stored (#1126).
	ErrBadComment = fmt.Errorf("backupvault: a kept backup needs a comment of 1 to %d characters saying why", MaxCommentRunes)
	// ErrAlreadyProtected reports that the generation is already in
	// this router's kept pool.
	ErrAlreadyProtected = errors.New("backupvault: that generation is already kept")
)

// checkComment validates and normalises one comment.
func checkComment(comment string) (string, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" || utf8.RuneCountInString(comment) > MaxCommentRunes {
		return "", ErrBadComment
	}
	for _, r := range comment {
		// Control characters are refused rather than stripped: this
		// line is rendered as a single line beside the date and size,
		// and a newline or an escape sequence in it is either a
		// mistake or an attempt at one.
		if unicode.IsControl(r) {
			return "", ErrBadComment
		}
	}
	return comment, nil
}

// Protect moves one of device's generations into its kept pool, with
// the admin's comment saying why. by is who asked, recorded beside the
// comment; now is passed in for the same reason Store takes it.
//
// The generation stops counting towards MaxGenerations from this
// moment: nothing else in the vault can evict it, and there is no
// limit on how many a router keeps.
func (v *Vault) Protect(device, generationID, comment, by string, now time.Time) error {
	if v == nil || v.key == nil {
		return ErrDisabled
	}
	comment, err := checkComment(comment)
	if err != nil {
		return err
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	rm := v.meta.Routers[device]
	if rm == nil {
		return ErrNotFound
	}
	if generationsHave(rm.Protected, generationID) {
		return ErrAlreadyProtected
	}
	idx := -1
	for i, g := range rm.Generations {
		if g.ID == generationID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}

	gen := rm.Generations[idx]
	prevGenerations := rm.Generations
	prevAnchor := rm.Anchor
	rm.Generations = append(rm.Generations[:idx:idx], rm.Generations[idx+1:]...)
	// The anchor is the newest of the *cycling* copies from before the
	// trouble (#1125). A generation that has left the cycling set is
	// not one low-space mode can be asked to spare, so the anchor
	// moves to whatever is newest there now.
	if rm.Anchor == generationID {
		rm.Anchor = ""
		if n := len(rm.Generations); n > 0 {
			rm.Anchor = rm.Generations[n-1].ID
		}
	}
	gen.Comment = comment
	gen.ProtectedAt = now
	gen.ProtectedBy = by
	rm.Protected = insertGeneration(rm.Protected, gen)

	if err := v.persistMetaLocked(); err != nil {
		rm.Generations = prevGenerations
		rm.Anchor = prevAnchor
		rm.Protected = removeGeneration(rm.Protected, generationID)
		gen.Comment, gen.ProtectedAt, gen.ProtectedBy = "", time.Time{}, ""
		return err
	}
	// The id, not the comment: an audit entry and a log line are read
	// by whoever can read the server's logs, which is not the same set
	// of people as those who may open this vault.
	v.log.Info(fmt.Sprintf("keeping %s's generation %s out of the cycling set at %s's request", device, generationID, by))
	return nil
}

// Unprotect puts a kept generation back into the cycling set, in its
// oldest-first place by id, and drops its comment. The set may now be
// over MaxGenerations, in which case the oldest goes as it always
// does -- which is what releasing one means.
func (v *Vault) Unprotect(device, generationID string) error {
	if v == nil || v.key == nil {
		return ErrDisabled
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	rm := v.meta.Routers[device]
	if rm == nil {
		return ErrNotFound
	}
	idx := -1
	for i, g := range rm.Protected {
		if g.ID == generationID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}

	gen := rm.Protected[idx]
	prevProtected := rm.Protected
	prevGenerations := rm.Generations
	comment, protectedAt, protectedBy := gen.Comment, gen.ProtectedAt, gen.ProtectedBy
	rm.Protected = append(rm.Protected[:idx:idx], rm.Protected[idx+1:]...)
	gen.Comment, gen.ProtectedAt, gen.ProtectedBy = "", time.Time{}, ""

	// Over the cap now that gen rejoins the cycling set, possibly
	// because it was already full. Eviction candidates are drawn only
	// from prevGenerations -- what was already cycling *before* this
	// release -- never from gen itself, no matter where its id sorts
	// once it rejoins: a kept backup is exactly the kind that tends to
	// be old ("before the 7.16 upgrade"), so picking the victim after
	// gen had already rejoined the set could -- and did -- pick gen,
	// deleting the very backup the release just put back (v0.6.0
	// pre-release audit). Releasing must never delete the thing being
	// released.
	//
	// The index is written before the files go, the opposite order to
	// storeLocked's: there the file has to exist before the index can
	// name it, and here it has to stop being named before it can be
	// deleted, so a crash in between leaves an unreferenced file that
	// reconcile sweeps rather than an index entry pointing at nothing.
	var evicted []*generationMeta
	retained := prevGenerations
	if !v.meta.LowSpace {
		for len(retained) >= MaxGenerations {
			evicted = append(evicted, retained[0])
			retained = retained[1:]
		}
	}
	rm.Generations = insertGeneration(retained, gen)

	if err := v.persistMetaLocked(); err != nil {
		rm.Protected = prevProtected
		rm.Generations = prevGenerations
		gen.Comment, gen.ProtectedAt, gen.ProtectedBy = comment, protectedAt, protectedBy
		return err
	}
	dir := v.dirFor(device, rm)
	for _, g := range evicted {
		for _, k := range []string{KindBackup, KindRsc} {
			_ = os.Remove(filepath.Join(dir, v.fileName(g.ID, k)))
		}
		v.log.Info(fmt.Sprintf("released %s's generation %s back into the cycling set: dropped %s, which was the oldest",
			device, generationID, g.ID))
	}
	if len(evicted) == 0 {
		v.log.Info(fmt.Sprintf("released %s's generation %s back into the cycling set", device, generationID))
	}
	return nil
}

// SetComment rewrites a kept generation's comment. Only a generation
// already in the pool has one: the comment and the keeping are the
// same decision, so there is nothing here to attach a note to a
// cycling generation with.
func (v *Vault) SetComment(device, generationID, comment string) error {
	if v == nil || v.key == nil {
		return ErrDisabled
	}
	comment, err := checkComment(comment)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	rm := v.meta.Routers[device]
	if rm == nil {
		return ErrNotFound
	}
	for _, g := range rm.Protected {
		if g.ID != generationID {
			continue
		}
		previous := g.Comment
		g.Comment = comment
		if err := v.persistMetaLocked(); err != nil {
			g.Comment = previous
			return err
		}
		return nil
	}
	return ErrNotFound
}

// ProtectedGenerations returns device's kept pool, oldest first -- the
// same order Generations uses, which the list reverses for the screen.
func (v *Vault) ProtectedGenerations(device string) []Generation {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	rm := v.meta.Routers[device]
	if rm == nil {
		return nil
	}
	out := make([]Generation, 0, len(rm.Protected))
	for _, g := range rm.Protected {
		out = append(out, g.toGeneration())
	}
	return out
}

// insertGeneration puts gen into an oldest-first list in its place by
// id. Ids are minted from the arrival time and sort in arrival order
// (nextGenerationID), so this is the same ordering both lists are
// already kept in, restored rather than re-derived.
// A fresh slice rather than an append in place: the caller keeps the
// list it started with to put back if the index write fails, and
// sorting a slice that shares an array with that one would reorder it
// underneath the rollback.
func insertGeneration(gens []*generationMeta, gen *generationMeta) []*generationMeta {
	out := make([]*generationMeta, 0, len(gens)+1)
	out = append(out, gens...)
	out = append(out, gen)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// removeGeneration drops the entry with this id, if it is there.
func removeGeneration(gens []*generationMeta, id string) []*generationMeta {
	for i, g := range gens {
		if g.ID == id {
			return append(gens[:i:i], gens[i+1:]...)
		}
	}
	return gens
}
