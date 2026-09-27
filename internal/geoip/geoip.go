// SPDX-License-Identifier: AGPL-3.0-only

// Package geoip resolves public IPs to a country code, and -- where the
// source has it -- to the network that owns the address (#1352).
//
// Three sources, each an MMDB file this instance fetches for itself at
// runtime and caches on disk; nothing ships in the image (AGENTS.md,
// "List and lookup data"):
//
//   - DB-IP IP-to-Country Lite: the no-setup default. No account, no
//     key, a new file each month.
//   - IPinfo Lite: country and network owner (ASN), with a free token
//     the operator enters in the Engine Room.
//   - MaxMind GeoLite2-Country: country only, downloaded on the
//     operator's behalf from the account ID and licence key they enter
//     the same way.
//
// Precedence is fixed, not chosen (Fable call, 2026-09-27): IPinfo beats
// MaxMind beats DB-IP. Setting a key switches the source once its file
// has arrived; removing it falls back. Lookups only ever consult the
// source in use -- never fall through to a lower one per address --
// because the credits follow the source in use (DB-IP's link sits at the
// foot of the stream only while DB-IP is it), and an answer quietly taken
// from a source nobody is crediting would break that.
//
// Every failure degrades rather than errors: no file yet, a failed
// fetch or a bad key means lookups report "unknown" (or keep answering
// from the last good file), never a startup failure.
package geoip

import (
	"log/slog"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/settings"
)

// SourceID names one of the three sources. The strings are the API's
// ("source": "ipinfo") and the settings document's keys.
type SourceID string

const (
	DBIP    SourceID = "dbip"
	IPinfo  SourceID = "ipinfo"
	MaxMind SourceID = "maxmind"
)

// precedence is the fixed order sources are preferred in.
var precedence = []SourceID{IPinfo, MaxMind, DBIP}

// StaleAfter is when a loaded file stops being quietly trusted: it keeps
// serving (an address's country rarely moves) but each refresh attempt
// logs a warning, so a feed that has been failing for weeks is said out
// loud. 45 days gives DB-IP's monthly file a full month plus slack.
const StaleAfter = 45 * 24 * time.Hour

// KeyStore is where sealed keys are kept -- *settings.Store in
// production, which is already in the backup envelope.
type KeyStore interface {
	GeoKey(source string) (settings.GeoKey, bool)
	SetGeoKey(source string, k settings.GeoKey) error
	ClearGeoKey(source string) error
}

// Options configures New.
type Options struct {
	// CacheDir holds the fetched MMDB files and a small state document
	// between restarts. Empty keeps everything in memory, so each start
	// fetches afresh.
	CacheDir string
	// Keys stores the sealed API keys. Nil refuses key entry.
	Keys KeyStore
	// Sealer is the retention key (history.keyFile) the router-backup
	// vault seals with. Nil refuses key entry -- see ErrNoSealKey.
	Sealer *retention.Key
	Log    *slog.Logger
}

// source is one feed's live state. Every field is guarded by
// Manager.mu.
type source struct {
	id SourceID

	reader    *maxminddb.Reader
	fetchedAt time.Time
	// month is the DB-IP file month loaded ("2026-09"); unused by the
	// other two.
	month        string
	etag         string
	lastModified string
	// fetchedURL is the redacted URL the ETag belongs to, so a
	// conditional request is only ever made against the same file.
	fetchedURL  string
	nextRefresh time.Time
	lastError   string
	// force asks the refresh loop to fetch this source on its next pass
	// regardless of nextRefresh -- set when a key is entered.
	force bool

	// Keyed sources only.
	keySet bool
	setAt  time.Time
	setBy  string
	cred   credential
	// keyGen moves on every set or removal, so a fetch that started
	// under a previous key cannot install its result afterwards.
	keyGen uint64
}

// Manager owns the three sources. Safe for concurrent use; the zero
// value is not usable, but a nil *Manager is, and answers every lookup
// with "unknown".
type Manager struct {
	mu      sync.RWMutex
	sources map[SourceID]*source
	// lastSource is the source in use as of the last change, so a
	// switch is logged once rather than on every call.
	lastSource SourceID

	cacheDir string
	keys     KeyStore
	sealer   *retention.Key
	log      *slog.Logger
	client   *http.Client
	eps      endpoints
	now      func() time.Time

	kick chan struct{}
}

// New builds a Manager, loads any cached files (and, for the keyed
// sources, only when their key is still stored and opens), and does no
// network I/O -- Run does that.
func New(opts Options) *Manager {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	m := &Manager{
		sources: map[SourceID]*source{
			DBIP:    {id: DBIP},
			IPinfo:  {id: IPinfo},
			MaxMind: {id: MaxMind},
		},
		cacheDir: opts.CacheDir,
		keys:     opts.Keys,
		sealer:   opts.Sealer,
		log:      log,
		client:   newFetchClient(),
		eps:      defaultEndpoints(),
		now:      time.Now,
		kick:     make(chan struct{}, 1),
	}
	m.loadKeys()
	m.loadCache()
	m.mu.Lock()
	m.lastSource = m.sourceLocked()
	m.mu.Unlock()
	return m
}

// Close releases every open file.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sources {
		if s.reader != nil {
			_ = s.reader.Close()
			s.reader = nil
		}
	}
}

// sourceLocked is the source in use: the first in precedence order with
// a loaded file (and, for a keyed source, a key still set). Empty when
// none is.
func (m *Manager) sourceLocked() SourceID {
	for _, id := range precedence {
		s := m.sources[id]
		if s.reader == nil {
			continue
		}
		if id != DBIP && !s.keySet {
			continue
		}
		return id
	}
	return ""
}

