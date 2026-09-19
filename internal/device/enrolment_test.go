// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
)

// TestCreateDeclaresANamelessDevice is POST /api/devices' backing rule:
// a device with no address at all, ready to be enrolled.
func TestCreateDeclaresANamelessDevice(t *testing.T) {
	r := NewRegistry(nil)
	info, err := r.Create("hap-ax3", "Hap AX3", time.Now())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if info.ID != "hap-ax3" || info.Name != "Hap AX3" || info.SourceIP != "" || info.AcceptedIP != "" {
		t.Errorf("Create() = %+v, want a bare device with no address", info)
	}
	if _, err := r.Create("hap-ax3", "Duplicate", time.Now()); err != ErrDeviceExists {
		t.Errorf("Create() on an existing id = %v, want ErrDeviceExists", err)
	}
}

// TestDeleteClearsTheDeviceAndItsAddress is the "deleting a device
// clears its address" rule: once deleted, its former AcceptedIP is free
// for another device to be enrolled at.
func TestDeleteClearsTheDeviceAndItsAddress(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	enrolAt(t, r, "hap-ax3", "10.10.0.1")

	if err := r.Delete("hap-ax3"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(r.List()) != 0 {
		t.Errorf("List() after Delete = %+v, want empty", r.List())
	}
	if r.Allowed("10.10.0.1") {
		t.Errorf("Allowed(%q) = true after the device that claimed it was deleted, want false", "10.10.0.1")
	}

	if _, err := r.Create("other", "other", now); err != nil {
		t.Fatal(err)
	}
	enrolAt(t, r, "other", "10.10.0.1")
	if id := r.Resolve("10.10.0.1", now); id != "other" {
		t.Errorf("Resolve() = %q, want the address free to re-enrol to a different device", id)
	}
}

// TestDeleteRefusesAConfiguredDevice: a config.yaml declaration is
// recreated on every boot regardless, so deleting it via the API would
// only reappear confusingly on restart.
func TestDeleteRefusesAConfiguredDevice(t *testing.T) {
	r := NewRegistry([]config.Device{{ID: "core", SourceIP: "192.168.1.1"}})
	if err := r.Delete("core"); err != ErrDeviceConfigured {
		t.Errorf("Delete() on a configured device = %v, want ErrDeviceConfigured", err)
	}
}

func TestDeleteUnknownDeviceNotFound(t *testing.T) {
	r := NewRegistry(nil)
	if err := r.Delete("nope"); err != ErrDeviceNotFound {
		t.Errorf("Delete() on an unknown device = %v, want ErrDeviceNotFound", err)
	}
}

// TestMintEnrolmentReplacesAnyPendingToken is the "Reroll" affordance:
// minting again invalidates the previous token outright rather than
// letting either one redeem.
func TestMintEnrolmentReplacesAnyPendingToken(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}

	first, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("two mints produced the same token")
	}

	if r.TryEnrol("10.10.0.1", []byte("mikroview-enrol "+first)) {
		t.Errorf("the first, superseded token redeemed -- want it invalidated by the second mint")
	}
	if !r.TryEnrol("10.10.0.1", []byte("mikroview-enrol "+second)) {
		t.Errorf("the current token failed to redeem")
	}
}

// TestTokenIsSingleUse: redeeming a token burns it, so replaying the
// same line a second time (a duplicate delivery, or an attacker who
// captured it) does nothing.
func TestTokenIsSingleUse(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	token, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	line := []byte("mikroview-enrol " + token)
	if !r.TryEnrol("10.10.0.1", line) {
		t.Fatalf("first redemption failed")
	}
	if r.TryEnrol("10.10.0.2", line) {
		t.Errorf("a burned token redeemed a second time, at a different address")
	}
	if r.Allowed("10.10.0.2") {
		t.Errorf("the second address became allowed from a replayed, already-burned token")
	}
}

