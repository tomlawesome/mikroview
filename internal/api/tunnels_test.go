// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
)

func TestParseRouterOSDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"1m23s", 1*time.Minute + 23*time.Second},
		{"2h13m5s", 2*time.Hour + 13*time.Minute + 5*time.Second},
		{"3s", 3 * time.Second},
		{"50ms", 50 * time.Millisecond},
		{"4w2d5h24m35s", 4*7*24*time.Hour + 2*24*time.Hour + 5*time.Hour + 24*time.Minute + 35*time.Second},
	}
	for _, c := range cases {
		got, err := parseRouterOSDuration(c.in)
		if err != nil {
			t.Errorf("parseRouterOSDuration(%q) = error %v, want %v", c.in, err, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("parseRouterOSDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseRouterOSDurationRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "not a duration", "5", "5x", "1h garbage"} {
		if _, err := parseRouterOSDuration(in); err == nil {
			t.Errorf("parseRouterOSDuration(%q) = nil error, want a rejection", in)
		}
	}
}

func applyIngest(t *testing.T, s *Server, device, body string) {
	t.Helper()
	p, err := ingest.DecodePayload(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodePayload(%s): %v", body, err)
	}
	if err := s.RouterState.Apply(device, p, time.Now()); err != nil {
		t.Fatalf("Apply(%s): %v", body, err)
	}
}

// TestHandleRouterOSWireguardDerivesState is issue #874's reproducer for
// the state-derivation layer: a peer under the three-minute handshake
// window is up, one over it (or one that has never handshaken) is down,
// and the interface is up because at least one of its peers is.
func TestHandleRouterOSWireguardDerivesState(t *testing.T) {
	s, _ := newTestServer(t)
	applyIngest(t, s, "core", `{"kind":"wireguard-interface","page":1,"pages":1,"records":[{"name":"wg0","comment":"","publicKey":"","listenPort":51820}]}`)
	applyIngest(t, s, "core", `{"kind":"wireguard-peer","page":1,"pages":1,"records":[
	  {"publicKey":"recent","allowedAddress":"10.0.0.0/24","endpointAddress":"","comment":"branch","lastHandshake":"1m23s"},
	  {"publicKey":"stale","allowedAddress":"10.0.1.0/24","endpointAddress":"","comment":"old","lastHandshake":"10m0s"},
	  {"publicKey":"never","allowedAddress":"10.0.2.0/24","endpointAddress":"","comment":"new"}
	]}`)

	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/routeros/core/wireguard")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Available      bool `json:"available"`
		PeersAvailable bool `json:"peersAvailable"`
		Interfaces     []struct {
			Name  string `json:"name"`
			State string `json:"state"`
			Peers []struct {
				PublicKey string  `json:"publicKey"`
				State     string  `json:"state"`
				Since     *string `json:"since"`
			} `json:"peers"`
		} `json:"interfaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if !body.Available || !body.PeersAvailable {
		t.Fatalf("Available/PeersAvailable = %v/%v, want true/true", body.Available, body.PeersAvailable)
	}
	if len(body.Interfaces) != 1 {
		t.Fatalf("len(Interfaces) = %d, want 1", len(body.Interfaces))
	}
	iface := body.Interfaces[0]
	if iface.State != "up" {
		t.Errorf("interface State = %q, want %q (at least one peer is up)", iface.State, "up")
	}
	if len(iface.Peers) != 3 {
		t.Fatalf("len(Peers) = %d, want 3", len(iface.Peers))
	}
	states := map[string]string{}
	for _, p := range iface.Peers {
		states[p.PublicKey] = p.State
	}
	if states["recent"] != "up" {
		t.Errorf("recent peer State = %q, want %q", states["recent"], "up")
	}
	if states["stale"] != "down" {
		t.Errorf("stale peer State = %q, want %q", states["stale"], "down")
	}
	if states["never"] != "down" {
		t.Errorf("never-handshaken peer State = %q, want %q", states["never"], "down")
	}
}

// TestHandleRouterOSWireguardUnknownWhenPeerKindNeverPushed covers
// #874's "a kind never pushed yields unknown, never a guess": an
// interface named by wireguard-interface with no wireguard-peer push at
// all must report unknown, not down.
func TestHandleRouterOSWireguardUnknownWhenPeerKindNeverPushed(t *testing.T) {
	s, _ := newTestServer(t)
	applyIngest(t, s, "core", `{"kind":"wireguard-interface","page":1,"pages":1,"records":[{"name":"wg0","comment":"","publicKey":"","listenPort":51820}]}`)

	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/routeros/core/wireguard")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		PeersAvailable bool `json:"peersAvailable"`
		Interfaces     []struct {
			State string `json:"state"`
		} `json:"interfaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.PeersAvailable {
		t.Error("PeersAvailable = true, want false -- wireguard-peer was never pushed")
	}
	if len(body.Interfaces) != 1 || body.Interfaces[0].State != "unknown" {
		t.Errorf("Interfaces = %+v, want one interface with State \"unknown\"", body.Interfaces)
	}
}

// TestHandleRouterOSWireguardNoDataAtAll covers a device that has never
// pushed either WireGuard kind: available/peersAvailable both false, no
// interfaces at all, and a caller reads that as unknown rather than
// "down" or "no WireGuard configured".
func TestHandleRouterOSWireguardNoDataAtAll(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/routeros/core/wireguard")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Available      bool          `json:"available"`
		PeersAvailable bool          `json:"peersAvailable"`
		Interfaces     []interface{} `json:"interfaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Available || body.PeersAvailable {
		t.Error("Available/PeersAvailable = true, want both false -- nothing has ever been pushed")
	}
	if body.Interfaces == nil {
		t.Error("Interfaces = null, want an empty array")
	}
	if len(body.Interfaces) != 0 {
		t.Errorf("len(Interfaces) = %d, want 0", len(body.Interfaces))
	}
}

// wireguardTestBody decodes a wireguard tunnels response body just far
// enough for the attribution tests below: which peer public keys landed
// under which named interface, and that interface's own derived state.
type wireguardTestBody struct {
	Interfaces []struct {
		Name  string `json:"name"`
		State string `json:"state"`
		Peers []struct {
			PublicKey string `json:"publicKey"`
		} `json:"peers"`
	} `json:"interfaces"`
}

func peerKeys(t *testing.T, peers []struct {
	PublicKey string `json:"publicKey"`
}) []string {
	t.Helper()
	out := make([]string, len(peers))
	for i, p := range peers {
		out[i] = p.PublicKey
	}
	return out
}

// TestHandleRouterOSWireguardAttributesPeersByInterfaceField is the
// follow-up fixing the gap the original #874 cut flagged: once a peer
// names its interface, a two-interface device must not show one
// interface's peers under the other, and each interface's own State
// must depend only on the peers actually attributed to it.
func TestHandleRouterOSWireguardAttributesPeersByInterfaceField(t *testing.T) {
	s, _ := newTestServer(t)
	applyIngest(t, s, "core", `{"kind":"wireguard-interface","page":1,"pages":1,"records":[
	  {"name":"wg0","comment":"","publicKey":"","listenPort":51820},
	  {"name":"wg1","comment":"","publicKey":"","listenPort":51821}
	]}`)
	applyIngest(t, s, "core", `{"kind":"wireguard-peer","page":1,"pages":1,"records":[
	  {"publicKey":"wg0-up","allowedAddress":"10.0.0.0/24","endpointAddress":"","comment":"","interface":"wg0","lastHandshake":"1m0s"},
	  {"publicKey":"wg1-down","allowedAddress":"10.0.1.0/24","endpointAddress":"","comment":"","interface":"wg1","lastHandshake":"10m0s"}
	]}`)

	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/routeros/core/wireguard")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body wireguardTestBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Interfaces) != 2 {
		t.Fatalf("len(Interfaces) = %d, want 2", len(body.Interfaces))
	}
	byName := map[string]struct {
		state string
		keys  []string
	}{}
	for _, iface := range body.Interfaces {
		byName[iface.Name] = struct {
			state string
			keys  []string
		}{iface.State, peerKeys(t, iface.Peers)}
	}

	if got := byName["wg0"]; got.state != "up" || len(got.keys) != 1 || got.keys[0] != "wg0-up" {
		t.Errorf("wg0 = %+v, want state up with only wg0-up attached", got)
	}
	if got := byName["wg1"]; got.state != "down" || len(got.keys) != 1 || got.keys[0] != "wg1-down" {
		t.Errorf("wg1 = %+v, want state down with only wg1-down attached", got)
	}
}

// TestHandleRouterOSWireguardFallsBackWithoutInterfaceField pins the
// pre-follow-up behaviour for a push script that has never sent the
// interface field on any peer: every peer still attaches to every
// interface, rather than to none.
func TestHandleRouterOSWireguardFallsBackWithoutInterfaceField(t *testing.T) {
	s, _ := newTestServer(t)
	applyIngest(t, s, "core", `{"kind":"wireguard-interface","page":1,"pages":1,"records":[
	  {"name":"wg0","comment":"","publicKey":"","listenPort":51820},
	  {"name":"wg1","comment":"","publicKey":"","listenPort":51821}
	]}`)
	applyIngest(t, s, "core", `{"kind":"wireguard-peer","page":1,"pages":1,"records":[
	  {"publicKey":"p1","allowedAddress":"10.0.0.0/24","endpointAddress":"","comment":"","lastHandshake":"1m0s"},
	  {"publicKey":"p2","allowedAddress":"10.0.1.0/24","endpointAddress":"","comment":""}
	]}`)

	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/routeros/core/wireguard")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body wireguardTestBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Interfaces) != 2 {
		t.Fatalf("len(Interfaces) = %d, want 2", len(body.Interfaces))
	}
	for _, iface := range body.Interfaces {
		if len(iface.Peers) != 2 {
			t.Errorf("interface %s has %d peers, want 2 (attribution unavailable, every peer attaches to every interface)", iface.Name, len(iface.Peers))
		}
		if iface.State != "up" {
			t.Errorf("interface %s State = %q, want %q", iface.Name, iface.State, "up")
		}
	}
}

// TestHandleRouterOSPPPActive covers issue #874's second table: a
// session present in the push is state "up", with Since estimated from
// the reported uptime.
func TestHandleRouterOSPPPActive(t *testing.T) {
	s, _ := newTestServer(t)
	applyIngest(t, s, "core", `{"kind":"ppp-active","page":1,"pages":1,"records":[
	  {"name":"branch-l2tp","service":"l2tp","address":"10.20.0.5","callerId":"203.0.113.44","uptime":"1h0m0s"}
	]}`)

	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/routeros/core/ppp-active")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Available bool `json:"available"`
		Sessions  []struct {
			Name  string  `json:"name"`
			State string  `json:"state"`
			Since *string `json:"since"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Available {
		t.Fatal("Available = false, want true")
	}
	if len(body.Sessions) != 1 {
		t.Fatalf("len(Sessions) = %d, want 1", len(body.Sessions))
	}
	if body.Sessions[0].Name != "branch-l2tp" || body.Sessions[0].State != "up" {
		t.Errorf("Sessions[0] = %+v, want name branch-l2tp, state up", body.Sessions[0])
	}
	if body.Sessions[0].Since == nil {
		t.Error("Since = nil, want an estimated start time from the parsed uptime")
	}
}

// TestHandleRouterOSPPPActiveNoDataAtAll covers a device that has never
// pushed ppp-active: available false, and an empty (never null) array
// -- absence of a push is not the same claim as "no sessions are up".
func TestHandleRouterOSPPPActiveNoDataAtAll(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/routeros/core/ppp-active")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Available bool          `json:"available"`
		Sessions  []interface{} `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Available {
		t.Error("Available = true, want false -- ppp-active was never pushed")
	}
	if body.Sessions == nil {
		t.Error("Sessions = null, want an empty array")
	}
}