// Source reports the source in use, or "" when there is none -- the
// `geoSource` field on /api/healthz.
func (m *Manager) Source() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return string(m.sourceLocked())
}

// Available reports whether country flags can be shown at all: the
// `geoip` field on /api/healthz.
func (m *Manager) Available() bool { return m.Source() != "" }

// noteSourceChangeLocked logs a switch of the source in use, once.
func (m *Manager) noteSourceChangeLocked() {
	now := m.sourceLocked()
	if now == m.lastSource {
		return
	}
	switch {
	case now == "":
		m.log.Info("country flags: no source loaded -- flags are off until one is")
	case m.lastSource == "":
		m.log.Info("country flags: now from " + displayName(now))
	default:
		m.log.Info("country flags: switched from " + displayName(m.lastSource) + " to " + displayName(now))
	}
	m.lastSource = now
}

// Country returns the ISO 3166-1 alpha-2 country code for ipStr from the
// source in use, and whether one was found. ok=false (never an error)
// for: no source loaded, an unparseable IP, a non-public address, or no
// match.
func (m *Manager) Country(ipStr string) (code string, ok bool) {
	ip, ok := lookupAddr(ipStr)
	if !ok || m == nil {
		return "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	id := m.sourceLocked()
	if id == "" {
		return "", false
	}
	code = countryFrom(m.sources[id].reader.Lookup(ip))
	return code, code != ""
}

// Owner returns the network owner for ipStr -- the ASN and its name --
// and whether one was found. Only IPinfo carries it, so this answers
// only while IPinfo is the source in use.
func (m *Manager) Owner(ipStr string) (asn uint, name string, ok bool) {
	ip, ok := lookupAddr(ipStr)
	if !ok || m == nil {
		return 0, "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.sourceLocked() != IPinfo {
		return 0, "", false
	}
	asn, name = ownerFrom(m.sources[IPinfo].reader.Lookup(ip))
	return asn, name, asn != 0
}

// lookupAddr parses and filters an address for lookup. Unmapped first:
// ::ffff:8.8.8.8 is the same host as 8.8.8.8 (#1081).
func lookupAddr(ipStr string) (netip.Addr, bool) {
	if ipStr == "" {
		return netip.Addr{}, false
	}
	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return netip.Addr{}, false
	}
	ip = ip.Unmap()
	return ip, isPublic(ip)
}

// isPublic is the ingest-path filter: an address it is meaningful to
// geolocate. Deliberately the looser of this package's two predicates
// (see IsPublicUnicast), unchanged from before #1352.
func isPublic(ip netip.Addr) bool {
	return ip.IsValid() &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast()
}

// SourceStatus is one source's fetch state, as GET /api/settings/geo
// reports it. Pointers so "never" serialises as null rather than as the
// year 1.
type SourceStatus struct {
	Loaded      bool       `json:"loaded"`
	FetchedAt   *time.Time `json:"fetchedAt"`
	NextRefresh *time.Time `json:"nextRefresh"`
	LastError   *string    `json:"lastError"`
}

// KeyedSourceStatus adds the write-only key's state: whether one is set,
// when and by whom -- never the value.
type KeyedSourceStatus struct {
	KeySet bool       `json:"keySet"`
	SetAt  *time.Time `json:"setAt"`
	SetBy  *string    `json:"setBy"`
	SourceStatus
}

// Sources is Status's per-source half.
type Sources struct {
	DBIP    SourceStatus      `json:"dbip"`
	IPinfo  KeyedSourceStatus `json:"ipinfo"`
	MaxMind KeyedSourceStatus `json:"maxmind"`
}

// Status is GET /api/settings/geo's body.
type Status struct {
	Source  *string `json:"source"`
	Sources Sources `json:"sources"`
}

// Status reports every source's state. Safe on a nil Manager, which
// reports nothing loaded.
func (m *Manager) Status() Status {
	if m == nil {
		return Status{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var st Status
	if id := m.sourceLocked(); id != "" {
		s := string(id)
		st.Source = &s
	}
	st.Sources.DBIP = m.sources[DBIP].statusLocked()
	st.Sources.IPinfo = m.sources[IPinfo].keyedStatusLocked()
	st.Sources.MaxMind = m.sources[MaxMind].keyedStatusLocked()
	return st
}

func (s *source) statusLocked() SourceStatus {
	st := SourceStatus{Loaded: s.reader != nil}
	if !s.fetchedAt.IsZero() && s.reader != nil {
		t := s.fetchedAt
		st.FetchedAt = &t
	}
	if !s.nextRefresh.IsZero() {
		t := s.nextRefresh
		st.NextRefresh = &t
	}
	if s.lastError != "" {
		e := s.lastError
		st.LastError = &e
	}
	return st
}

func (s *source) keyedStatusLocked() KeyedSourceStatus {
	ks := KeyedSourceStatus{KeySet: s.keySet}
	if !s.keySet {
		// No key, no feed: whatever a previous key left in the
		// fetch fields is not this source's state any more.
		return ks
	}
	ks.SourceStatus = s.statusLocked()
	if !s.setAt.IsZero() {
		t := s.setAt
		ks.SetAt = &t
	}
	if s.setBy != "" {
		b := s.setBy
		ks.SetBy = &b
	}
	return ks
}

func displayName(id SourceID) string {
	switch id {
	case DBIP:
		return "DB-IP Lite"
	case IPinfo:
		return "IPinfo Lite"
	case MaxMind:
		return "MaxMind GeoLite2"
	}
	return string(id)
}
