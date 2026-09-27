// SPDX-License-Identifier: AGPL-3.0-only

package geoip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

const (
	// keyedInterval is IPinfo's and MaxMind's cadence: both publish
	// daily, and the poll is conditional where the server allows it.
	keyedInterval = 24 * time.Hour
	// retryAfter is how soon a failed fetch is tried again -- sooner
	// than a day, so a brief outage does not cost a day's freshness.
	retryAfter = time.Hour
	// minWait keeps the loop from spinning when a schedule lands in the
	// past.
	minWait = time.Minute
)

// Run is the refresh loop: it sleeps firstDelay (main.go jitters it,
// like the OUI feed's), then fetches every source that is due, and again
// whenever the earliest next refresh arrives or a key change calls Kick.
// Returns when ctx ends.
func (m *Manager) Run(ctx context.Context, firstDelay time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	first := m.now().Add(firstDelay)
	for _, s := range m.sources {
		if s.nextRefresh.IsZero() || s.nextRefresh.Before(first) {
			s.nextRefresh = first
		}
	}
	m.mu.Unlock()

	timer := time.NewTimer(firstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-m.kick:
		}
		m.RefreshDue(ctx)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(m.untilNext())
	}
}

// untilNext is how long until the earliest scheduled refresh.
func (m *Manager) untilNext() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := m.now()
	wait := keyedInterval
	for _, s := range m.sources {
		if s.id != DBIP && !s.keySet {
			continue
		}
		if d := s.nextRefresh.Sub(now); d < wait {
			wait = d
		}
	}
	if wait < minWait {
		wait = minWait
	}
	return wait
}

// RefreshDue fetches every source whose refresh is due or forced.
func (m *Manager) RefreshDue(ctx context.Context) {
	for _, id := range precedence {
		m.mu.RLock()
		s := m.sources[id]
		due := s.force || !m.now().Before(s.nextRefresh)
		usable := id == DBIP || (s.keySet && !s.cred.empty())
		m.mu.RUnlock()
		if due && usable {
			m.refresh(ctx, id)
		}
	}
}

// refresh fetches one source and, if a new file arrived and opens as an
// MMDB, swaps it in. Every failure keeps the last good file -- fail to
// last-known-good, never to empty, the contract internal/oui states.
func (m *Manager) refresh(ctx context.Context, id SourceID) {
	switch id {
	case DBIP:
		m.refreshDBIP(ctx)
	default:
		m.refreshKeyed(ctx, id)
	}
	m.warnIfStale(id)
}

