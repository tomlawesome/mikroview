// SPDX-License-Identifier: AGPL-3.0-only

package seen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// listValues is what a menu would actually be built from, as a plain
// slice of strings in the order served.
func listValues(r *Register, f Field) []string {
	out := []string{}
	for _, v := range r.List(f) {
		out = append(out, v.Value)
	}
	return out
}

func has(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestObserveRecordsProtocolAndBothInterfaces(t *testing.T) {
	r, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	now := time.Now()

	r.Observe("tcp", "ether1", "bridge-lan", now)

	protos := listValues(r, FieldProto)
	if len(protos) != 1 || protos[0] != "tcp" {
		t.Errorf("proto = %v, want [tcp]", protos)
	}
	// One list, not two: store.Query.Interface matches an event on
	// either its in or its out interface, so both names belong to the
	// same filter and the same menu.
	ifaces := listValues(r, FieldInterface)
	if len(ifaces) != 2 || !has(ifaces, "ether1") || !has(ifaces, "bridge-lan") {
		t.Errorf("interface = %v, want both ether1 and bridge-lan", ifaces)
	}
}

func TestObserveFoldsProtocolCaseAndKeepsInterfaceNamesVerbatim(t *testing.T) {
	r, _ := Open("")
	now := time.Now()

	// Query.Protocol matches with EqualFold, so TCP and tcp are one
	// filter and must be one menu entry. Query.Interface matches
	// exactly, so a folded interface name would select nothing.
	r.Observe("TCP", "Ether1", "", now)
	r.Observe("tcp", "ether1", "", now.Add(2*time.Minute))

	if protos := listValues(r, FieldProto); len(protos) != 1 || protos[0] != "tcp" {
		t.Errorf("proto = %v, want a single folded [tcp]", protos)
	}
	if ifaces := listValues(r, FieldInterface); len(ifaces) != 2 {
		t.Errorf("interface = %v, want Ether1 and ether1 kept apart -- the filter matches exactly", ifaces)
	}
}

func TestObserveSkipsEmptyFields(t *testing.T) {
	r, _ := Open("")

	// The ordinary case for an event that carries no protocol, or that
	// has no outbound interface: absent is not a value.
	r.Observe("", "  ", "", time.Now())

	if got := listValues(r, FieldProto); len(got) != 0 {
		t.Errorf("proto = %v, want nothing recorded for an absent field", got)
	}
	if got := listValues(r, FieldInterface); len(got) != 0 {
		t.Errorf("interface = %v, want nothing recorded for an absent field", got)
	}
}

func TestObserveKeepsFirstSeenAndMovesLastSeen(t *testing.T) {
	r, _ := Open("")
	first := time.Now().Add(-48 * time.Hour)
	later := first.Add(10 * time.Minute)

	r.Observe("udp", "", "", first)
	r.Observe("udp", "", "", later)

	values := r.List(FieldProto)
	if len(values) != 1 {
		t.Fatalf("proto = %v, want one entry", values)
	}
	if !values[0].FirstSeen.Equal(first) {
		t.Errorf("firstSeen = %v, want the earliest sighting %v", values[0].FirstSeen, first)
	}
	if !values[0].LastSeen.Equal(later) {
		t.Errorf("lastSeen = %v, want the latest sighting %v", values[0].LastSeen, later)
	}
}

func TestObserveOnlyReportsAChangeWorthPersisting(t *testing.T) {
	r, _ := Open("")
	now := time.Now()

	if !r.Observe("icmp", "", "", now) {
		t.Error("a first sighting must report a change")
	}
	// A repeat a moment later moves LastSeen by less than the
	// granularity: against a 90-day retention that is not worth
	// re-encoding the whole document for.
	if r.Observe("icmp", "", "", now.Add(time.Second)) {
		t.Error("a repeat within the granularity must not report a change")
	}
	if !r.Observe("icmp", "", "", now.Add(2*time.Minute)) {
		t.Error("a repeat past the granularity must report a change")
	}
}

func TestUnusableValuesAreIgnored(t *testing.T) {
	r, _ := Open("")
	now := time.Now()

	long := ""
	for i := 0; i < maxValueLength+1; i++ {
		long += "a"
	}
	// A bidi/control character written as an escape, never as a literal
	// byte in this file: the point is that such a value is refused, and a
	// value that renders as one thing and sorts as another is exactly
	// what normalise exists to keep out of a menu.
	r.Observe("", long, "ether\x021", now)

	// A clean name alongside them, so the test proves the two were
	// refused rather than that nothing was recorded at all.
	r.Observe("", "ether1", "", now)

	got := listValues(r, FieldInterface)
	if len(got) != 1 || got[0] != "ether1" {
		t.Errorf("interface = %v, want only the clean name -- an oversized one and one carrying a control character are both refused", got)
	}
	if r.Shed() != 2 {
		t.Errorf("shed = %d, want both unusable observations counted", r.Shed())
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen-values.json")
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	last := first.Add(time.Hour)

	r.Observe("tcp", "ether1", "bridge-lan", first)
	r.Observe("tcp", "ether1", "", last)
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the register did not write a document: %v", err)
	}

	r2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = r2.Close(context.Background()) })

	protos := r2.List(FieldProto)
	if len(protos) != 1 || protos[0].Value != "tcp" {
		t.Fatalf("proto after reopen = %v, want [tcp]", protos)
	}
	if !protos[0].FirstSeen.Equal(first) || !protos[0].LastSeen.Equal(last) {
		t.Errorf("proto window after reopen = %v..%v, want %v..%v",
			protos[0].FirstSeen, protos[0].LastSeen, first, last)
	}
	if ifaces := listValues(r2, FieldInterface); len(ifaces) != 2 || !has(ifaces, "ether1") || !has(ifaces, "bridge-lan") {
		t.Errorf("interface after reopen = %v, want both names restored", ifaces)
	}
}

