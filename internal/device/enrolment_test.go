// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

// TestTryEnrolLeavesTheDeviceUnenrolledWhenPersistFails is the v0.6.0
// audit's R6 fix: an enrolment that cannot be saved must not read as
// enrolled in memory -- and the token must stay spendable -- or a
// restart before the next good write would silently un-enrol a router
// this call just told the caller succeeded.
func TestTryEnrolLeavesTheDeviceUnenrolledWhenPersistFails(t *testing.T) {
	b := &failingSaveBackend{}
	r, err := OpenRegistryWithBackend(b, nil)
	if err != nil {
		t.Fatalf("OpenRegistryWithBackend: %v", err)
	}
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	token, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	line := []byte(`<30>Jan  1 00:00:00 router mikroview-enrol ` + token)

	// Create above already spent one Save attempt (also against this
	// always-failing backend, swallowed the same way every ordinary write
	// is); what matters here is TryEnrol's own attempt, not the running
	// total.
	before := b.count()
	if r.TryEnrol("10.10.0.1", line) {
		t.Fatal("TryEnrol() = true against a backend that cannot save, want false")
	}
	if got := b.count() - before; got != 1 {
		t.Errorf("TryEnrol attempted %d saves, want exactly 1", got)
	}

	got := r.List()
	if len(got) != 1 || got[0].AcceptedIP != "" || !got[0].EnrolledAt.IsZero() {
		t.Errorf("device state after a failed persist = %+v, want no AcceptedIP/EnrolledAt", got)
	}
	if r.Allowed("10.10.0.1") {
		t.Error("Allowed(10.10.0.1) = true after a failed enrolment persist -- the address must not read as claimed")
	}
	if p := r.PendingEnrolment("hap-ax3"); !p.Pending {
		t.Error("PendingEnrolment().Pending = false after a failed persist -- the token must stay redeemable for a retry")
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

// TestRebindEnrolmentMovesTheWindowAndKeepsTheToken is issue #1291's
// wrong-address recovery (ruling 23a): the operator named the wrong
// address, their router was turned away at accept and landed in the
// refused-senders list, and one click points the window at it. The
// token is untouched, so nothing is pasted into the router again.
func TestRebindEnrolmentMovesTheWindowAndKeepsTheToken(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	token, expiresAt, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	// The real router is at .5, so its connection is turned away.
	r.RefuseConnection("10.10.0.5")
	if r.AcceptsConnectionFrom("10.10.0.5") {
		t.Fatal("AcceptsConnectionFrom(.5) = true before rebinding, want false")
	}

	if err := r.RebindEnrolment("hap-ax3", "10.10.0.5"); err != nil {
		t.Fatalf("RebindEnrolment: %v", err)
	}
	if !r.AcceptsConnectionFrom("10.10.0.5") {
		t.Error("AcceptsConnectionFrom(.5) = false after rebinding, want the window moved")
	}
	if r.AcceptsConnectionFrom("10.10.0.1") {
		t.Error("AcceptsConnectionFrom(.1) = true after rebinding, want the old address closed again")
	}

	// The same token, never re-pasted, now redeems at the new address.
	line := []byte("mikroview-enrol " + token)
	if !r.TryEnrol("10.10.0.5", line) {
		t.Fatal("TryEnrol() = false at the rebound address, want the original token still good")
	}
	if expiresAt.IsZero() {
		t.Error("mint returned a zero expiry")
	}
}

// TestRebindEnrolmentOnlyAcceptsARefusedAddress is what makes the
// rebind safe to offer as one click rather than another password
// prompt: it can only point the window at an address that already
// reached the listener under its own steam.
func TestRebindEnrolmentOnlyAcceptsARefusedAddress(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", now); err != nil {
		t.Fatal(err)
	}
	if err := r.RebindEnrolment("hap-ax3", "203.0.113.7"); !errors.Is(err, ErrNotRefused) {
		t.Errorf("RebindEnrolment(never-refused) error = %v, want ErrNotRefused", err)
	}
	if r.AcceptsConnectionFrom("203.0.113.7") {
		t.Error("a refused rebind still opened the window, want it unchanged")
	}
}

// TestRebindEnrolmentNeedsAPendingToken: there is no window to move
// when nothing is pending, and rebinding must never create one.
func TestRebindEnrolmentNeedsAPendingToken(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	r.RefuseConnection("10.10.0.5")
	if err := r.RebindEnrolment("hap-ax3", "10.10.0.5"); !errors.Is(err, ErrNoPendingEnrolment) {
		t.Errorf("RebindEnrolment(nothing pending) error = %v, want ErrNoPendingEnrolment", err)
	}
	if r.AcceptsConnectionFrom("10.10.0.5") {
		t.Error("rebinding with nothing pending opened the window, want it to grant nothing")
	}
}

// TestValidateExpectedAddressSharedByMintAndRebind is audit finding
// 26b: MintEnrolment and RebindEnrolment used to validate the expected
// address via two independent copies of the same three lines, so a
// future rule change could make them disagree about what counts as
// valid. This exercises the shared validateExpectedAddress helper
// directly -- if a later change reintroduces a second copy inside one of
// the two callers instead of editing this one, that caller stops
// agreeing with this test's cases without this test itself changing.
func TestValidateExpectedAddressSharedByMintAndRebind(t *testing.T) {
	for _, tc := range []struct {
		name    string
		addr    string
		wantErr error
		wantKey string
	}{
		{"empty", "", ErrExpectedAddressRequired, ""},
		{"only spaces", "   ", ErrExpectedAddressRequired, ""},
		{"a hostname, not an address", "router.example.com", ErrExpectedAddressInvalid, ""},
		{"nonsense", "10.0.0.999", ErrExpectedAddressInvalid, ""},
		{"a valid address", "10.10.0.1", nil, "10.10.0.1"},
		{"padded with spaces", "  10.10.0.1  ", nil, "10.10.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, err := validateExpectedAddress(tc.addr)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("validateExpectedAddress(%q) error = %v, want %v", tc.addr, err, tc.wantErr)
			}
			if key != tc.wantKey {
				t.Errorf("validateExpectedAddress(%q) key = %q, want %q", tc.addr, key, tc.wantKey)
			}
		})
	}

	// Both callers must actually go through the helper, not merely agree
	// with it by coincidence: same bad input, same error, from each.
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.MintEnrolment("hap-ax3", "not-an-ip", now); !errors.Is(err, ErrExpectedAddressInvalid) {
		t.Errorf("MintEnrolment(bad address) error = %v, want ErrExpectedAddressInvalid", err)
	}
	if err := r.RebindEnrolment("hap-ax3", "not-an-ip"); !errors.Is(err, ErrExpectedAddressInvalid) {
		t.Errorf("RebindEnrolment(bad address) error = %v, want ErrExpectedAddressInvalid", err)
	}
}