// TestTokenExpires: a token minted more than 15 minutes ago no longer
// redeems.
func TestTokenExpires(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	token, expiresAt, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	if !expiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Errorf("expiresAt = %v, want exactly 15 minutes from mint time", expiresAt)
	}

	// TryEnrol reads the wall clock directly, so simulate lateness by
	// minting far enough in the past that "now" (real time.Now, a
	// moment from now) is already past expiry -- the mint call accepts
	// any reference instant, including one in the past, precisely so
	// this is testable without a fake clock.
	longAgo := now.Add(-16 * time.Minute)
	staleToken, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", longAgo)
	if err != nil {
		t.Fatal(err)
	}
	if r.TryEnrol("10.10.0.1", []byte("mikroview-enrol "+staleToken)) {
		t.Errorf("an expired token redeemed")
	}
	_ = token
}

// TestBurnEnrolmentRevokesAPendingToken is DELETE
// /api/devices/{id}/enrolment's backing rule.
func TestBurnEnrolmentRevokesAPendingToken(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	token, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.BurnEnrolment("hap-ax3"); err != nil {
		t.Fatalf("BurnEnrolment: %v", err)
	}
	if r.TryEnrol("10.10.0.1", []byte("mikroview-enrol "+token)) {
		t.Errorf("a burned pending token still redeemed")
	}
	if err := r.BurnEnrolment("hap-ax3"); err != ErrNoPendingEnrolment {
		t.Errorf("BurnEnrolment on an already-burned device = %v, want ErrNoPendingEnrolment", err)
	}
	if err := r.BurnEnrolment("nope"); err != ErrDeviceNotFound {
		t.Errorf("BurnEnrolment on an unknown device = %v, want ErrDeviceNotFound", err)
	}
}

// TestVerifyPendingTokenChecksHashAndExpiry is POST /api/setup/commands'
// gate before it ever embeds a caller-echoed raw token into a rendered
// RouterOS command.
func TestVerifyPendingTokenChecksHashAndExpiry(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	token, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.VerifyPendingToken("hap-ax3", token, now) {
		t.Errorf("VerifyPendingToken() = false for the current token, want true")
	}
	if r.VerifyPendingToken("hap-ax3", "wrong-token-entirely-00", now) {
		t.Errorf("VerifyPendingToken() = true for an unrelated value")
	}
	if r.VerifyPendingToken("hap-ax3", token, now.Add(16*time.Minute)) {
		t.Errorf("VerifyPendingToken() = true past the token's 15-minute life")
	}
	// Verifying must not itself consume the token -- rendering a command
	// has to be safe to repeat.
	if !r.TryEnrol("10.10.0.1", []byte("mikroview-enrol "+token)) {
		t.Errorf("token failed to redeem after being merely verified")
	}
}

// TestAllowedIsTrueForConfiguredAndAcceptedAddresses: the listener
// gate's fast path.
func TestAllowedIsTrueForConfiguredAndAcceptedAddresses(t *testing.T) {
	r := NewRegistry([]config.Device{{ID: "core", SourceIP: "192.168.1.1"}})
	if !r.Allowed("192.168.1.1") {
		t.Errorf("Allowed() = false for a config.yaml sourceIp, want true -- no token needed")
	}
	if r.Allowed("10.10.0.1") {
		t.Errorf("Allowed() = true for an address nothing has enrolled yet")
	}

	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	enrolAt(t, r, "hap-ax3", "10.10.0.1")
	if !r.Allowed("10.10.0.1") {
		t.Errorf("Allowed() = false once the device was enrolled at that address")
	}
}

// TestRefuseCountsRejectedLinesAndAllowsTheConnectionOn is GET
// /api/devices/refused's backing rule.
func TestRefuseCountsRejectedLinesAndAllowsTheConnectionOn(t *testing.T) {
	r := NewRegistry(nil)
	r.Refuse("10.10.0.1", []byte("not an enrol line"))
	r.Refuse("10.10.0.1", []byte("still not one"))

	got := r.Refused()
	if len(got) != 1 || got[0].Address != "10.10.0.1" || got[0].Lines != 2 {
		t.Fatalf("Refused() = %+v, want one address with two lines", got)
	}
	if got[0].FirstSeen.IsZero() || got[0].LastSeen.IsZero() {
		t.Errorf("Refused() = %+v, want first/last seen set", got)
	}
}