func TestValuesOlderThanMaxAgeExpire(t *testing.T) {
	r, _ := Open("")
	now := time.Now()
	stale := now.Add(-MaxAge - 24*time.Hour)
	fresh := now.Add(-MaxAge + 24*time.Hour)

	r.Observe("", "gone-ages-ago", "still-here", stale)
	// Move the fresh one forward on its own so the two have genuinely
	// different last-seen times either side of the 90-day line.
	r.Observe("", "still-here", "", fresh)

	got := listValues(r, FieldInterface)
	if has(got, "gone-ages-ago") {
		t.Errorf("interface = %v, want a value unseen for over %v left out", got, MaxAge)
	}
	if !has(got, "still-here") {
		t.Errorf("interface = %v, want a value seen inside the window kept", got)
	}

	// Expiry is applied at write time: recording a new value is what
	// actually deletes the aged-out one, not a timer.
	r.Observe("", "brand-new", "", now)
	r.mu.RLock()
	_, stillHeld := r.byName[FieldInterface]["gone-ages-ago"]
	r.mu.RUnlock()
	if stillHeld {
		t.Error("recording a new value must drop the aged-out one, not merely hide it from List")
	}
}

func TestExpiredValuesDoNotSurviveAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen-values.json")
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	now := time.Now()
	r.Observe("", "ancient", "", now.Add(-MaxAge-time.Hour))
	r.Observe("", "recent", "", now)
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = r2.Close(context.Background()) })

	got := listValues(r2, FieldInterface)
	if has(got, "ancient") {
		t.Errorf("interface after reopen = %v, want the aged-out value gone from disk too", got)
	}
	if !has(got, "recent") {
		t.Errorf("interface after reopen = %v, want the live value restored", got)
	}
}

func TestCapEvictsTheLeastRecentlySeen(t *testing.T) {
	r, _ := Open("")
	base := time.Now().Add(-time.Hour)

	// MaxValues distinct names, each seen one second later than the
	// last, so iface-0 is unambiguously the least recently seen.
	for i := 0; i < MaxValues; i++ {
		r.Observe("", fmt.Sprintf("iface-%03d", i), "", base.Add(time.Duration(i)*time.Second))
	}
	if got := len(r.List(FieldInterface)); got != MaxValues {
		t.Fatalf("interface count = %d, want the field filled to %d", got, MaxValues)
	}

	r.Observe("", "one-too-many", "", base.Add(time.Hour))

	got := listValues(r, FieldInterface)
	if len(got) != MaxValues {
		t.Errorf("interface count = %d, want the cap held at %d", len(got), MaxValues)
	}
	if has(got, "iface-000") {
		t.Error("the least recently seen value should have been evicted to make room")
	}
	if !has(got, "iface-001") {
		t.Error("only one value should have been evicted")
	}
	if !has(got, "one-too-many") {
		t.Error("the newly seen value should be in the list")
	}
}

func TestCapIsPerFieldNotShared(t *testing.T) {
	r, _ := Open("")
	base := time.Now().Add(-time.Hour)
	for i := 0; i < MaxValues; i++ {
		r.Observe("", fmt.Sprintf("iface-%03d", i), "", base.Add(time.Duration(i)*time.Second))
	}

	r.Observe("tcp", "", "", base)

	if got := listValues(r, FieldProto); len(got) != 1 || got[0] != "tcp" {
		t.Errorf("proto = %v, want a full interface list to leave the protocol list alone", got)
	}
}

func TestListIsMostRecentlySeenFirst(t *testing.T) {
	r, _ := Open("")
	now := time.Now()
	r.Observe("tcp", "", "", now.Add(-3*time.Hour))
	r.Observe("udp", "", "", now.Add(-time.Hour))
	r.Observe("icmp", "", "", now.Add(-2*time.Hour))

	got := listValues(r, FieldProto)
	want := []string{"udp", "icmp", "tcp"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("proto = %v, want %v -- what the network is doing now belongs at the top", got, want)
			break
		}
	}
}

func TestAllServesEveryFieldEvenWhenEmpty(t *testing.T) {
	r, _ := Open("")
	all := r.All()
	for _, f := range Fields {
		values, ok := all[f]
		if !ok {
			t.Errorf("All() is missing %q -- the API's shape must not depend on what has been seen", f)
			continue
		}
		if values == nil {
			t.Errorf("All()[%q] is nil, want an empty list so it marshals as [] rather than null", f)
		}
	}
}

func TestNilRegisterIsSafeOnTheIngestPath(t *testing.T) {
	var r *Register
	if r.Observe("tcp", "ether1", "", time.Now()) {
		t.Error("a nil register must report no change")
	}
	if got := r.List(FieldProto); got != nil {
		t.Errorf("List on a nil register = %v, want nil", got)
	}
	if all := r.All(); len(all) != len(Fields) {
		t.Errorf("All on a nil register = %v, want every field present and empty", all)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Errorf("Close on a nil register: %v", err)
	}
}

func TestOpenRefusesAnUnreadableDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen-values.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Fail closed (#378): a live register must never silently overwrite
	// a document it could not read.
	if _, err := Open(path); err == nil {
		t.Error("Open on an unparseable document returned no error")
	}
}