// TestCollisionReasonNamesTheWayOut is stage 6 finding 1 of the #1291
// audit: TryEnrol's refusal for an address already enrolled to another
// device used to stop at naming that device ("already enrolled as
// <id>"), with no hint that the address is recoverable. An operator
// whose router was swapped for new hardware at the same address has no
// way to know that re-enrolling the old id elsewhere, or deleting its
// device record, frees the address for the replacement. The wording
// lives in collisionReason itself, so this checks it once at the source
// rather than in each caller.
func TestCollisionReasonNamesTheWayOut(t *testing.T) {
	got := collisionReason("old-router", false)
	if !strings.Contains(got, "old-router") {
		t.Fatalf("collisionReason(enrolled) = %q, want it to name the device that holds the address", got)
	}
	if !strings.Contains(got, "re-enrolling") || !strings.Contains(got, "deleting") {
		t.Errorf("collisionReason(enrolled) = %q, want it to say how to free the address: re-enrol the holder elsewhere, or delete its device record", got)
	}

	// The config.yaml case is a different fix (edit that file) and
	// deliberately keeps its own, unchanged wording -- this only asserts
	// the two cases stay distinguishable, not that the declared case also
	// grows the same guidance it does not apply to.
	if got := collisionReason("core", true); !strings.Contains(got, "declared as core") {
		t.Errorf("collisionReason(configured) = %q, want the declared-in-config wording unchanged", got)
	}
}

// TestRebindEnrolmentRefusesAnExpiredWindow: rebinding checked the
// device, the pending token and the refused list, but never the
// expiry -- unlike VerifyPendingToken, TryEnrol and
// AcceptsConnectionFrom, which all do. Past the TTL the rebind
// returned nil, so the wizard reported success and told the operator to
// connect their router while the gate stayed shut, with no error
// anywhere to explain the silence. Recovery is a fresh mint, and
// nothing was telling the operator that. Found by the v0.6.0 audit
// (#1257), finding 3.
func TestRebindEnrolmentRefusesAnExpiredWindow(t *testing.T) {
	r := NewRegistry(nil)
	minted := time.Now().Add(-2 * enrolTokenTTL)
	if _, err := r.Create("hap-ax3", "hap-ax3", minted); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.MintEnrolment("hap-ax3", "10.10.0.1", minted); err != nil {
		t.Fatal(err)
	}
	r.RefuseConnection("10.10.0.5")

	err := r.RebindEnrolment("hap-ax3", "10.10.0.5")
	if !errors.Is(err, ErrEnrolmentExpired) {
		t.Errorf("RebindEnrolment on a lapsed token = %v, want ErrEnrolmentExpired", err)
	}
	if r.AcceptsConnectionFrom("10.10.0.5") {
		t.Error("the window moved to .5 anyway -- a lapsed token must not reopen the gate")
	}
}
