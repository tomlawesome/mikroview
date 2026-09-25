// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/prefs"
)

// preferencesSchemaVersion is the only version PATCH /api/me/preferences
// currently accepts. It exists so a future, incompatible reshaping of
// the document has something to bump and refuse the old shape against,
// the same reason every other versioned document in this codebase
// carries one -- there is no migration logic yet because nothing has
// ever needed one.
const preferencesSchemaVersion = 1

// preferencesDocument is the wire shape both GET and PATCH use (#1283):
// a schema version alongside the caller's own opaque preferences
// object. This package never looks inside Prefs -- see internal/prefs's
// own doc comment for why that is deliberate.
//
// UserID is set only on the way out, by handlePreferencesGet: the
// frontend's one-time legacy-localStorage migration binds itself to
// whichever account first sees an empty record after the upgrade (a
// shared browser must not hand those old keys to a second account that
// later signs in on it), and needs this id to tell that account apart
// from any other. A PATCH body never carries one; nothing here reads it
// back in.
type preferencesDocument struct {
	Version int             `json:"version"`
	Prefs   json.RawMessage `json:"prefs"`
	UserID  string          `json:"userId,omitempty"`
}

// emptyPrefsObject is what GET answers for a user with no stored
// record -- the documented "missing record" default, distinct from a
// user who explicitly stored `{}`, which reads back the same way. #1283
// settled that a missing record and an empty one are indistinguishable
// on the wire, since either means every frontend module falls back to
// its own default.
var emptyPrefsObject = json.RawMessage(`{}`)

// handlePreferencesGet answers the caller's own preferences record,
// always as the caller's own -- there is no id in the request, the same
// "acts only on the session's own account" shape as /api/auth/password
// and /api/auth/logout-all. A user with no stored record yet (never
// saved anything, or freshly created) gets version 1 and an empty
// object, not a 404: an empty record is a normal, expected state, not
// an error.
func (s *Server) handlePreferencesGet(w http.ResponseWriter, r *http.Request) {
	user, ok := s.sessionUser(r, time.Now())
	if !ok {
		writeUnauthorized(w, "sign in first")
		return
	}

	doc := preferencesDocument{Version: preferencesSchemaVersion, Prefs: emptyPrefsObject, UserID: user.ID}
	if raw, ok := s.Prefs.Get(user.ID); ok {
		doc.Prefs = raw
	}
	writeJSON(w, http.StatusOK, doc)
}

// handlePreferencesPatch merges the caller's changed preference keys
// into their stored record (internal/prefs's Store.Merge, under the
// store's own lock), rather than replacing it -- #1283's own "save only
// what changed" ruling. A whole-record replace meant two browser tabs
// each saving a different key around the same time raced: whichever
// PUT landed second discarded the other tab's key even though nothing
// about it had changed. prefs here is the changed keys only, not the
// caller's whole record.
//
// The body is capped at maxJSONBodyBytes the same way every other JSON
// body on this API is (decodeJSONBody), version must be exactly
// preferencesSchemaVersion, and prefs must decode as a JSON object --
// not an array, string, number or null. Anything outside that is a 400,
// and a patch that would grow the stored record past
// prefs.MaxRecordBytes is a 413; there is nothing here for a caller to
// be forbidden from doing (see the route comment in server.go), so
// every failure this handler can produce is the caller's mistake, never
// a permission question.
func (s *Server) handlePreferencesPatch(w http.ResponseWriter, r *http.Request) {
	user, ok := s.sessionUser(r, time.Now())
	if !ok {
		writeUnauthorized(w, "sign in first")
		return
	}

	var req preferencesDocument
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Version != preferencesSchemaVersion {
		http.Error(w, "unsupported preferences version", http.StatusBadRequest)
		return
	}
	// json.RawMessage already proved req.Prefs is syntactically valid
	// JSON (or absent, which decodes to nil and fails this the same
	// way) -- this proves it is specifically an object, not any other
	// valid JSON value, without this package ever needing to know what
	// is inside it. `null` is the one value json.Unmarshal accepts into
	// a map with no error while still not being an object -- it leaves
	// probe nil rather than allocating an empty map, which is exactly
	// how an absent Prefs field decodes too, so nil is refused
	// alongside it explicitly.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(req.Prefs, &probe); err != nil || probe == nil {
		http.Error(w, "prefs must be a JSON object", http.StatusBadRequest)
		return
	}

	if err := s.Prefs.Merge(user.ID, req.Prefs); err != nil {
		if errors.Is(err, prefs.ErrRecordTooLarge) {
			http.Error(w, fmt.Sprintf("preferences record would exceed %d KiB; nothing was saved", prefs.MaxRecordBytes/1024), http.StatusRequestEntityTooLarge)
			return
		}
		apiLog.Error("saving preferences for " + user.ID + ": " + err.Error())
		http.Error(w, "preferences could not be saved", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
