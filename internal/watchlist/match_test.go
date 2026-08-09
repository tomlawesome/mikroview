// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
)

func baseEvent() store.Event {
	return store.Event{
		Action:  store.ActionAccept,
		SrcIP:   "192.168.1.50",
		SrcMAC:  "aa:bb:cc:dd:ee:ff",
		DstIP:   "10.0.0.5",
		DstPort: 22,
	}
}

func TestMatchOnPort(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22, 2222}}

	tuple, ok := Match(entry, baseEvent())
	if !ok {
		t.Fatal("expected a match on a listed port")
	}
	if tuple.Port != 22 || tuple.DestIP != "10.0.0.5" {
		t.Errorf("unexpected tuple: %+v", tuple)
	}

	e := baseEvent()
	e.DstPort = 443
	if _, ok := Match(entry, e); ok {
		t.Error("matched a port not in the entry's list")
	}
}

func TestMatchRequiresTrackableConnState(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}}

	e := baseEvent()
	e.ConnState = "new"
	if _, ok := Match(entry, e); !ok {
		t.Error("connState=new should be trackable")
	}

	e.ConnState = ""
	if _, ok := Match(entry, e); !ok {
		t.Error("connState=\"\" (unset) should be trackable")
	}

	e.ConnState = "established"
	if _, ok := Match(entry, e); ok {
		t.Error("connState=established (return traffic on an accepted service) must not match")
	}
}

func TestMatchWithNoDstPortNeverMatches(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}}
	e := baseEvent()
	e.DstPort = 0
	if _, ok := Match(entry, e); ok {
		t.Error("an event with no destination port must never match")
	}
}

func TestMatchUnscopedEntryMatchesAnySource(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}} // no Source set

	e1 := baseEvent()
	e1.SrcMAC, e1.SrcIP = "11:11:11:11:11:11", "192.168.1.1"
	e2 := baseEvent()
	e2.SrcMAC, e2.SrcIP = "22:22:22:22:22:22", "192.168.1.2"

	t1, ok1 := Match(entry, e1)
	t2, ok2 := Match(entry, e2)
	if !ok1 || !ok2 {
		t.Fatal("an unscoped entry should match any source")
	}
	// The tuple must carry each event's OWN identity, not the entry's
	// (empty) Source -- an unscoped entry watching many devices produces
	// one matchlog record per device, not a shared one.
	if t1.Source.MAC != "11:11:11:11:11:11" || t2.Source.MAC != "22:22:22:22:22:22" {
		t.Errorf("tuples did not carry the matching events' own identities: %+v / %+v", t1, t2)
	}
}

func TestMatchScopedBySourceMAC(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}}

	e := baseEvent() // SrcMAC = aa:bb:cc:dd:ee:ff
	if _, ok := Match(entry, e); !ok {
		t.Error("expected a match: event's MAC equals the entry's scoped MAC")
	}

	other := baseEvent()
	other.SrcMAC = "99:99:99:99:99:99"
	if _, ok := Match(entry, other); ok {
		t.Error("a different MAC must not match a MAC-scoped entry")
	}
}

// The whole point of MAC-preferred identity (#243 section 1): a
// MAC-scoped entry must keep matching a device even after its IP
// changes under DHCP.
func TestMatchScopedBySourceMACSurvivesIPChange(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}}

	e := baseEvent()
	e.SrcIP = "192.168.1.200" // different IP, same MAC
	if _, ok := Match(entry, e); !ok {
		t.Error("a MAC-scoped entry must match regardless of the event's IP")
	}
}

func TestMatchScopedBySourceIPFallback(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}, Source: matchlog.Identity{IP: "192.168.1.50"}}

	e := baseEvent()
	e.SrcMAC = "" // no MAC known for this chain -- IP fallback
	if _, ok := Match(entry, e); !ok {
		t.Error("expected a match via IP fallback when the entry has no MAC")
	}

	other := baseEvent()
	other.SrcMAC = ""
	other.SrcIP = "192.168.1.99"
	if _, ok := Match(entry, other); ok {
		t.Error("a different IP must not match an IP-scoped entry")
	}
}

func TestMatchEventWithNoIdentityNeverMatches(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}} // unscoped

	e := baseEvent()
	e.SrcMAC, e.SrcIP = "", "" // nothing to attribute this to
	if _, ok := Match(entry, e); ok {
		t.Error("an event with neither MAC nor IP must never match, even an unscoped entry")
	}
}

func TestMatchScopedByDestIP(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}, DestIP: "10.0.0.5"}

	if _, ok := Match(entry, baseEvent()); !ok {
		t.Error("expected a match: event's DstIP equals the entry's scoped DestIP")
	}

	other := baseEvent()
	other.DstIP = "10.0.0.6"
	if _, ok := Match(entry, other); ok {
		t.Error("a different destination must not match a dest-scoped entry")
	}
}

func TestMatchRequiresAllScopesTogether(t *testing.T) {
	entry := Entry{
		ID:     "e1",
		Ports:  []int{22},
		Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
		DestIP: "10.0.0.5",
	}

	if _, ok := Match(entry, baseEvent()); !ok {
		t.Fatal("expected a match: source, dest and port all satisfy the entry")
	}

	wrongDest := baseEvent()
	wrongDest.DstIP = "10.0.0.9"
	if _, ok := Match(entry, wrongDest); ok {
		t.Error("right source/port but wrong dest must not match a dest-scoped entry")
	}

	wrongSource := baseEvent()
	wrongSource.SrcMAC = "99:99:99:99:99:99"
	if _, ok := Match(entry, wrongSource); ok {
		t.Error("right dest/port but wrong source must not match a source-scoped entry")
	}
}
