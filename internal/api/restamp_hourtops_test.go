// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/naming"
	"github.com/tomlawesome/mikroview/internal/store"
)

// TestHourTopsFollowsAHostRenameThroughTheRing is #996's second half,
// which needed no code: HourTops has no stored per-name buckets to
// re-key. It tallies talkers from the ring on every call
// (internal/store/ring.go), and #993 already re-stamps the ring on
// entity upsert/delete, so the next poll of /api/stats/tops counts the
// renamed host under its new name. Pinned here so a future move to
// counted-at-insert buckets -- which is what #996 assumed was already
// the case -- cannot reintroduce the stale key unnoticed.
func TestHourTopsFollowsAHostRenameThroughTheRing(t *testing.T) {
	s := newAuthTestServer(t)
	s.Naming = naming.Resolver{Entities: s.Entities}

	now := time.Now()
	// An old anchor event, comfortably before the target minute, so the
	// ring's oldest-held event cannot make that minute read incomplete
	// -- the same setup internal/store's own HourTops tests use.
	s.Store.Insert(store.Event{
		Time: now.Add(-55 * time.Minute), ReceivedAt: now.Add(-55 * time.Minute),
		DeviceID: "core", SrcIP: "10.0.0.1", DstPort: 22,
	})
	s.Store.Insert(store.Event{
		Time: now, ReceivedAt: now, DeviceID: "core",
		SrcIP: "10.0.0.9", SrcHostName: "old-name", DstPort: 443,
	})

	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, ts)

	thisMinute := func() store.HourTop {
		t.Helper()
		tops := s.Store.HourTops()
		return tops[len(tops)-1]
	}

	if got := thisMinute(); got.Talker != "old-name" {
		t.Fatalf("before the rename, Talker = %q (Complete=%v), want %q", got.Talker, got.Complete, "old-name")
	}

	resp := postJSON(t, client, ts.URL+"/api/entities", entityRequest{
		Type: entities.TypeHost, Key: "10.0.0.9", Label: "jellyfish",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upsert status = %d, want 201", resp.StatusCode)
	}

	if got := thisMinute(); got.Talker != "jellyfish" {
		t.Errorf("after the rename, Talker = %q, want %q -- the hour tops still name the host as it was", got.Talker, "jellyfish")
	}
}