// TestRefuseConnectionCountsIntoTheSameRefusedListAsRefuse is issue
// #1281's connection gate feeding the same GET /api/devices/refused
// list a refused line already does: a connection refused at accept
// (RefuseConnection) and a line refused after accept (Refuse) against
// the same address land in the one entry, and Lines counts both kinds
// -- the JSON field is not renamed or split for the new source.
func TestRefuseConnectionCountsIntoTheSameRefusedListAsRefuse(t *testing.T) {
	r := NewRegistry(nil)
	r.RefuseConnection("10.10.0.1")
	r.Refuse("10.10.0.1", []byte("not an enrol line"))

	got := r.Refused()
	if len(got) != 1 || got[0].Address != "10.10.0.1" || got[0].Lines != 2 {
		t.Fatalf("Refused() = %+v, want one address with Lines counting one connection refusal and one line refusal", got)
	}
}

// TestAcceptsConnectionFromIsFalseWithNoPendingTokens is the closed-port
// default: a registry with no device ever having minted a token has
// nothing pending, so the syslog port must not stay open to unknown
// addresses.
func TestAcceptsConnectionFromIsFalseWithNoPendingTokens(t *testing.T) {
	r := NewRegistry(nil)
	if r.AcceptsConnectionFrom("10.10.0.1") {
		t.Error("AcceptsConnectionFrom() = true, want false with nothing pending")
	}
}

// TestAcceptsConnectionFromIsTrueForTheAddressATokenWasMintedFor is issue #1281's
// connection gate opening: while any device has an unexpired pending
// enrolment token, the port must accept connections from addresses it
// does not yet recognise, since the enrol line proving the token has to
// be able to arrive from exactly such an address.
func TestAcceptsConnectionFromIsTrueForTheAddressATokenWasMintedFor(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now); err != nil {
		t.Fatalf("MintEnrolment: %v", err)
	}
	if !r.AcceptsConnectionFrom("10.10.0.1") {
		t.Error("AcceptsConnectionFrom() = false, want true for the address the pending token was minted for")
	}
}

// TestAcceptsConnectionFromIsFalseOnceTheOnlyPendingTokenExpires mints a token
// far enough in the past (mirroring TestTokenExpires) that it has
// already expired against the real clock, and checks the port closes
// back up: a token's 15-minute life, not its mere existence, is what
// keeps the port open.
func TestAcceptsConnectionFromIsFalseOnceTheOnlyPendingTokenExpires(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatalf("Create: %v", err)
	}
	longAgo := now.Add(-16 * time.Minute)
	if _, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", longAgo); err != nil {
		t.Fatalf("MintEnrolment: %v", err)
	}
	if r.AcceptsConnectionFrom("10.10.0.1") {
		t.Error("AcceptsConnectionFrom() = true, want false once the only pending token has expired")
	}
}

// TestRefusedIsBoundedAndEvictsOldestLastSeenFirst pins the 256-address
// cap: past it, the address that has been quietest the longest is the
// one dropped, so an active flood cannot itself evict the accounting
// for other still-active refused senders.
func TestRefusedIsBoundedAndEvictsOldestLastSeenFirst(t *testing.T) {
	r := NewRegistry(nil)
	if maxRefusedAddresses != 256 {
		t.Fatalf("maxRefusedAddresses = %d, want 256", maxRefusedAddresses)
	}
	for i := 0; i < maxRefusedAddresses+10; i++ {
		host := ipFromIndex(i)
		r.Refuse(host, []byte("x"))
	}
	got := r.Refused()
	if len(got) > maxRefusedAddresses {
		t.Fatalf("Refused() returned %d entries, want at most %d", len(got), maxRefusedAddresses)
	}
	// The very first addresses refused are the oldest by last-seen and
	// must be the ones evicted.
	for _, ref := range got {
		if ref.Address == ipFromIndex(0) {
			t.Errorf("the oldest refused address survived the eviction: %+v", got)
		}
	}
}

func ipFromIndex(i int) string {
	return fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256)
}

