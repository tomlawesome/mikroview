// SPDX-License-Identifier: AGPL-3.0-only

package geoip

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/tomlawesome/mikroview/internal/settings"
)

// ErrNoSealKey refuses key entry on an instance with no retention key
// (history.keyFile). Keys are sealed the way router backups are, and the
// vault's rule is "no key, no backups" (#394); there is deliberately no
// fallback that would store an API key in the clear instead.
var ErrNoSealKey = errors.New("API keys can't be stored on this instance: it has no retention key " +
	"(history.keyFile), which MikroView seals stored keys with the same way it seals router backups. " +
	"Mount a key and restart, then enter it again -- see docs/configuration.md, \"GeoIP country flags\"")

// ErrNoKeyStore refuses key entry when there is no settings store to
// keep it in -- a test fixture's situation, never a real deployment's.
var ErrNoKeyStore = errors.New("API keys can't be stored on this instance: no settings store is configured")

// ValidationError is a credential that fails the minimal checks. Its
// message never repeats the value.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

// maxCredentialLen is far above any real token (IPinfo's are ~14
// characters, MaxMind licence keys 40) and bounds what a request can
// make this store seal and keep.
const maxCredentialLen = 256

// credential is a keyed source's secret. Sealed as JSON; never logged,
// never serialised anywhere else.
type credential struct {
	Token      string `json:"token,omitempty"`
	AccountID  string `json:"accountId,omitempty"`
	LicenseKey string `json:"licenseKey,omitempty"`
}

func (c credential) empty() bool { return c == credential{} }

// secrets lists the values redact strips from every message.
func (c credential) secrets() []string {
	return []string{c.Token, c.LicenseKey, c.AccountID}
}

// sealInfo namespaces this package's use of retention.Key.SealDocument
// per source, so a sealed IPinfo token copied into the MaxMind slot
// fails to open rather than being sent to the wrong provider.
func sealInfo(id SourceID) string { return "geoip/v1/key/" + string(id) }

func validateField(label, v string) error {
	switch {
	case v == "":
		return &ValidationError{label + " is empty"}
	case len(v) > maxCredentialLen:
		return &ValidationError{fmt.Sprintf("%s is longer than %d characters", label, maxCredentialLen)}
	}
	for _, r := range v {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) || r > unicode.MaxASCII {
			return &ValidationError{label + " contains whitespace or characters no key uses"}
		}
	}
	return nil
}

// SetIPinfoToken validates, seals and stores an IPinfo Lite token, and
// asks for an immediate fetch.
func (m *Manager) SetIPinfoToken(token, actor string) error {
	if err := validateField("token", token); err != nil {
		return err
	}
	return m.setKey(IPinfo, credential{Token: token}, actor)
}

// SetMaxMind validates, seals and stores a MaxMind account ID and
// licence key, and asks for an immediate fetch.
func (m *Manager) SetMaxMind(accountID, licenseKey, actor string) error {
	if err := validateField("account ID", accountID); err != nil {
		return err
	}
	// A colon would end the account ID early inside HTTP basic auth.
	if strings.Contains(accountID, ":") {
		return &ValidationError{"account ID contains a colon"}
	}
	if err := validateField("licence key", licenseKey); err != nil {
		return err
	}
	return m.setKey(MaxMind, credential{AccountID: accountID, LicenseKey: licenseKey}, actor)
}

func (m *Manager) setKey(id SourceID, cred credential, actor string) error {
	if m == nil || m.keys == nil {
		return ErrNoKeyStore
	}
	if m.sealer == nil {
		return ErrNoSealKey
	}
	plain, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("encoding the key: %w", err)
	}
	sealed, err := m.sealer.SealDocument(sealInfo(id), plain)
	if err != nil {
		return fmt.Errorf("sealing the key: %w", err)
	}
	at := m.now().UTC()
	if err := m.keys.SetGeoKey(string(id), settings.GeoKey{Sealed: sealed, SetAt: at, SetBy: actor}); err != nil {
		// The settings store has already applied the change in memory
		// (its documented contract), so the running instance uses the
		// key either way; the caller reports that it will not survive
		// a restart.
		m.applyKey(id, cred, at, actor)
		return fmt.Errorf("the key is in use but could not be written to the settings store: %w", err)
	}
	m.applyKey(id, cred, at, actor)
	return nil
}

func (m *Manager) applyKey(id SourceID, cred credential, at time.Time, actor string) {
	m.mu.Lock()
	s := m.sources[id]
	s.keySet = true
	s.cred = cred
	s.setAt = at
	s.setBy = actor
	s.keyGen++
	s.force = true
	s.lastError = ""
	s.nextRefresh = m.now()
	m.mu.Unlock()
	m.log.Info(displayName(id) + ": key set by " + actor + " -- fetching now")
	m.Kick()
}

// RemoveKey forgets a keyed source's key and its downloaded file, and
// falls back to the next source at once -- no network needed.
func (m *Manager) RemoveKey(id SourceID) error {
	if id != IPinfo && id != MaxMind {
		return fmt.Errorf("%q has no key", id)
	}
	if m == nil || m.keys == nil {
		return ErrNoKeyStore
	}
	storeErr := m.keys.ClearGeoKey(string(id))

	m.mu.Lock()
	s := m.sources[id]
	if s.reader != nil {
		_ = s.reader.Close()
	}
	*s = source{id: id, keyGen: s.keyGen + 1}
	m.noteSourceChangeLocked()
	m.mu.Unlock()

	if p := m.cacheFile(id); p != "" {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			m.log.Warn(fmt.Sprintf("%s: could not delete the cached file %s (%v)", displayName(id), p, err))
		}
	}
	m.saveState()
	m.log.Info(displayName(id) + ": key removed")
	if storeErr != nil {
		return fmt.Errorf("the key is no longer in use but its removal could not be written to the settings store: %w", storeErr)
	}
	return nil
}

// Kick asks the refresh loop to run now. Never blocks; a kick while one
// is already pending is the same kick.
func (m *Manager) Kick() {
	if m == nil {
		return
	}
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// loadKeys reads and opens the stored keys at startup. A key that will
// not open -- the retention key changed, or none is mounted any more --
// is reported as set but unusable, so the card says to enter it again
// rather than pretending no key was ever set.
func (m *Manager) loadKeys() {
	if m.keys == nil {
		return
	}
	for _, id := range []SourceID{IPinfo, MaxMind} {
		k, ok := m.keys.GeoKey(string(id))
		if !ok {
			continue
		}
		s := m.sources[id]
		s.keySet = true
		s.setAt = k.SetAt
		s.setBy = k.SetBy
		if m.sealer == nil {
			s.lastError = "the stored key can't be opened: this instance has no retention key (history.keyFile) -- mount the key it was stored under, or remove it and enter it again"
			m.log.Warn(displayName(id) + ": " + s.lastError)
			continue
		}
		plain, err := m.sealer.OpenDocument(sealInfo(id), k.Sealed)
		if err != nil {
			s.lastError = "the stored key can't be opened with this instance's retention key -- remove it and enter it again"
			m.log.Warn(displayName(id) + ": " + s.lastError)
			continue
		}
		var cred credential
		if err := json.Unmarshal(plain, &cred); err != nil || cred.empty() {
			s.lastError = "the stored key is unreadable -- remove it and enter it again"
			m.log.Warn(displayName(id) + ": " + s.lastError)
			continue
		}
		s.cred = cred
	}
}
