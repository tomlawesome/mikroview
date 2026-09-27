// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"
	"net/netip"

	"github.com/tomlawesome/mikroview/internal/geoip"
	"github.com/tomlawesome/mikroview/internal/logging"
)

var geoLog = logging.New("geoip")

// GeoService is what the API needs from internal/geoip's Manager. An
// interface so the handlers' own rules -- address filtering, key
// redaction, error mapping -- are testable without downloading a
// database.
type GeoService interface {
	Country(ip string) (string, bool)
	Owner(ip string) (asn uint, name string, ok bool)
	Source() string
	Status() geoip.Status
	SetIPinfoToken(token, actor string) error
	SetMaxMind(accountID, licenseKey, actor string) error
	RemoveKey(id geoip.SourceID) error
}

func (s *Server) geoAvailable() bool { return s.geoSource() != nil }

// geoSource is healthz's `geoSource`: the source in use, or nil (JSON
// null) when none is loaded.
func (s *Server) geoSource() *string {
	if s.Geo == nil {
		return nil
	}
	src := s.Geo.Source()
	if src == "" {
		return nil
	}
	return &src
}

func (s *Server) geoStatus() geoip.Status {
	if s.Geo == nil {
		return geoip.Status{}
	}
	return s.Geo.Status()
}

// handleGeoSettings is GET /api/settings/geo: the source in use and each
// source's fetch state. For the two keyed sources it says whether a key
// is set, when and by whom -- never the key: there is no route by which
// a stored key comes back out (the droplist pull-key pattern).
//
// Admin-only, like the rest of the Engine Room grid the card sits in.
func (s *Server) handleGeoSettings(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, s.geoStatus())
}

type geoIPinfoRequest struct {
	Token string `json:"token"`
}

type geoMaxMindRequest struct {
	AccountID  string `json:"accountId"`
	LicenseKey string `json:"licenseKey"`
}

// handleGeoIPinfoSet is PUT /api/settings/geo/ipinfo.
func (s *Server) handleGeoIPinfoSet(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	var req geoIPinfoRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	s.geoKeyChange(w, r, geoip.IPinfo, "key set", func(actor string) error {
		return s.Geo.SetIPinfoToken(req.Token, actor)
	})
}

// handleGeoMaxMindSet is PUT /api/settings/geo/maxmind.
func (s *Server) handleGeoMaxMindSet(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	var req geoMaxMindRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	s.geoKeyChange(w, r, geoip.MaxMind, "key set", func(actor string) error {
		return s.Geo.SetMaxMind(req.AccountID, req.LicenseKey, actor)
	})
}

// handleGeoIPinfoDelete is DELETE /api/settings/geo/ipinfo.
func (s *Server) handleGeoIPinfoDelete(w http.ResponseWriter, r *http.Request) {
	s.geoKeyRemove(w, r, geoip.IPinfo)
}

// handleGeoMaxMindDelete is DELETE /api/settings/geo/maxmind.
func (s *Server) handleGeoMaxMindDelete(w http.ResponseWriter, r *http.Request) {
	s.geoKeyRemove(w, r, geoip.MaxMind)
}

func (s *Server) geoKeyRemove(w http.ResponseWriter, r *http.Request, id geoip.SourceID) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	s.geoKeyChange(w, r, id, "key removed", func(string) error {
		return s.Geo.RemoveKey(id)
	})
}

// geoKeyChange runs one key change and answers with the GET shape. The
// audit entry records which source and what happened -- never the
// value, which appears in no log, audit detail or response.
//
// A persistence failure is reported but still audited: the change is
// already in effect on the running instance (see settings.Store's
// contract), so the log must say it happened.
func (s *Server) geoKeyChange(w http.ResponseWriter, r *http.Request, id geoip.SourceID, what string, apply func(actor string) error) {
	if s.Geo == nil {
		http.Error(w, "country data sources are not configured on this instance", http.StatusServiceUnavailable)
		return
	}
	actor := auditActor(r)
	err := apply(actor)
	var ve *geoip.ValidationError
	switch {
	case errors.As(err, &ve):
		http.Error(w, ve.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, geoip.ErrNoSealKey), errors.Is(err, geoip.ErrNoKeyStore):
		http.Error(w, err.Error(), http.StatusConflict)
		return
	case err != nil:
		s.Audit.Record(actor, "settings.geo", string(id), what)
		geoLog.Error(string(id) + ": " + err.Error())
		http.Error(w, "the change is in effect but could not be saved, so it will not survive a restart", http.StatusInternalServerError)
		return
	}
	s.Audit.Record(actor, "settings.geo", string(id), what)
	writeJSON(w, http.StatusOK, s.geoStatus())
}

// geoLookupResponse is GET /api/geo/lookup's body. Every field is null
// when unknown.
type geoLookupResponse struct {
	Country *string `json:"country"`
	ASN     *uint   `json:"asn"`
	ASName  *string `json:"asName"`
}

// handleGeoLookup is GET /api/geo/lookup?ip=<addr>: the country and
// network owner for one address, on demand -- the IP popover's
// "Network" line and the dossier's "network owner" row. Owner is only
// ever looked up here, never attached to every event (Fable call,
// 2026-09-27): the stream carries thousands of events and the owner is
// read only when someone asks.
//
// Any signed-in user: it reveals nothing the stream's flags do not, for
// addresses the caller is already looking at. Only public unicast
// addresses are looked up; a private or reserved one answers all
// nulls, and a malformed one is a 400.
func (s *Server) handleGeoLookup(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("ip")
	addr, err := netip.ParseAddr(raw)
	if err != nil || addr.Zone() != "" {
		http.Error(w, "ip must be an IPv4 or IPv6 address", http.StatusBadRequest)
		return
	}
	addr = addr.Unmap()
	var resp geoLookupResponse
	if s.Geo != nil && geoip.IsPublicUnicast(addr) {
		ip := addr.String()
		if code, ok := s.Geo.Country(ip); ok {
			resp.Country = &code
		}
		if asn, name, ok := s.Geo.Owner(ip); ok {
			resp.ASN = &asn
			if name != "" {
				resp.ASName = &name
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