// TestOwnPrefixesReadsOnlySourceAndAcceptedIP is issue #1281's
// replacement for the drop-list's former OwnRanges source
// (routerstate's pushed /ip/address tables): only real evidence -- a
// config.yaml declaration or a redeemed enrolment token -- ever counts,
// never a device's pushed table.
func TestOwnPrefixesReadsOnlySourceAndAcceptedIP(t *testing.T) {
	r := NewRegistry([]config.Device{{ID: "core", SourceIP: "192.168.1.1"}})
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	enrolAt(t, r, "hap-ax3", "10.10.0.1")

	got := r.OwnPrefixes()
	want := map[string]bool{"192.168.1.1/32": true, "10.10.0.1/32": true}
	if len(got) != len(want) {
		t.Fatalf("OwnPrefixes() = %v, want exactly %v", got, want)
	}
	for _, p := range got {
		if !want[p.String()] {
			t.Errorf("OwnPrefixes() contains unexpected prefix %v", p)
		}
	}
}

// TestEnrolFromPushedAddressesEnrolsTheSoleClaimant is the "Upgrading
// to 0.6.0" one-shot nudge: a device with no AcceptedIP yet, whose
// pushed table is the only one naming a given address, is enrolled at
// it once and the enrolment persists across the call boundary (never
// re-run against a device that already has one).
func TestEnrolFromPushedAddressesEnrolsTheSoleClaimant(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	r.Ensure("hap-ax3", now)
	tables := pushedAddresses{"hap-ax3": {"10.10.0.1/24"}}

	enrolled := r.EnrolFromPushedAddresses(tables, now)
	if len(enrolled) != 1 || enrolled[0] != "hap-ax3" {
		t.Fatalf("EnrolFromPushedAddresses() = %v, want [hap-ax3]", enrolled)
	}
	if id := r.Resolve("10.10.0.1", now); id != "hap-ax3" {
		t.Errorf("Resolve() = %q after the upgrade nudge, want %q", id, "hap-ax3")
	}

	// Idempotent: a device already enrolled (by this call or a real
	// token) is left alone on a later call, even with the same evidence
	// still in front of it.
	if again := r.EnrolFromPushedAddresses(tables, now); len(again) != 0 {
		t.Errorf("EnrolFromPushedAddresses() on an already-enrolled device = %v, want none", again)
	}
}

// TestEnrolFromPushedAddressesSkipsContestedAddresses: two devices
// pushing the same address is exactly the case the old live-attribution
// claim step could not settle either -- the one-shot nudge must not
// guess, so neither device is enrolled from it.
func TestEnrolFromPushedAddressesSkipsContestedAddresses(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	r.Ensure("a", now)
	r.Ensure("b", now)
	tables := pushedAddresses{
		"a": {"172.23.0.1/16"},
		"b": {"172.23.0.1/16"},
	}

	if enrolled := r.EnrolFromPushedAddresses(tables, now); len(enrolled) != 0 {
		t.Errorf("EnrolFromPushedAddresses() = %v, want none: the address is contested", enrolled)
	}
}

// TestAcceptedIPAndEnrolledAtSurviveRestart is issue #1281's core
// persistence promise: a device this registry created, once enrolled,
// keeps its AcceptedIP/EnrolledAt (and continues to exist at all) after
// the process restarts -- the same JSON-file + atomic-write convention
// internal/device/mac_registry.go already uses.
func TestAcceptedIPAndEnrolledAtSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")

	r1, err := OpenRegistry(path, nil)
	if err != nil {
		t.Fatalf("OpenRegistry: %v", err)
	}
	now := time.Now().Truncate(time.Second)
	if _, err := r1.Create("hap-ax3", "Hap AX3", now); err != nil {
		t.Fatal(err)
	}
	enrolAt(t, r1, "hap-ax3", "10.10.0.1")

	r2, err := OpenRegistry(path, nil)
	if err != nil {
		t.Fatalf("OpenRegistry (reload): %v", err)
	}
	list := r2.List()
	if len(list) != 1 {
		t.Fatalf("List() after reload = %+v, want the one created device", list)
	}
	d := list[0]
	if d.ID != "hap-ax3" || d.Name != "Hap AX3" || d.AcceptedIP != "10.10.0.1" || d.EnrolledAt.IsZero() {
		t.Errorf("reloaded device = %+v, want identity and enrolment to have survived", d)
	}
	if !r2.Allowed("10.10.0.1") {
		t.Errorf("Allowed(%q) = false after reload, want true: the enrolment must be live, not just visible", "10.10.0.1")
	}
}