func monthOf(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// refreshDBIP gets this month's DB-IP file, or last month's while this
// month's is not yet published (a 404 early in the month). A file for
// the current month is final, so once it is loaded nothing is fetched
// until the next month begins; until then it is tried daily.
func (m *Manager) refreshDBIP(ctx context.Context) {
	now := m.now()
	cur := monthOf(now)
	nextMonth := cur.AddDate(0, 1, 0)
	curKey := cur.Format("2006-01")
	prevKey := cur.AddDate(0, -1, 0).Format("2006-01")

	m.mu.Lock()
	s := m.sources[DBIP]
	s.force = false
	loadedMonth := s.month
	have := s.reader != nil
	etag, modified, fetchedURL := s.etag, s.lastModified, s.fetchedURL
	m.mu.Unlock()

	if have && loadedMonth == curKey {
		m.schedule(DBIP, nextMonth)
		return
	}

	for _, month := range []string{curKey, prevKey} {
		if have && month == loadedMonth {
			// Already holding last month's file and this month's is
			// not out yet: nothing to download, try again tomorrow.
			m.schedule(DBIP, now.Add(keyedInterval))
			return
		}
		u := m.eps.dbip(month)
		req := request{url: u, kind: maybeGzip}
		if redactURL(u) == fetchedURL {
			req.etag, req.modified = etag, modified
		}
		res, err := m.fetchInto(ctx, DBIP, req, month, 0)
		if err != nil {
			m.fail(DBIP, err, retryAfter)
			return
		}
		if res.notFound {
			continue
		}
		next := nextMonth
		if month != curKey {
			next = now.Add(keyedInterval)
		}
		m.schedule(DBIP, next)
		return
	}
	m.fail(DBIP, errors.New("neither this month's nor last month's file is published (404)"), retryAfter)
}

// refreshKeyed fetches IPinfo's or MaxMind's file with the stored key.
func (m *Manager) refreshKeyed(ctx context.Context, id SourceID) {
	m.mu.Lock()
	s := m.sources[id]
	s.force = false
	cred, gen := s.cred, s.keyGen
	etag, modified := s.etag, s.lastModified
	m.mu.Unlock()
	if cred.empty() {
		return
	}

	req := request{etag: etag, modified: modified, secrets: cred.secrets()}
	switch id {
	case IPinfo:
		req.url = m.eps.ipinfo + "?" + url.Values{"token": {cred.Token}}.Encode()
		req.kind = maybeGzip
	case MaxMind:
		req.url = m.eps.maxmind
		req.user, req.pass = cred.AccountID, cred.LicenseKey
		req.kind = tarGzip
	}

	res, err := m.fetchInto(ctx, id, req, "", gen)
	now := m.now()
	switch {
	case err != nil && res.unauthorized:
		// A refused key will not start working on its own: wait a
		// full day (or for the key to change) rather than hammering
		// the provider hourly with it.
		m.fail(id, fmt.Errorf("%w -- the provider refused the key; check it and enter it again", err), keyedInterval)
	case err != nil:
		m.fail(id, err, retryAfter)
	case res.notFound:
		m.fail(id, errors.New("the download is not there (404)"), retryAfter)
	default:
		m.schedule(id, now.Add(keyedInterval))
	}
}

// fetchInto downloads req and installs the result. gen is the key
// generation the fetch started under (0 for DB-IP); a result that
// arrives after the key changed is discarded.
func (m *Manager) fetchInto(ctx context.Context, id SourceID, req request, month string, gen uint64) (response, error) {
	var (
		buf     *bytes.Buffer
		tmp     *os.File
		tmpName string
		dst     io.Writer
	)
	if m.cacheDir != "" {
		if err := os.MkdirAll(m.cacheDir, 0o700); err != nil {
			return response{}, fmt.Errorf("cannot create the cache directory: %w", err)
		}
		f, err := os.CreateTemp(m.cacheDir, ".geoip-"+string(id)+"-*")
		if err != nil {
			return response{}, fmt.Errorf("cannot write to the cache directory: %w", err)
		}
		tmp, tmpName, dst = f, f.Name(), f
		defer os.Remove(tmpName) // no-op once the rename below succeeds
		if err := tmp.Chmod(0o600); err != nil {
			tmp.Close()
			return response{}, fmt.Errorf("cannot set cache file permissions: %w", err)
		}
	} else {
		buf = &bytes.Buffer{}
		dst = buf
	}

	res, err := m.download(ctx, req, dst)
	if tmp != nil {
		if cerr := tmp.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}
	if err != nil || res.notFound {
		return res, err
	}
	if res.notModified {
		m.mu.Lock()
		s := m.sources[id]
		if s.keyGen == gen && s.reader != nil {
			// Unchanged since the last fetch: confirmed current, so the
			// fetch time moves forward (the OUI feed's reasoning).
			s.fetchedAt = m.now()
		}
		m.mu.Unlock()
		m.saveState()
		return res, nil
	}

	// Verify before swapping: a captive portal's HTML or a truncated
	// file must never replace a working database.
	var reader *maxminddb.Reader
	if tmp != nil {
		reader, err = maxminddb.Open(tmpName)
	} else {
		reader, err = maxminddb.OpenBytes(buf.Bytes())
	}
	if err != nil {
		return res, fmt.Errorf("the download is not a readable MMDB database: %w", err)
	}
	if reader.Metadata.NodeCount == 0 {
		_ = reader.Close()
		return res, errors.New("the download is an empty MMDB database")
	}

	m.mu.Lock()
	s := m.sources[id]
	if s.keyGen != gen {
		m.mu.Unlock()
		_ = reader.Close()
		return res, errors.New("the key changed while this download was in flight -- discarded")
	}
	if tmp != nil {
		// Renamed under the lock so a concurrent RemoveKey cannot
		// delete the file between the check above and the swap.
		if err := os.Rename(tmpName, m.cacheFile(id)); err != nil {
			m.mu.Unlock()
			_ = reader.Close()
			return res, fmt.Errorf("cannot replace the cached file: %w", err)
		}
	}
	old := s.reader
	s.reader = reader
	s.fetchedAt = m.now()
	s.month = month
	s.etag = res.etag
	s.lastModified = res.lastModified
	s.fetchedURL = redactURL(req.url)
	s.lastError = ""
	if old != nil {
		// Safe: lookups hold the read lock for the whole lookup, so no
		// lookup is using old once the write lock is held.
		_ = old.Close()
	}
	m.noteSourceChangeLocked()
	m.mu.Unlock()

	m.saveState()
	m.log.Info(fmt.Sprintf("%s: fetched %s", displayName(id), redactURL(req.url)))
	return res, nil
}

// schedule records a successful pass and when the next one is due.
func (m *Manager) schedule(id SourceID, next time.Time) {
	m.mu.Lock()
	s := m.sources[id]
	s.nextRefresh = next
	s.lastError = ""
	m.mu.Unlock()
}

// fail records a failed pass, keeping whatever file is loaded.
func (m *Manager) fail(id SourceID, err error, retry time.Duration) {
	m.mu.Lock()
	s := m.sources[id]
	msg := redact(err.Error(), s.cred.secrets())
	s.lastError = msg
	s.nextRefresh = m.now().Add(retry)
	kept := s.reader != nil
	m.mu.Unlock()
	if kept {
		m.log.Warn(fmt.Sprintf("%s: refresh failed (%s) -- keeping the last good data", displayName(id), msg))
	} else {
		m.log.Warn(fmt.Sprintf("%s: fetch failed (%s)", displayName(id), msg))
	}
}

func (m *Manager) warnIfStale(id SourceID) {
	m.mu.RLock()
	s := m.sources[id]
	stale := s.reader != nil && !s.fetchedAt.IsZero() && m.now().Sub(s.fetchedAt) > StaleAfter
	age := m.now().Sub(s.fetchedAt)
	m.mu.RUnlock()
	if stale {
		m.log.Warn(fmt.Sprintf("%s: the data was last fetched %s ago, more than %s -- still in use, but newer address assignments may be missing",
			displayName(id), age.Round(time.Hour), StaleAfter))
	}
}

// cacheFile is where a source's MMDB lives, "" with no cache directory.
func (m *Manager) cacheFile(id SourceID) string {
	if m.cacheDir == "" {
		return ""
	}
	return filepath.Join(m.cacheDir, string(id)+".mmdb")
}

const stateFileName = "state.json"

// stateFile is the small document beside the MMDB files: what a restart
// needs to make conditional requests and to state freshness honestly.
// It is a cache of someone else's public data, not MikroView's own
// state, so -- like internal/oui's cache -- it deliberately does not go
// through internal/persist and never enters a backup.
type stateFile struct {
	Sources map[SourceID]stateEntry `json:"sources"`
}

type stateEntry struct {
	FetchedAt    time.Time `json:"fetchedAt"`
	Month        string    `json:"month,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"lastModified,omitempty"`
	// URL is redacted: it never carries a token.
	URL string `json:"url,omitempty"`
}

// loadCache opens every cached file at startup, so flags are there from
// the first event after a restart. A keyed source's file is opened only
// while its key is stored and opens. Every failure is a warning and an
// empty source, never a failed start.
func (m *Manager) loadCache() {
	if m.cacheDir == "" {
		return
	}
	var st stateFile
	// #nosec G304 -- this instance's own cache directory, from config.
	if data, err := os.ReadFile(filepath.Join(m.cacheDir, stateFileName)); err == nil {
		if err := json.Unmarshal(data, &st); err != nil {
			m.log.Warn(fmt.Sprintf("country data cache state is unreadable (%v) -- fetching afresh", err))
			st = stateFile{}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range precedence {
		s := m.sources[id]
		p := m.cacheFile(id)
		if id != DBIP && (!s.keySet || s.cred.empty()) {
			// No usable key: a file left from a removed key must
			// never answer.
			if !s.keySet {
				_ = os.Remove(p)
			}
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		r, err := maxminddb.Open(p)
		if err != nil {
			m.log.Warn(fmt.Sprintf("%s: the cached file %s is unreadable (%v) -- fetching afresh", displayName(id), p, err))
			continue
		}
		s.reader = r
		e, ok := st.Sources[id]
		if ok && !e.FetchedAt.IsZero() {
			s.fetchedAt = e.FetchedAt
			s.month, s.etag, s.lastModified, s.fetchedURL = e.Month, e.ETag, e.LastModified, e.URL
		} else {
			s.fetchedAt = info.ModTime()
		}
		m.log.Info(fmt.Sprintf("%s: loaded from the local cache (fetched %s)", displayName(id), s.fetchedAt.UTC().Format(time.RFC3339)))
	}
}

// saveState writes the state document atomically (temp, 0600, rename).
// A failure is logged and otherwise ignored: the only cost is an
// unconditional download next start.
func (m *Manager) saveState() {
	if m.cacheDir == "" {
		return
	}
	st := stateFile{Sources: map[SourceID]stateEntry{}}
	m.mu.RLock()
	for id, s := range m.sources {
		if s.reader == nil {
			continue
		}
		st.Sources[id] = stateEntry{FetchedAt: s.fetchedAt, Month: s.month, ETag: s.etag, LastModified: s.lastModified, URL: s.fetchedURL}
	}
	m.mu.RUnlock()

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(m.cacheDir, 0o700); err != nil {
		m.log.Warn(fmt.Sprintf("country data cache: cannot create %s (%v)", m.cacheDir, err))
		return
	}
	tmp, err := os.CreateTemp(m.cacheDir, ".geoip-state-*")
	if err != nil {
		m.log.Warn(fmt.Sprintf("country data cache: cannot write the state file (%v)", err))
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	if err := os.Rename(name, filepath.Join(m.cacheDir, stateFileName)); err != nil {
		m.log.Warn(fmt.Sprintf("country data cache: cannot replace the state file (%v)", err))
	}
}
