// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/naming"
)

// mustPayload decodes one pushed-state page the way the ingest endpoint
// would, so these tests seed RouterState through its real validation
// rather than hand-building an internal struct.
func mustPayload(t *testing.T, body string) ingest.Payload {
	t.Helper()
	p, err := ingest.DecodePayload(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodePayload(%s): %v", body, err)
	}
	return p
}

// getProvenance asks the endpoint about one token and decodes the answer.
func getProvenance(t *testing.T, base, entityType, key, device string) (int, nameProvenanceResponse) {
	t.Helper()
	url := base + "/api/naming/provenance?type=" + entityType + "&key=" + key
	if device != "" {
		url += "&device=" + device
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out nameProvenanceResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, out
}

// TestNameProvenanceRefusesAnEditTheRouterWouldShadow is the whole
// reason this endpoint exists (#413, owner ruling 2026-08-22).
//
// The setup is the exact trap: a host that RouterOS names via a DHCP
// lease, and an operator who has already saved their own label for it.
// POST /api/entities happily stored that label and answered 200 -- and
// it is never displayed, because naming.Resolver.Host consults
// RouterHosts first and always will. Editable=false is how the editor
// finds that out before offering a field, and Source/Router are what it
// needs to say where the real name lives.
func TestNameProvenanceRefusesAnEditTheRouterWouldShadow(t *testing.T) {
	s, _ := newTestServer(t)
	if _, err := s.Entities.Upsert(entities.Entity{Type: entities.TypeHost, Key: "192.168.1.20", Label: "nas"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RouterState.Apply("core", mustPayload(t, `{"kind":"dhcp-lease","page":1,"pages":1,`+
		`"records":[{"hostname":"android-dhcp-1234","mac":"aa:bb:cc:dd:ee:01","address":"192.168.1.20"}]}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	s.Naming = naming.Resolver{Entities: s.Entities, RouterHosts: s.RouterState}

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	status, got := getProvenance(t, ts.URL, "host", "192.168.1.20", "core")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got.Editable {
		t.Error("Editable = true -- the editor would accept a label the router's name permanently shadows")
	}
	if got.Source != naming.SourceRouterDHCPLease {
		t.Errorf("Source = %q, want %q -- the operator has to be told which pushed table to go and change", got.Source, naming.SourceRouterDHCPLease)
	}
	if got.Router != "core" {
		t.Errorf("Router = %q, want the device whose table holds the winning name", got.Router)
	}
	if got.Name != "android-dhcp-1234" {
		t.Errorf("Name = %q, want the router-pushed name that is actually displayed", got.Name)
	}
	if got.Label != "nas" {
		t.Errorf("Label = %q -- the shadowed label must still be reported, so the editor can say it is not what is shown", got.Label)
	}
}

// The gate must open as readily as it shuts. A host no router names, a
// rule label and a port are all editable, and each reports the source
// that actually supplied its name -- an editor that refused everything
// would satisfy the test above and ship nothing.
func TestNameProvenanceAllowsEditsThatTakeEffect(t *testing.T) {
	s, _ := newTestServer(t)
	if _, err := s.Entities.Upsert(entities.Entity{Type: entities.TypePort, Key: "8291", Label: "winbox"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RouterState.Apply("core", mustPayload(t, `{"kind":"dhcp-lease","page":1,"pages":1,`+
		`"records":[{"hostname":"android-dhcp-1234","mac":"aa:bb:cc:dd:ee:01","address":"192.168.1.20"}]}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	s.Naming = naming.Resolver{
		Entities:    s.Entities,
		Rules:       map[string]string{"fw-fwd-drop-inv": "Forward: drop invalid"},
		RouterHosts: s.RouterState,
	}

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	cases := []struct {
		entityType string
		key        string
		device     string
		wantName   string
		wantSource string
	}{
		{"host", "192.168.1.99", "core", "", naming.SourceNone},
		{"rule", "fw-fwd-drop-inv", "", "Forward: drop invalid", naming.SourceConfig},
		{"port", "8291", "", "winbox", naming.SourceEntity},
	}
	for _, tc := range cases {
		status, got := getProvenance(t, ts.URL, tc.entityType, tc.key, tc.device)
		if status != http.StatusOK {
			t.Fatalf("%s %q: status = %d, want 200", tc.entityType, tc.key, status)
		}
		if !got.Editable {
			t.Errorf("%s %q: Editable = false -- this edit does take effect", tc.entityType, tc.key)
		}
		if got.Router != "" {
			t.Errorf("%s %q: Router = %q, want empty on an editable token", tc.entityType, tc.key, got.Router)
		}
		if got.Name != tc.wantName || got.Source != tc.wantSource {
			t.Errorf("%s %q = {%q, %q}, want {%q, %q}", tc.entityType, tc.key, got.Name, got.Source, tc.wantName, tc.wantSource)
		}
	}
}

// A host name pushed by one router must not answer for another's
// traffic here either. The endpoint takes device from the caller's
// query string, so this is the one place the scoping fixed in
// #285/#283/#284 could be re-opened by a new entry point.
func TestNameProvenanceScopesRouterNamesToTheirDevice(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.RouterState.Apply("core", mustPayload(t, `{"kind":"dns-static","page":1,"pages":1,`+
		`"records":[{"name":"trusted-nas","address":"192.168.1.20"}]}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	s.Naming = naming.Resolver{Entities: s.Entities, RouterHosts: s.RouterState}

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	if _, got := getProvenance(t, ts.URL, "host", "192.168.1.20", "some-other-router"); !got.Editable || got.Name != "" {
		t.Errorf("provenance for another device = %+v -- core's pushed name reached it, and gated an edit that would in fact take effect", got)
	}
}

// The endpoint is admin-gated, like the entity store it reads and the
// editor it serves.
func TestNameProvenanceIsAdminOnly(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	if status, _ := getProvenance(t, ts.URL, "host", "192.168.1.20", "core"); status != http.StatusForbidden {
		t.Errorf("status = %d, want 403 without an admin identity", status)
	}
}

func TestNameProvenanceRejectsAnUnusableQuery(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	if status, _ := getProvenance(t, ts.URL, "host", "", "core"); status != http.StatusBadRequest {
		t.Errorf("empty key: status = %d, want 400", status)
	}
	if status, _ := getProvenance(t, ts.URL, "interface", "ether1", ""); status != http.StatusBadRequest {
		t.Errorf("unknown type: status = %d, want 400", status)
	}
	// A port key that is not a number is a miss, not an error -- the
	// resolver already treats port <= 0 that way, and this endpoint is
	// a question rather than a mutation.
	if status, got := getProvenance(t, ts.URL, "port", "not-a-port", ""); status != http.StatusOK || got.Source != naming.SourceNone {
		t.Errorf("non-numeric port: status = %d, source = %q, want 200/none", status, got.Source)
	}
}
