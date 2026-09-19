// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/device"
)

// deviceCreateRequest is POST /api/devices' body: a name and nothing
// else, since this route exists for a router that only ever sends logs
// -- it has no address to declare (that is exactly what enrolment is
// for) and no ingest token to auto-discover it through.
type deviceCreateRequest struct {
	Name string `json:"name"`
}

// handleDeviceCreate declares a device by name alone (issue #1281): the
// admin path for a syslog-only router, ready to be enrolled next via
// POST /api/devices/{id}/enrolment. Admin-only, like every other
// device-identity write in this file.
func (s *Server) handleDeviceCreate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	var req deviceCreateRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	// Validated the same way the setup wizard's own device-name field
	// already is (validSetupDevice, setupcommands.go): this id ends up
	// bare inside routeros.BackupScript's user=/dst-path= placements and
	// the enrolment line's device-scoped bookkeeping the same way that
	// field's values do, so it gets the same charset gate rather than a
	// second one that could drift from it.
	if name == "" || !validSetupDevice(name) {
		http.Error(w, "name must be 1 to 64 characters from letters, digits, '.', '_' and '-'", http.StatusBadRequest)
		return
	}
	if s.Devices == nil {
		http.Error(w, "the device registry is not available", http.StatusServiceUnavailable)
		return
	}
	info, err := s.Devices.Create(name, name, time.Now())
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, device.ErrDeviceExists) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.Audit.Record(auditActor(r), "device.created", info.ID, "")
	writeJSON(w, http.StatusCreated, info)
}

// deviceRegisterRequest is POST /api/devices/{id}/registration's body:
// the name the operator confirmed the router under. Empty keeps the
// name it already has.
type deviceRegisterRequest struct {
	Name string `json:"name"`
}

// handleDeviceRegister records the operator's confirmation of a router
// on the device itself -- the ledger's final Register step (issue
// #1291).
//
// Registering grants nothing, and this handler has no path that could:
// device.Registry.Register never sets or changes AcceptedIP. That is
// deliberate and is the rule this endpoint exists to keep -- no web
// request may cause an address to be accepted as a log source.
// Acceptance happens only when a valid, unexpired, single-use enrolment
// token arrives in syslog traffic from that address.
//
// So this endpoint is admin-only and CSRF-gated like its neighbours,
// but asks for no password re-proof: spoofing the click renames a
// device and stamps a date, which is worth nothing to an attacker. The
// re-proof belongs on the endpoint that opens the door, which is
// minting (handleDeviceEnrolmentCreate).
func (s *Server) handleDeviceRegister(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	if s.Devices == nil {
		http.Error(w, "the device registry is not available", http.StatusServiceUnavailable)
		return
	}
	var req deviceRegisterRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	// Same charset gate as handleDeviceCreate's name, for the same
	// reason: this value reaches routeros.BackupScript's bare
	// placements. An empty name is allowed here and means "keep the one
	// it has" -- the operator is confirming the router, not necessarily
	// renaming it.
	if name != "" && !validSetupDevice(name) {
		http.Error(w, "name must be 1 to 64 characters from letters, digits, '.', '_' and '-'", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	info, err := s.Devices.Register(id, name, time.Now())
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, device.ErrDeviceNotFound):
			status = http.StatusNotFound
		case errors.Is(err, device.ErrDeviceConfigured):
			status = http.StatusBadRequest
		}
		// Safe to echo for both: each names only a rule about the
		// requested id, never anything about another device.
		http.Error(w, err.Error(), status)
		return
	}
	s.Audit.Record(auditActor(r), "device.registered", info.ID, "")
	writeJSON(w, http.StatusOK, info)
}

// handleDeviceDelete removes a device this registry itself created --
// Create or a pushing ingest token, never a config.yaml declaration,
// which device.Registry.Delete refuses with ErrDeviceConfigured since
// it would simply reappear on the next boot. Clears the device's
// enrolled address and any pending enrolment token along with it
// (issue #1281's "deleting a device clears its address").
func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	if s.Devices == nil {
		http.Error(w, "the device registry is not available", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.Devices.Delete(id); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, device.ErrDeviceNotFound):
			status = http.StatusNotFound
		case errors.Is(err, device.ErrDeviceConfigured):
			status = http.StatusBadRequest
		}
		// err.Error() is safe to echo in both cases above: it names only
		// a rule about the requested id (not found, declared in
		// config.yaml), never anything about other devices.
		http.Error(w, err.Error(), status)
		return
	}
	s.Audit.Record(auditActor(r), "device.removed", id, "")
	w.WriteHeader(http.StatusNoContent)
}

// deviceEnrolmentResponse is POST /api/devices/{id}/enrolment's 201
// body: the raw token, shown exactly once -- the registry keeps only
// its hash from this point on (device.Registry.MintEnrolment) -- and
// when it expires.
type deviceEnrolmentResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// deviceEnrolmentRequest is POST /api/devices/{id}/enrolment's body:
// the admin's own password, re-proving at the moment of minting that
// they are who the session says they are (issue #1291).
type deviceEnrolmentRequest struct {
	Password string `json:"password"`
	// ExpectedAddress is the one source address this token may be
	// redeemed from (issue #1291). The enrolment window binds to it, so
	// the syslog port opens to that address alone rather than to every
	// unknown address on the network for the life of the token.
	ExpectedAddress string `json:"expectedAddress"`
}

