// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// TestListUsersResponseIsUnchangedByTheBatchedRead pins #1345 E1-F1's
// fix as a pure cost change: GET /api/auth/users must answer byte for
// byte what it answered when it asked the store List, HasActiveTOTP and
// PasskeyCount separately. The expected body is built that old way
// here, over accounts that between them hold each kind of factor, so a
// row reading the blanked copy instead of the live record shows up.
func TestListUsersResponseIsUnchangedByTheBatchedRead(t *testing.T) {
	s, _ := newTestServer(t)
	now := time.Now()
	var ids []string
	for _, name := range []string{"bilbo", "frodo", "sam"} {
		u, _, err := s.Auth.FindOrCreateOIDCUser("https://idp.example", "subject-"+name, name, now)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, u.ID)
	}
	if err := s.Auth.SetPendingTOTPSecret(ids[0], "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}
	if err := s.Auth.ConfirmTOTP(ids[0], now, 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []byte{1, 2} {
		pk := auth.Passkey{ID: []byte{id}, PublicKey: []byte{id, id, id}, RPID: "mikroview.example"}
		if _, err := s.Auth.AddPasskey(ids[1], pk); err != nil {
			t.Fatal(err)
		}
	}

	var legacy []userSummary
	for _, u := range s.Auth.List() {
		legacy = append(legacy, userSummary{
			ID:               u.ID,
			Username:         u.Username,
			Role:             string(u.Role),
			CreatedAt:        u.CreatedAt,
			LastLogin:        u.LastLogin,
			HasLocalPassword: u.LocalPassword(),
			SSO:              u.OIDCIssuer != "",
			HasTOTP:          s.Auth.HasActiveTOTP(u.ID),
			PasskeyCount:     s.Auth.PasskeyCount(u.ID),
		})
	}
	want := httptest.NewRecorder()
	writeJSON(want, http.StatusOK, legacy)

	got := httptest.NewRecorder()
	asAdmin(s.mux()).ServeHTTP(got, httptest.NewRequest(http.MethodGet, "/api/auth/users", nil))
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", got.Code, got.Body)
	}
	if !bytes.Equal(got.Body.Bytes(), want.Body.Bytes()) {
		t.Errorf("GET /api/auth/users body changed:\n got %s\nwant %s", got.Body, want.Body)
	}
	if !bytes.Contains(want.Body.Bytes(), []byte(`"hasTOTP":true`)) || !bytes.Contains(want.Body.Bytes(), []byte(`"passkeyCount":2`)) {
		t.Errorf("test setup: the expected body carries no factor to compare, got %s", want.Body)
	}
}
