// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/decommission"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/store"
)

// -- #460's surface: the offer, the ghost, and the way back -----------

// decommissionServer is newTestServer with a decommission store wired
// in. newTestServer leaves Decommissions nil (the "not available on this
// deployment" case every handler answers 503 to), so every test here
// starts by supplying one.
func decommissionServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s, st := newTestServer(t)
	ds, err := decommission.Open("")
	if err != nil {
		t.Fatalf("opening an unpersisted decommission store: %v", err)
	}
	s.Decommissions = ds
	return s, st
}

// pushIPAddresses puts one /ip/address cycle into RouterState the same
// way a real push does -- through Apply, so the departure bookkeeping
// (which needs two complete cycles before it will call anything gone)
// is the real one rather than a reach into the store.
func pushIPAddresses(t *testing.T, s *Server, device string, rows ...ingest.IPAddressEntry) {
	t.Helper()
	if err := s.RouterState.Apply(device, ingest.Payload{
		Kind:        ingest.KindIPAddress,
		Page:        1,
		Pages:       1,
		IPAddresses: rows,
	}, time.Now()); err != nil {
		t.Fatalf("Apply(%s, ip-address): %v", device, err)
	}
}

// addWatch stores one watch directly. The creation path has the offer in
// front of it (handleDecommissionCreate refuses a range nobody retired),
// and these tests are about what happens after a watch exists.
func addWatch(t *testing.T, s *Server, cidr string, createdAt time.Time) decommission.Watch {
	t.Helper()
	w, err := s.Decommissions.Add(decommission.Watch{
		CIDR:        cidr,
		Device:      "core",
		Interface:   "ether3",
		Name:        "lab",
		CreatedAt:   createdAt,
		CleanWindow: 6 * time.Hour,
		Covered:     true,
		LastKnown:   []decommission.Straggler{{Address: "192.0.2.7", Name: "garage-cam", SeenAt: createdAt}},
	})
	if err != nil {
		t.Fatalf("adding a watch for %s: %v", cidr, err)
	}
	return w
}

// getDecommission reads the whole surface back as a viewer -- the tier
// GET /api/decommission is served at.
func getDecommission(t *testing.T, ts *httptest.Server) decommissionResponse {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/decommission")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/decommission = %d, want 200", resp.StatusCode)
	}
	var body decommissionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

