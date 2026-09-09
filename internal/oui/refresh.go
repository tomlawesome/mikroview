// SPDX-License-Identifier: AGPL-3.0-only

package oui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Refresh fetches the registry and replaces the table.
//
// Every failure path leaves the previous table in place -- fail to
// last-known-good, never to empty -- which is the same contract
// internal/blocklist and internal/netclass state, and matters more
// here: an operator mid-identification should not have vendor names
// vanish because IEEE was briefly unreachable.
func (r *Registry) Refresh(ctx context.Context) {
	if !r.Enabled() {
		return
	}
	body, etag, notModified, err := r.client.fetch(ctx, r)
	if err != nil {
		r.log.Warn(fmt.Sprintf("IEEE MA-L registry: refresh failed (%v) -- keeping the last good data", err))
		return
	}

	now := time.Now()
	if notModified {
		// Unchanged since the last fetch: the data is confirmed
		// current, so the fetch time moves forward even though nothing
		// was downloaded. Without this a registry that IEEE has not
		// touched for a month would read as a month stale, which is
		// the opposite of the truth.
		r.mu.Lock()
		r.fetchedAt = now
		r.fromCache = false
		r.mu.Unlock()
		r.saveCache()
		r.log.Info(describe(r.entryCount(), "unchanged since the last fetch"))
		return
	}

	if !looksLikeCSV(body) {
		r.log.Warn("IEEE MA-L registry: the response does not look like the registry CSV (a portal or error page?) -- keeping the last good data")
		return
	}
	entries, err := parseCSV(body)
	if err != nil {
		r.log.Warn(fmt.Sprintf("IEEE MA-L registry: parse failed (%v) -- keeping the last good data", err))
		return
	}

	// A registry that suddenly halves is a truncated download or a
	// substituted body, not IEEE deleting twenty thousand assignments:
	// assignments are added, essentially never removed. Refusing the
	// shrink costs at most one day of new prefixes and keeps a
	// half-downloaded file from quietly becoming the answer to every
	// lookup. Same reasoning as netclass's coverage-delta guard.
	if prev := r.entryCount(); prev > 0 && len(entries) < prev/2 {
		r.log.Warn(fmt.Sprintf("IEEE MA-L registry: refusing a suspiciously small update (%d prefixes, was %d) -- keeping the last good data", len(entries), prev))
		return
	}

	r.mu.Lock()
	r.entries = entries
	r.etag = etag
	r.fetchedAt = now
	r.fromCache = false
	r.mu.Unlock()

	r.saveCache()
	r.log.Info(describe(len(entries), "refreshed"))
}

// cacheFile is the on-disk shape. The parsed prefix->organisation map
// is stored rather than the raw CSV: it is smaller, it is what a
// restart actually needs, and re-parsing four megabytes of CSV on every
// start to rebuild the same map is work nobody asked for.
//
// Source is recorded so a cache written for one feed URL is never
// served for another -- an operator who repoints the URL at their own
// mirror gets that mirror's data, not the previous source's.
type cacheFile struct {
	Source    string            `json:"source"`
	ETag      string            `json:"etag,omitempty"`
	FetchedAt time.Time         `json:"fetchedAt"`
	Entries   map[string]string `json:"entries"`
}

// loadCache reads a previously-fetched registry off disk, so vendor
// names are available from the first request after a restart rather
// than after the first refresh completes. Every failure is silent-ish:
// an absent, unreadable or foreign cache simply leaves the registry
// empty, which is the ordinary "no vendor data yet" state and not an
// error worth failing a start over.
//
// This deliberately does not go through internal/persist: this file is
// a cache of someone else's public data, not mikroview's own state --
// it must not be swept into an encrypted store, a Postgres row or a
// backup, all of which would amount to keeping a copy of the registry
// in places the no-vendoring reasoning above says it should not be.
func (r *Registry) loadCache() {
	if r.cachePath == "" {
		return
	}
	data, err := os.ReadFile(r.cachePath)
	if err != nil {
		if !os.IsNotExist(err) {
			r.log.Warn(fmt.Sprintf("IEEE MA-L registry: cache at %s unreadable (%v) -- starting with no vendor data", r.cachePath, err))
		}
		return
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		r.log.Warn(fmt.Sprintf("IEEE MA-L registry: cache at %s is not readable JSON (%v) -- starting with no vendor data", r.cachePath, err))
		return
	}
	if cf.Source != r.url || len(cf.Entries) == 0 {
		return
	}
	r.mu.Lock()
	r.entries = cf.Entries
	r.etag = cf.ETag
	r.fetchedAt = cf.FetchedAt
	r.fromCache = true
	r.mu.Unlock()
	r.log.Info(describe(len(cf.Entries), "loaded from the local cache"))
}

// saveCache writes the current table atomically (temp file in the same
// directory, 0600, rename) so a crash mid-write cannot leave a
// half-written cache that the next start would read as a short
// registry. A write failure is logged and otherwise ignored: the
// running instance still has its table, and the only cost is a
// re-download next start.
func (r *Registry) saveCache() {
	if r.cachePath == "" {
		return
	}
	r.mu.RLock()
	cf := cacheFile{Source: r.url, ETag: r.etag, FetchedAt: r.fetchedAt, Entries: r.entries}
	r.mu.RUnlock()
	if len(cf.Entries) == 0 {
		return
	}

	dir := filepath.Dir(r.cachePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		r.log.Warn(fmt.Sprintf("IEEE MA-L registry: cannot create %s (%v) -- the registry will be re-downloaded on the next start", dir, err))
		return
	}
	tmp, err := os.CreateTemp(dir, ".oui-cache-*")
	if err != nil {
		r.log.Warn(fmt.Sprintf("IEEE MA-L registry: cannot write the cache (%v) -- the registry will be re-downloaded on the next start", err))
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below succeeds

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		r.log.Warn(fmt.Sprintf("IEEE MA-L registry: cannot set cache permissions (%v) -- cache not written", err))
		return
	}
	if err := json.NewEncoder(tmp).Encode(cf); err != nil {
		tmp.Close()
		r.log.Warn(fmt.Sprintf("IEEE MA-L registry: cannot encode the cache (%v) -- cache not written", err))
		return
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		r.log.Warn(fmt.Sprintf("IEEE MA-L registry: cannot flush the cache (%v) -- cache not written", err))
		return
	}
	if err := tmp.Close(); err != nil {
		r.log.Warn(fmt.Sprintf("IEEE MA-L registry: cannot close the cache (%v) -- cache not written", err))
		return
	}
	if err := os.Rename(tmpName, r.cachePath); err != nil {
		r.log.Warn(fmt.Sprintf("IEEE MA-L registry: cannot replace the cache (%v) -- cache not written", err))
	}
}