// TestConfiguredDevicesAreNotPersisted: a config.yaml declaration is
// rebuilt fresh from that file on every boot, so persisting it too
// would be a second, potentially stale source of truth for the same
// device.
func TestConfiguredDevicesAreNotPersisted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")

	r1, err := OpenRegistry(path, []config.Device{{ID: "core", Name: "Core", SourceIP: "192.168.1.1"}})
	if err != nil {
		t.Fatal(err)
	}
	r1.Resolve("192.168.1.1", time.Now())
	// Force a write by also creating a real registry-owned device --
	// otherwise persistLocked has nothing to write and the file may not
	// exist at all yet, which is a valid but less informative case.
	if _, err := r1.Create("extra", "extra", time.Now()); err != nil {
		t.Fatal(err)
	}

	r2, err := OpenRegistry(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r2.List() {
		if d.ID == "core" {
			t.Errorf("a config.yaml-declared device was persisted and reloaded without config.yaml declaring it: %+v", d)
		}
	}
}

// TestEnrolmentClearsTheAddressFromTheRefusedList: a router logs its
// own logging-action change before the enrol line reaches us, so the
// first line or two from every router are refused and only then does
// the token redeem. Left in place, that entry puts the router the
// operator just enrolled in the wizard's "wrong sender" warning box and
// the fleet strip, beside the "enrolled at" line saying the opposite.
func TestEnrolmentClearsTheAddressFromTheRefusedList(t *testing.T) {
	r := NewRegistry(nil)
	if _, err := r.Create("hap-ax3", "hap-ax3", time.Now()); err != nil {
		t.Fatal(err)
	}
	r.RefuseConnection("10.10.0.1")
	r.Refuse("10.10.0.1", []byte("system,info log action changed by admin"))
	r.Refuse("10.10.0.9", []byte("some other box"))

	enrolAt(t, r, "hap-ax3", "10.10.0.1")

	got := r.Refused()
	if len(got) != 1 || got[0].Address != "10.10.0.9" {
		t.Fatalf("Refused() after enrolling 10.10.0.1 = %+v, want only the stranger left", got)
	}
}

// TestEnrolRefusesAnAddressAnotherDeviceAlreadyHolds: an address
// belongs to one router. Enrolling a second device at an address that
// is already some other device's declared sourceIp or enrolled address
// used to succeed: the registry logged "enrolled", the row said
// "Enrolled at ...", and Resolve went on attributing every line to
// whichever device the lookup order reached first -- so one of the two
// was silently dead while its card claimed otherwise. The enrolment is
// refused instead, and the token stays pending so the operator can
// redeem it from the right address without rerolling.
func TestEnrolRefusesAnAddressAnotherDeviceAlreadyHolds(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	for _, id := range []string{"held", "claimant"} {
		if _, err := r.Create(id, id, now); err != nil {
			t.Fatal(err)
		}
	}
	enrolAt(t, r, "held", "10.0.0.1")

	// Minted for the contested address itself, so the refusal below is
	// the held-address rule doing the work -- not #1291's address
	// binding, which would refuse any other address first and leave this
	// test passing while proving nothing.
	token, _, err := r.MintEnrolment("claimant", "10.0.0.1", now)
	if err != nil {
		t.Fatalf("MintEnrolment: %v", err)
	}
	line := []byte(`<30>Jan  1 00:00:00 router mikroview-enrol ` + token)
	if r.TryEnrol("10.0.0.1", line) {
		t.Fatal("TryEnrol() = true at an address another device already holds, want false")
	}

	for _, info := range r.List() {
		if info.ID == "claimant" && info.AcceptedIP != "" {
			t.Errorf("claimant AcceptedIP = %q, want it left unenrolled", info.AcceptedIP)
		}
		if info.ID == "held" && info.AcceptedIP != "10.0.0.1" {
			t.Errorf("held AcceptedIP = %q, want it kept", info.AcceptedIP)
		}
	}
	if id := r.Resolve("10.0.0.1", now); id != "held" {
		t.Errorf("Resolve() = %q, want the address still attributing to held", id)
	}
	// The token is unspent: once the address stops being held, the same
	// token still redeems there.
	//
	// It has to be the same address. Before #1291 this was checked by
	// redeeming at a different free one, which a token minted since
	// #1291 refuses -- it is bound to the address it was minted for, and
	// changing the address now means minting again. That is the binding
	// working, so the claim under test (the refusal leaves the token
	// unspent) is proved by freeing the address instead.
	if err := r.Delete("held"); err != nil {
		t.Fatalf("Delete(held): %v", err)
	}
	if !r.TryEnrol("10.0.0.1", line) {
		t.Error("TryEnrol() = false once the address was freed, want the unspent token still good")
	}
}

