// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routeros"
)

func postSetupCommands(t *testing.T, base string, req setupCommandsRequest) setupCommandsResponse {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(base+"/api/setup/commands", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/setup/commands: status = %d", resp.StatusCode)
	}
	var out setupCommandsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestHandleSetupCommandsRequiresAddress covers the one required field
// the contract states: every other field is optional, address is not.
func TestHandleSetupCommandsRequiresAddress(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	body, _ := json.Marshal(setupCommandsRequest{})
	resp, err := http.Post(ts.URL+"/api/setup/commands", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a missing address", resp.StatusCode)
	}
}

// TestHandleSetupCommandsNoVersionPicked covers the "picked": null case
// and that the table/steps still render against the single existing
// dialect with nothing chosen.
func TestHandleSetupCommandsNoVersionPicked(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	out := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "mv.example.net:8443"})

	if out.Picked != nil {
		t.Errorf("Picked = %+v, want nil when no version was sent", out.Picked)
	}
	// Compared against the dialect table rather than against literals:
	// what this endpoint owes the caller is the table's own bounds and
	// its own rows, so that is the thing to assert. Literals here meant
	// every added row broke an API test that had nothing to say about
	// the change -- and the fix for that is always to bump the number,
	// which tests nothing.
	if out.RouterOS.Minimum != routeros.MinimumVersion || out.RouterOS.Newest != routeros.NewestVersion() {
		t.Errorf("RouterOS bounds = %+v, want minimum %q and newest %q",
			out.RouterOS, routeros.MinimumVersion, routeros.NewestVersion())
	}
	if len(out.RouterOS.Rows) != len(routeros.Rows) {
		t.Fatalf("RouterOS.Rows has %d entries, want %d -- the endpoint must surface every row",
			len(out.RouterOS.Rows), len(routeros.Rows))
	}
	if !strings.Contains(out.Steps.CaTrust.Commands, "https://mv.example.net:8443/ca.crt") {
		t.Errorf("caTrust commands missing the address: %s", out.Steps.CaTrust.Commands)
	}
	if out.Steps.Push.Commands != "" {
		t.Errorf("push commands = %q, want empty with no token/kinds", out.Steps.Push.Commands)
	}
	if out.Steps.RuleTagging.Note != "" {
		t.Errorf("ruleTagging note = %q, want empty with no version picked", out.Steps.RuleTagging.Note)
	}
}

// TestHandleSetupCommandsPickedVersion covers a version the operator
// picked from the list: its standing, its row's dialect, and -- for
// 7.24.0 specifically -- the row's note surfacing on the ruleTagging
// step.
func TestHandleSetupCommandsPickedVersion(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	out := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "mv.example.net:8443", Version: "7.24"})

	if out.Picked == nil {
		t.Fatal("Picked = nil, want the resolved pick")
	}
	if out.Picked.Version != "7.24" || out.Picked.Standing != "reviewed" || out.Picked.Dialect != "a" {
		t.Errorf("Picked = %+v, want version 7.24, standing reviewed, dialect a", out.Picked)
	}
	if !strings.Contains(out.Steps.RuleTagging.Note, "find") {
		t.Errorf("ruleTagging note = %q, want 7.24's find-lookup-bug note", out.Steps.RuleTagging.Note)
	}
}

// TestHandleSetupCommandsPickedVersionOutsideEveryRow covers a version
// no row covers -- below the floor and ahead of the newest row both --
// which must still resolve to a standing and a (fallback) dialect, never
// an error: the design record says this never blocks.
func TestHandleSetupCommandsPickedVersionOutsideEveryRow(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	below := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "mv.example.net:8443", Version: "7.16"})
	if below.Picked == nil || below.Picked.Standing != "below-minimum" || below.Picked.Dialect != "a" {
		t.Errorf("Picked (below floor) = %+v, want standing below-minimum, dialect a (the fallback)", below.Picked)
	}

	ahead := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "mv.example.net:8443", Version: "7.99"})
	if ahead.Picked == nil || ahead.Picked.Standing != "ahead-of-review" || ahead.Picked.Dialect != "a" {
		t.Errorf("Picked (ahead of newest) = %+v, want standing ahead-of-review, dialect a (the fallback)", ahead.Picked)
	}
}

// TestHandleSetupCommandsListsRoutersWithKnownVersions covers the
// "routers" list: only routers whose version is known appear at all, and
// each carries its own standing and row note.
func TestHandleSetupCommandsListsRoutersWithKnownVersions(t *testing.T) {
	s, _ := newTestServer(t)
	p, err := ingest.DecodePayload(strings.NewReader(
		`{"kind":"arp","page":1,"pages":1,"routerosVersion":"7.24","records":[{"address":"192.0.2.50","mac":"aa:bb:cc:dd:ee:01"}]}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RouterState.Apply("core", p, time.Now()); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(s.mux())
	defer ts.Close()
	out := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "mv.example.net:8443"})

	if len(out.Routers) != 1 {
		t.Fatalf("Routers has %d entries, want exactly the one device with a known version: %+v", len(out.Routers), out.Routers)
	}
	r := out.Routers[0]
	if r.ID != "core" || r.RouterOSVersion != "7.24" || r.Standing != "reviewed" {
		t.Errorf("Routers[0] = %+v, want core at 7.24, reviewed", r)
	}
	if !strings.Contains(r.Note, "find") {
		t.Errorf("Routers[0].Note = %q, want 7.24's find-lookup-bug note carried per-router", r.Note)
	}
}

// TestHandleSetupCommandsPushRendersOnlyWithTokenAndKinds covers the
// contract's rule that push commands are empty when no token/kinds are
// given: both must be present for a push script to render at all.
func TestHandleSetupCommandsPushRendersOnlyWithTokenAndKinds(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	tokenOnly := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "h", Token: "tok-123"})
	if tokenOnly.Steps.Push.Commands != "" {
		t.Errorf("push commands = %q, want empty with a token but no kinds", tokenOnly.Steps.Push.Commands)
	}

	kindsOnly := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "h", Kinds: []string{"filter-rule"}})
	if kindsOnly.Steps.Push.Commands != "" {
		t.Errorf("push commands = %q, want empty with kinds but no token", kindsOnly.Steps.Push.Commands)
	}

	both := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "h", Token: "tok-123", Kinds: []string{"filter-rule"}})
	if !strings.Contains(both.Steps.Push.Commands, "Bearer tok-123") {
		t.Errorf("push commands = %q, want the token embedded", both.Steps.Push.Commands)
	}
}