// handleDeviceEnrolmentCreate mints a fresh enrolment token for device,
// replacing any pending one -- this is also the wizard's "Reroll"
// affordance, since minting again is exactly a reroll. Admin-only: an
// enrolment token is a bearer credential for attributing syslog traffic
// to a device, the same tier every other token-issuing endpoint in this
// API holds to (see handleTokensCreate).
func (s *Server) handleDeviceEnrolmentCreate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	if s.Devices == nil {
		http.Error(w, "the device registry is not available", http.StatusServiceUnavailable)
		return
	}
	now := time.Now()
	// Holding an admin session is not enough to open this door (issue
	// #1291). This is the endpoint that mints the credential which, once
	// it arrives from a machine the holder controls, has an address
	// accepted as a log source -- so a stolen session cookie, a
	// cross-site request riding the admin's browser, or script injected
	// into a page they are viewing must not be sufficient on its own.
	// The admin re-proves who they are at the moment of minting.
	//
	// Reroll goes through this same endpoint, so it asks every time.
	// That is the feature working, not a snag to smooth over.
	user, ok := s.sessionUser(r, now)
	if !ok {
		writeUnauthorized(w, "sign in first")
		return
	}
	var req deviceEnrolmentRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// An SSO-only admin has no local password to re-prove with, and
	// inventing a weaker check for them would defeat the point of having
	// one at all. Minting through a round trip back to the identity
	// provider is its own piece of work; until it ships they are told
	// plainly rather than quietly let through. Same shape and status as
	// handleAuthChangePassword's answer to the same situation.
	if !user.LocalPassword() {
		http.Error(w, "this account signs in through your identity provider, and minting an enrolment token needs a password re-check that does not exist for it yet; declare this router in config.yaml instead", http.StatusConflict)
		return
	}
	// Rate-limited on the same limiter as login, keyed by user: the
	// password is a credential and this is a guess at it, so verifying
	// one without counting the attempt is a brute-force oracle that
	// happens to need a session -- and a session is exactly what an
	// attacker who has stolen a cookie already has. Copied from
	// handleAuthChangePassword, which guards the same thing.
	userKey := "user:" + strings.ToLower(user.Username)
	if !s.LoginLimiter.Reserve(userKey, now) {
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}
	if _, err := s.Auth.Authenticate(user.Username, req.Password, now); err != nil {
		// Reservation stays claimed: that is what counts the failure.
		writeUnauthorized(w, "password is incorrect")
		return
	}
	s.LoginLimiter.Release(userKey, now)

	id := r.PathValue("id")
	token, expiresAt, err := s.Devices.MintEnrolment(id, strings.TrimSpace(req.ExpectedAddress), now)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, device.ErrDeviceNotFound):
			status = http.StatusNotFound
		case errors.Is(err, device.ErrExpectedAddressRequired), errors.Is(err, device.ErrExpectedAddressInvalid):
			status = http.StatusBadRequest
		}
		// Safe to echo: each names only a rule about what the caller
		// just sent, never anything about another device.
		http.Error(w, err.Error(), status)
		return
	}
	s.Audit.Record(auditActor(r), "device.enrolment_minted", id, "")
	// The raw token is shown exactly once, in this response -- no-store
	// so no cache along the way keeps a copy, same header every other
	// one-time-secret response on this API sets (e.g. droplist's key
	// mint).
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, deviceEnrolmentResponse{Token: token, ExpiresAt: expiresAt})
}

// handleDeviceEnrolmentDelete burns device's pending enrolment token,
// if it has one -- withdrawing an in-flight enrolment before it is
// redeemed. Admin-only, same tier as minting one.
func (s *Server) handleDeviceEnrolmentDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	if s.Devices == nil {
		http.Error(w, "the device registry is not available", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.Devices.BurnEnrolment(id); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, device.ErrDeviceNotFound), errors.Is(err, device.ErrNoPendingEnrolment):
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.Audit.Record(auditActor(r), "device.enrolment_revoked", id, "")
	w.WriteHeader(http.StatusNoContent)
}

// handleDevicesRefused serves every syslog source address the listener
// gate has refused a line from (issue #1281): not yet a device's
// sourceIp/acceptedIp, and never carrying a valid enrolment marker
// either. In-memory, bounded to 256 addresses (device.Refused's own doc
// comment). Admin-only: unlike GET /api/devices, this names addresses
// that have never proven anything about themselves, which is closer to
// GET /api/audit's "who has been probing this instance" than to the
// fleet's own read.
func (s *Server) handleDevicesRefused(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	var refused []device.Refused
	if s.Devices != nil {
		refused = s.Devices.Refused()
	}
	if refused == nil {
		refused = []device.Refused{}
	}
	writeJSON(w, http.StatusOK, refused)
}