// postUndo asks for the retirement back.
func postUndo(t *testing.T, ts *httptest.Server, id string) *http.Response {
	t.Helper()
	resp, err := http.Post(ts.URL+"/api/decommission/watches/"+id+"/undo", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestDecommissionServesTheOfferWithItsReceiptAndTheWatchesBeside(t *testing.T) {
	// One call for the whole surface: what is being offered, and what is
	// already being watched. The offer's receipt is the persuasive half
	// -- "would have caught 3" plus which device it means.
	s, st := decommissionServer(t)
	lab := ingest.IPAddressEntry{Address: "192.0.2.1/24", Network: "192.0.2.0", Interface: "ether3", Comment: "lab"}
	desks := ingest.IPAddressEntry{Address: "198.51.100.1/24", Network: "198.51.100.0", Interface: "ether4", Comment: "desks"}
	if err := s.RouterState.Apply("core", ingest.Payload{
		Kind: ingest.KindDHCPLease, Page: 1, Pages: 1,
		DHCPLeases: []ingest.DHCPLease{{Address: "192.0.2.7", Hostname: "garage-cam", MAC: "aa:bb:cc:dd:ee:ff"}},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	pushIPAddresses(t, s, "core", lab, desks)
	pushIPAddresses(t, s, "core", desks)

	// Three lines from the named address and one from an address no push
	// ever covered, so the receipt has both an order to report and the
	// absence rule to obey.
	for i := 0; i < 3; i++ {
		st.Insert(store.Event{DeviceID: "core", Action: store.ActionAccept, Protocol: "tcp", Time: time.Now(),
			SrcIP: "192.0.2.7", DstIP: "10.0.20.5", DstPort: 445})
	}
	st.Insert(store.Event{DeviceID: "core", Action: store.ActionDrop, Protocol: "tcp", Time: time.Now(),
		SrcIP: "192.0.2.9", DstIP: "10.0.20.5", DstPort: 22})

	watch := addWatch(t, s, "203.0.113.0/24", time.Now().Add(-time.Hour))

	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()
	body := getDecommission(t, ts)

	if len(body.Offers) != 1 {
		t.Fatalf("got %d offers, want the one departed range: %+v", len(body.Offers), body.Offers)
	}
	offer := body.Offers[0]
	if offer.CIDR != "192.0.2.0/24" || offer.Name != "lab" || offer.Interface != "ether3" {
		t.Errorf("offer = %+v, want the lab range on ether3", offer)
	}
	if offer.Receipt == nil {
		t.Fatalf("the offer arrived without its receipt: %+v", offer)
	}
	if offer.Receipt.EmissionCount != 4 {
		t.Errorf("receipt caught %d lines, want 4", offer.Receipt.EmissionCount)
	}
	want := []decommissionReplayAddressView{
		{Address: "192.0.2.7", Name: "garage-cam", Count: 3},
		{Address: "192.0.2.9", Count: 1},
	}
	if len(offer.Receipt.Addresses) != len(want) {
		t.Fatalf("receipt names %+v, want %+v", offer.Receipt.Addresses, want)
	}
	for i, w := range want {
		if offer.Receipt.Addresses[i] != w {
			t.Errorf("receipt address %d = %+v, want %+v", i, offer.Receipt.Addresses[i], w)
		}
	}

	if len(body.Watches) != 1 || body.Watches[0].ID != watch.ID {
		t.Fatalf("got %d watches, want the one being watched: %+v", len(body.Watches), body.Watches)
	}
	if got := body.Watches[0].RetiresIn; got != "5h0m0s" {
		t.Errorf("retiresIn = %q, want 5h0m0s of the six-hour window left", got)
	}
	if !body.Watches[0].UndoableUntil.IsZero() {
		t.Errorf("a watch that has not retired carries undoableUntil %s, want it absent", body.Watches[0].UndoableUntil)
	}
}

func TestARetiredWatchCarriesItsUndoDeadline(t *testing.T) {
	// Retirement is silent, so the deadline is how a surface knows
	// whether the undo is still on offer -- without restating the hour
	// the store owns.
	s, _ := decommissionServer(t)
	watch := addWatch(t, s, "192.0.2.0/24", time.Now().Add(-7*time.Hour))
	retired := s.Decommissions.Sweep(time.Now())
	if len(retired) != 1 {
		t.Fatalf("sweep retired %d watches, want 1", len(retired))
	}

	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()
	body := getDecommission(t, ts)

	if len(body.Watches) != 1 {
		t.Fatalf("got %d watches, want 1", len(body.Watches))
	}
	got := body.Watches[0]
	if got.ID != watch.ID || got.State != decommission.StateRetired {
		t.Fatalf("watch = %s in state %q, want the retired one", got.ID, got.State)
	}
	wantUntil := retired[0].RetiredAt.Add(decommission.UndoWindow)
	if !got.UndoableUntil.Equal(wantUntil) {
		t.Errorf("undoableUntil = %s, want retirement plus %s (%s)", got.UndoableUntil, decommission.UndoWindow, wantUntil)
	}
}

func TestUndoTakesTheRetirementBack(t *testing.T) {
	s, _ := decommissionServer(t)
	// Created six and a half hours ago on a six-hour window, so the
	// retirement is stamped half an hour ago -- inside the offer.
	watch := addWatch(t, s, "192.0.2.0/24", time.Now().Add(-6*time.Hour-30*time.Minute))
	if len(s.Decommissions.Sweep(time.Now())) != 1 {
		t.Fatal("the watch did not retire, so there is nothing to undo")
	}

	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()
	resp := postUndo(t, ts, watch.ID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo = %d, want 200", resp.StatusCode)
	}
	var view decommissionWatchView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if !view.RetiredAt.IsZero() {
		t.Errorf("the restored watch still carries retiredAt %s", view.RetiredAt)
	}
	if !view.UndoableUntil.IsZero() {
		t.Errorf("undoableUntil = %s, want it gone with the retirement", view.UndoableUntil)
	}
	// Draining, not holding: the undo restarts the clock by writing the
	// moment of the undo to LastTrafficAt, and that field is what
	// StateAt reads to separate draining from holding.
	if view.State != decommission.StateDraining {
		t.Errorf("state = %q, want %q", view.State, decommission.StateDraining)
	}
	if got := view.RetiresIn; got != "6h0m0s" {
		t.Errorf("retiresIn = %q, want a fresh full window -- a restored watch earns its retirement again", got)
	}

	// The store agrees, so the next paint says the same thing.
	back, ok := s.Decommissions.Get(watch.ID)
	if !ok || !back.RetiredAt.IsZero() {
		t.Errorf("the stored watch is %+v, want it un-retired", back)
	}
	if len(s.Decommissions.Active()) != 1 {
		t.Error("the restored watch is not active again, so nothing would watch the range")
	}
}

func TestUndoRefusesWhatItCannotTakeBack(t *testing.T) {
	tests := []struct {
		name string
		// setup returns the id to ask about, having put the store into
		// the state the refusal is about.
		setup func(t *testing.T, s *Server) string
		want  int
	}{
		{
			name: "a watch that never retired",
			setup: func(t *testing.T, s *Server) string {
				return addWatch(t, s, "192.0.2.0/24", time.Now()).ID
			},
			want: http.StatusConflict,
		},
		{
			name: "a retirement older than the hour",
			setup: func(t *testing.T, s *Server) string {
				w := addWatch(t, s, "192.0.2.0/24", time.Now().Add(-9*time.Hour))
				// Retirement is stamped at the instant the window
				// elapsed, so a watch created nine hours ago retired
				// three hours ago -- past the offer.
				if len(s.Decommissions.Sweep(time.Now())) != 1 {
					t.Fatal("the watch did not retire")
				}
				return w.ID
			},
			want: http.StatusConflict,
		},
		{
			name: "a watch that does not exist",
			setup: func(*testing.T, *Server) string {
				return "watch-that-never-existed"
			},
			want: http.StatusNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := decommissionServer(t)
			id := tc.setup(t, s)
			ts := httptest.NewServer(asUser(s.mux()))
			defer ts.Close()

			resp := postUndo(t, ts, id)
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("undo = %d, want %d", resp.StatusCode, tc.want)
			}
			if tc.want == http.StatusConflict {
				if w, ok := s.Decommissions.Get(id); ok && !w.RetiredAt.IsZero() {
					// A refused undo must leave the record alone.
					if !w.LastTrafficAt.IsZero() {
						t.Errorf("a refused undo restarted the clock at %s", w.LastTrafficAt)
					}
				}
			}
		})
	}
}

func TestUndoIsUserTier(t *testing.T) {
	s, _ := decommissionServer(t)
	watch := addWatch(t, s, "192.0.2.0/24", time.Now().Add(-7*time.Hour))
	s.Decommissions.Sweep(time.Now())

	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()
	resp := postUndo(t, ts, watch.ID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("undo as a viewer = %d, want 403", resp.StatusCode)
	}
	if w, _ := s.Decommissions.Get(watch.ID); w.RetiredAt.IsZero() {
		t.Error("a viewer's refused undo un-retired the watch anyway")
	}
}

func TestTheLastStragglersDetailReachesTheWire(t *testing.T) {
	// The callout and the card #485 draws: which address in the dead
	// range is still talking, to whom, on what, through which rule --
	// not merely that something did.
	s, _ := decommissionServer(t)
	watch := addWatch(t, s, "192.0.2.0/24", time.Now().Add(-time.Hour))
	at := time.Now()
	hit := s.Decommissions.RecordTraffic("192.0.2.7", "10.0.20.5", at, decommission.Observation{
		Protocol:  "tcp",
		Port:      445,
		Interface: "ether3",
		Rule:      "iot-out",
		Action:    string(store.ActionAccept),
	})
	if len(hit) != 1 {
		t.Fatalf("recording traffic hit %d watches, want the one whose range it touched", len(hit))
	}

	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()
	body := getDecommission(t, ts)

	if len(body.Watches) != 1 || body.Watches[0].ID != watch.ID {
		t.Fatalf("got %d watches, want the one that took the straggler", len(body.Watches))
	}
	got := body.Watches[0].LastStraggler
	if got == nil {
		t.Fatal("the watch reached the wire with no straggler detail, so the card has nothing to draw")
	}
	want := decommission.Sighting{
		Address:   "192.0.2.7",
		Name:      "garage-cam",
		Peer:      "10.0.20.5",
		Protocol:  "tcp",
		Port:      445,
		Interface: "ether3",
		Rule:      "iot-out",
		Action:    "accept",
		At:        at,
	}
	if !got.At.Equal(want.At) {
		t.Errorf("lastStraggler.at = %s, want %s", got.At, want.At)
	}
	got.At, want.At = time.Time{}, time.Time{}
	if *got != want {
		t.Errorf("lastStraggler = %+v, want %+v", *got, want)
	}
	if body.Watches[0].TrafficCount != 1 {
		t.Errorf("trafficCount = %d, want 1 beside the detail", body.Watches[0].TrafficCount)
	}
}