// TestMintEnrolmentRequiresAnExpectedAddress: the address the token
// binds to is not optional (issue #1291). A token minted without one
// would be #1281's global gate again under a new name -- the port open
// to every unknown address for the life of the token -- so there is
// deliberately no way to ask for that.
func TestMintEnrolmentRequiresAnExpectedAddress(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, addr string
		want       error
	}{
		{"empty", "", ErrExpectedAddressRequired},
		{"only spaces", "   ", ErrExpectedAddressRequired},
		{"a hostname, not an address", "router.example.com", ErrExpectedAddressInvalid},
		{"nonsense", "10.0.0.999", ErrExpectedAddressInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := r.MintEnrolment("hap-ax3", tc.addr, now); !errors.Is(err, tc.want) {
				t.Errorf("MintEnrolment(%q) error = %v, want %v", tc.addr, err, tc.want)
			}
		})
	}
	// Nothing was left pending by any of the refusals above, so the
	// connection gate stayed shut throughout.
	if r.AcceptsConnectionFrom("10.10.0.1") {
		t.Error("AcceptsConnectionFrom() = true after only refused mints, want the port still closed")
	}
}

// TestTokenIsRefusedFromAnAddressItWasNotMintedFor is issue #1291's
// binding at the redemption itself: a token names one address, and a
// line carrying it from anywhere else enrols nothing. The connection
// gate refuses that address before a byte is read in the real listener,
// so reaching TryEnrol from the wrong address means it was allowed for
// some other reason -- and this is what stops that becoming a way in.
func TestTokenIsRefusedFromAnAddressItWasNotMintedFor(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	token, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	line := []byte("mikroview-enrol " + token)

	if r.TryEnrol("10.10.0.2", line) {
		t.Fatal("TryEnrol() = true from an address the token was not minted for, want false")
	}
	for _, info := range r.List() {
		if info.ID == "hap-ax3" && info.AcceptedIP != "" {
			t.Errorf("acceptedIp = %q after a refused redemption, want it left unenrolled", info.AcceptedIP)
		}
	}
	// The token is unspent -- a wrong-address attempt must not burn it,
	// or anyone able to reach the port could destroy an enrolment in
	// flight by replaying the line from somewhere else.
	if !r.TryEnrol("10.10.0.1", line) {
		t.Error("TryEnrol() = false at the minted address, want the token unharmed by the refused attempt")
	}
}

// TestAcceptsConnectionFromIsFalseForAnyOtherAddress is the narrowing
// #1291 exists for: before it, one pending token anywhere left the
// syslog port reachable by every unknown address on the network.
func TestAcceptsConnectionFromIsFalseForAnyOtherAddress(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now); err != nil {
		t.Fatal(err)
	}
	if !r.AcceptsConnectionFrom("10.10.0.1") {
		t.Error("AcceptsConnectionFrom(minted address) = false, want true")
	}
	for _, other := range []string{"10.10.0.2", "192.168.1.1", "", "not-an-address"} {
		if r.AcceptsConnectionFrom(other) {
			t.Errorf("AcceptsConnectionFrom(%q) = true while a token is pending for another address, want false", other)
		}
	}
}
