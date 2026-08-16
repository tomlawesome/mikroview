// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// newShippedDestSpreadDefinition builds either direction's shipped
// definition at the params given, defaulting to
// internal/detect.DefaultConfig()'s own values for that direction.
func newShippedDestSpreadDefinition(t *testing.T, id string, sink func(RoutedEmission), params Params, scope Scope, enabled bool) *destSpreadDefinition {
	t.Helper()
	var full Params
	var schema []ParamSchema
	switch id {
	case "outbound_anomaly":
		full = Params{"threshold": 25, "window": (5 * time.Minute).String(), "vpnConfidenceMultiplier": 1.5}
		schema = OutboundAnomalyParamSchema
	case "internal_recon":
		full = Params{"threshold": 10, "window": time.Minute.String(), "vpnConfidenceMultiplier": 1.5}
		schema = InternalReconParamSchema
	default:
		t.Fatalf("newShippedDestSpreadDefinition: unknown id %q", id)
	}
	for k, v := range params {
		full[k] = v
	}
	def := Definition{
		ID:          id,
		Name:        id,
		Intent:      IntentDetection,
		Kind:        KindProgrammatic,
		Enabled:     enabled,
		Scope:       scope,
		Params:      full,
		ParamSchema: schema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(%s): %v", id, err)
	}
	d := built.(*destSpreadDefinition)
	d.SetSink(sink)
	return d
}

func dsFlag(fs *flags.Store, typ flags.Type) *flags.Flag {
	for _, f := range fs.List() {
		f := f
		if f.Type == typ {
			return &f
		}
	}
	return nil
}

// lanEvt is internal/detect/dest_spread_test.go's helper of the same
// name.
func lanEvt(srcIP, dstIP string, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: dstIP, DstPort: 443, ReceivedAt: at}
}

// pub3 is internal/detect/characterization_test.go's helper of the same
// name: distinct public addresses across 203.0.113.0/24 and
// 198.51.100.0/24, so a test needing more than 254 of them still gets
// distinct values.
func pub3(i int) string {
	if i < 254 {
		return fmt.Sprintf("203.0.113.%d", i+1)
	}
	return fmt.Sprintf("198.51.100.%d", i-253)
}

func sortedStringsCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

func TestShippedOutboundAnomalyFlagsManyDistinctExternalDestinations(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs),
		Params{"threshold": 5, "window": time.Minute.String()}, Scope{}, true)

	now := time.Now()
	for i := 1; i <= 5; i++ {
		d.Evaluate(lanEvt("192.168.1.50", fmt.Sprintf("203.0.113.%d", i), now.Add(time.Duration(i)*time.Second)))
	}
	f := dsFlag(fs, flags.TypeOutboundAnomaly)
	if f == nil {
		t.Fatalf("expected an outbound_anomaly flag, got %+v", fs.List())
	}
	if f.Target != "192.168.1.50" {
		t.Errorf("Target = %q, want the LAN source", f.Target)
	}
}

// TestShippedOutboundAnomalyIgnoresExternalSources is
// internal/detect/dest_spread_test.go's TestDestSpreadIgnoresExternalSources
// for the outbound half: an external source's destination spread is
// internet background noise, not one network's.
func TestShippedOutboundAnomalyIgnoresExternalSources(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs),
		Params{"threshold": 2, "window": time.Minute.String()}, Scope{}, true)

	now := time.Now()
	for i := 1; i <= 5; i++ {
		d.Evaluate(lanEvt("203.0.113.200", fmt.Sprintf("203.0.113.%d", i), now.Add(time.Duration(i)*time.Second)))
	}
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected no flag for a public source, got %+v", got)
	}
}

// TestShippedOutboundAnomalyIgnoresInternalDestinations pins the
// direction split: an internal destination is not counted by this
// definition, however many distinct ones there are.
func TestShippedOutboundAnomalyIgnoresInternalDestinations(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs),
		Params{"threshold": 3, "window": time.Minute.String()}, Scope{}, true)

	now := time.Now()
	for i := 1; i <= 8; i++ {
		d.Evaluate(lanEvt("192.168.1.50", fmt.Sprintf("192.168.1.%d", i), now.Add(time.Duration(i)*time.Second)))
	}
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected internal destinations never to count as outbound, got %+v", got)
	}
}

func TestShippedOutboundAnomalyIgnoresEstablishedTraffic(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs),
		Params{"threshold": 3, "window": time.Minute.String()}, Scope{}, true)

	now := time.Now()
	for i := 1; i <= 8; i++ {
		e := lanEvt("192.168.1.50", fmt.Sprintf("203.0.113.%d", i), now.Add(time.Duration(i)*time.Second))
		e.ConnState = "established"
		d.Evaluate(e)
	}
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected established/return traffic to be ignored, got %+v", got)
	}
}

func TestShippedOutboundAnomalyRespectsHostsScope(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs),
		Params{"threshold": 3, "window": time.Minute.String()},
		Scope{Hosts: []string{"192.168.1.50"}, HostsMode: ListModeAllow}, true)

	now := time.Now()
	for i, dst := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
		d.Evaluate(lanEvt("192.168.1.99", dst, now.Add(time.Duration(i)*time.Second)))
	}
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected a source outside the allowlist to never flag, got %+v", got)
	}
	for i, dst := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
		d.Evaluate(lanEvt("192.168.1.50", dst, now.Add(time.Duration(i)*time.Second)))
	}
	if got := fs.List(); len(got) != 1 {
		t.Fatalf("expected the allowlisted source to still flag, got %+v", got)
	}
}

func TestShippedOutboundAnomalyDisabledIsInert(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs),
		Params{"threshold": 2, "window": time.Minute.String()}, Scope{}, false)

	now := time.Now()
	for i := 1; i <= 5; i++ {
		d.Evaluate(lanEvt("192.168.1.50", fmt.Sprintf("203.0.113.%d", i), now.Add(time.Duration(i)*time.Second)))
	}
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected a disabled definition to never flag, got %+v", got)
	}
}

// TestShippedOutboundAnomalyConfidenceScalesWithOvershoot is
// internal/detect/dest_spread_test.go's
// TestOutboundAnomalyConfidenceScalesWithOvershoot, moved unchanged.
func TestShippedOutboundAnomalyConfidenceScalesWithOvershoot(t *testing.T) {
	now := time.Now()

	fs := newTestFlagsStore(t)
	justOver := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs),
		Params{"threshold": 5, "window": (5 * time.Minute).String()}, Scope{}, true)
	for i := 1; i <= 5; i++ {
		justOver.Evaluate(lanEvt("192.168.1.50", fmt.Sprintf("203.0.113.%d", i), now.Add(time.Duration(i)*time.Second)))
	}
	list := fs.List()
	if len(list) != 1 || list[0].Confidence == nil || *list[0].Confidence != 0 {
		t.Fatalf("expected 0%% confidence exactly at threshold, got %+v", list)
	}

	fs2 := newTestFlagsStore(t)
	wellOver := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs2),
		Params{"threshold": 5, "window": (5 * time.Minute).String()}, Scope{}, true)
	for i := 1; i <= 15; i++ {
		wellOver.Evaluate(lanEvt("192.168.1.50", fmt.Sprintf("203.0.113.%d", i), now.Add(time.Duration(i)*time.Second)))
	}
	list2 := fs2.List()
	if len(list2) != 1 || list2[0].Confidence == nil || *list2[0].Confidence != 100 {
		t.Fatalf("expected 100%% confidence at the overshoot ceiling, got %+v", list2)
	}
}

// TestShippedOutboundAnomaly_FieldsRefireClearRevive is
// internal/detect/characterization_test.go's
// TestCharacterizationOutboundAnomaly_FieldsRefireClearRevive, moved.
// Every pinned value is unchanged: the 24/25 boundary at the real
// 25-destinations/5-minute defaults, Target, the byte-for-byte Detail,
// Confidence=0 at the boundary and 2 after one more, the
// maxEvidenceHosts=20 cap with its exact sorted contents, and the
// re-fire/clear/revive sequence.
func TestShippedOutboundAnomaly_FieldsRefireClearRevive(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs), nil, Scope{}, true)
	src := "192.168.1.50"
	t0 := time.Now()

	for i := 0; i < 24; i++ {
		d.Evaluate(store.Event{SrcIP: src, DstIP: pub3(i), DstPort: 443, ReceivedAt: t0.Add(time.Duration(i) * time.Second)})
	}
	if got := dsFlag(fs, flags.TypeOutboundAnomaly); got != nil {
		t.Fatalf("expected no flag at 24 distinct external destinations, got %+v", got)
	}

	d.Evaluate(store.Event{SrcIP: src, DstIP: pub3(24), DstPort: 443, ReceivedAt: t0.Add(24 * time.Second)})
	f := dsFlag(fs, flags.TypeOutboundAnomaly)
	if f == nil {
		t.Fatal("expected a flag at exactly 25 distinct external destinations")
	}
	if f.Target != src {
		t.Errorf("Target = %q, want %q", f.Target, src)
	}
	if want := "25 distinct external destinations in 5m0s"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0", f.Confidence)
	}
	if len(f.Evidence.Hosts) != 20 {
		t.Fatalf("Evidence.Hosts length = %d, want 20 (maxEvidenceHosts cap)", len(f.Evidence.Hosts))
	}
	wantAll := make([]string, 25)
	for i := range wantAll {
		wantAll[i] = pub3(i)
	}
	wantCapped := sortedStringsCopy(wantAll)[:20]
	if fmt.Sprint(f.Evidence.Hosts) != fmt.Sprint(wantCapped) {
		t.Errorf("Evidence.Hosts = %v, want %v (sorted, capped at 20)", f.Evidence.Hosts, wantCapped)
	}

	// Re-fire.
	d.Evaluate(store.Event{SrcIP: src, DstIP: pub3(25), DstPort: 443, ReceivedAt: t0.Add(25 * time.Second)})
	f2 := dsFlag(fs, flags.TypeOutboundAnomaly)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 2 {
		t.Errorf("Confidence after re-fire = %v, want 2 (overshootConfidence(26,25))", f2.Confidence)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, t0.Add(26*time.Second)) {
		t.Fatal("expected Clear to succeed")
	}
	d.Evaluate(store.Event{SrcIP: src, DstIP: pub3(26), DstPort: 443, ReceivedAt: t0.Add(27 * time.Second)})
	f3 := dsFlag(fs, flags.TypeOutboundAnomaly)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
}

// TestShippedOutboundAnomalyRechecksOnAnInternalDestination pins the one
// consequence of splitting internal/detect's shared destWindow into two
// definitions that would otherwise be invisible: internal/detect ran the
// outbound threshold query on *every* qualifying event, whichever
// direction it was, so an internal-destination event from a source
// already over the threshold re-fired the outbound flag. Recording is
// direction-gated; checking is not.
func TestShippedOutboundAnomalyRechecksOnAnInternalDestination(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs),
		Params{"threshold": 3, "window": (5 * time.Minute).String()}, Scope{}, true)

	t0 := time.Now()
	for i := 1; i <= 3; i++ {
		d.Evaluate(lanEvt("192.168.1.50", fmt.Sprintf("203.0.113.%d", i), t0.Add(time.Duration(i)*time.Second)))
	}
	f := dsFlag(fs, flags.TypeOutboundAnomaly)
	if f == nil || f.Count != 1 {
		t.Fatalf("expected one episode at the threshold, got %+v", f)
	}

	d.Evaluate(lanEvt("192.168.1.50", "192.168.1.77", t0.Add(4*time.Second)))
	f2 := dsFlag(fs, flags.TypeOutboundAnomaly)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected the still-crossed window to re-fire on an internal-destination event, got %+v", f2)
	}
}

// TestShippedOutboundAnomalyVPNBoostsConfidenceAndNamesTheInterface pins
// #105's boost through the ported definition: the same anomaly arriving
// on a VPN-tagged interface scores higher and says so.
func TestShippedOutboundAnomalyVPNBoostsConfidenceAndNamesTheInterface(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs),
		Params{"threshold": 5, "window": (5 * time.Minute).String(),
			"vpnInterfaces": []string{"wireguard*"}, "vpnConfidenceMultiplier": 1.5},
		Scope{}, true)

	t0 := time.Now()
	for i := 1; i <= 8; i++ {
		e := lanEvt("192.168.1.50", fmt.Sprintf("203.0.113.%d", i), t0.Add(time.Duration(i)*time.Second))
		e.InInterface = "wireguard1"
		d.Evaluate(e)
	}
	f := dsFlag(fs, flags.TypeOutboundAnomaly)
	if f == nil {
		t.Fatalf("expected a flag, got %+v", fs.List())
	}
	plain := overshootConfidence(8, 5)
	want := int(math.Round(float64(plain) * 1.5))
	if f.Confidence == nil || *f.Confidence != want {
		t.Errorf("Confidence = %v, want %d (overshootConfidence(8,5)=%d boosted 1.5x)", f.Confidence, want, plain)
	}
	wantSuffix := " -- arrived via VPN interface \"wireguard1\", scored more confidently as an already-authenticated remote peer"
	if got := f.Detail; len(got) < len(wantSuffix) || got[len(got)-len(wantSuffix):] != wantSuffix {
		t.Errorf("Detail = %q, want it to end in %q", got, wantSuffix)
	}
}

// TestShippedOutboundAnomalyGroupReputationSamplesAreCapped is
// internal/detect/reputation_test.go's
// TestOutboundAnomalyGroupReputationSamplesAreCapped, moved onto
// GroupReputationSink -- which is what main.go wires onto this
// definition (see shippedGroupReputationIDs).
func TestShippedOutboundAnomalyGroupReputationSamplesAreCapped(t *testing.T) {
	fs := newTestFlagsStore(t)
	fake := newFakeReputation()
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly",
		GroupReputationSink(fs, fake, 8),
		Params{"threshold": 15, "window": (5 * time.Minute).String()}, Scope{}, true)

	// Every possible member gets the same score, so it does not matter
	// which members the sample happens to pick, or how many of them the
	// lookup pool actually has room for.
	now := time.Now()
	for i := 1; i <= 15; i++ {
		dst := fmt.Sprintf("203.0.113.%d", i)
		fake.setScore(dst, 80)
		d.Evaluate(lanEvt("192.168.1.50", dst, now.Add(time.Duration(i)*time.Millisecond)))
	}

	// The group's sampling loop is synchronous and does not retry a
	// member skipped for a saturated pool, so from an otherwise-idle pool
	// it reaches min(reputationGroupSampleSize, concurrency) real
	// lookups; the remaining sampled-but-skipped members are recorded as
	// no-data rather than retried.
	wantStarted := reputationGroupSampleSize
	if 8 < wantStarted {
		wantStarted = 8
	}
	seen := make(map[string]bool)
	for i := 0; i < wantStarted; i++ {
		seen[repExpectStarted(t, fake.started)] = true
	}
	if len(seen) != wantStarted {
		t.Fatalf("expected exactly %d distinct lookups, got %d", wantStarted, len(seen))
	}
	repExpectNoneStarted(t, fake.started) // 15 members -- must never exceed the pool

	close(fake.release)
	wantFloor := int(math.Round(80 * (float64(wantStarted) / float64(reputationGroupSampleSize))))
	repWaitForConfidence(t, fs, "192.168.1.50", wantFloor)
}

// TestShippedOutboundAnomalyReplayDeclinesOnShortCorpus and its receipt
// counterpart pin #403's contract for this definition.
func TestShippedOutboundAnomalyReplayDeclinesOnShortCorpus(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs), nil, Scope{}, true)

	t0 := time.Now()
	var events []store.Event
	for i := 0; i < 30; i++ {
		events = append(events, lanEvt("192.168.1.50", pub3(i), t0.Add(time.Duration(i)*time.Second)))
	}
	res, err := d.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Decline == nil {
		t.Fatalf("expected a Decline over a 29-second corpus against a 5m window, got %+v", res)
	}
}

func TestShippedOutboundAnomalyReplayProducesReceipt(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs), nil, Scope{}, true)

	t0 := time.Now().Add(-time.Hour)
	var events []store.Event
	for i := 0; i < 40; i++ {
		events = append(events, lanEvt("192.168.1.50", pub3(i), t0.Add(time.Duration(i)*10*time.Second)))
	}
	res, err := d.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Decline != nil {
		t.Fatalf("expected a Receipt over a 6m30s corpus, got Decline %+v", res.Decline)
	}
	if res.Receipt == nil || res.Receipt.EmissionCount() == 0 {
		t.Fatalf("expected a Receipt with emissions, got %+v", res.Receipt)
	}
	if got := len(fs.List()); got != 0 {
		t.Errorf("Replay raised %d live flag(s) -- it must never touch the live sink", got)
	}
	if d.dests.Len() != 0 {
		t.Errorf("Replay mutated the live definition's state (%d keys) -- call-local only", d.dests.Len())
	}
}

// TestShippedOutboundAnomalyConfidenceIdenticalWhenVPNInterfacesUnset is
// internal/detect/vpn_test.go's
// TestDestSpreadConfidenceIdenticalRegardlessOfInterfaceWhenVPNInterfacesUnset,
// moved: with no vpnInterfaces configured (the default), an interface
// named "wireguard1" is just an interface name and scores nothing extra.
func TestShippedOutboundAnomalyConfidenceIdenticalWhenVPNInterfacesUnset(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", FlagsSink(fs),
		Params{"threshold": 5, "window": time.Minute.String()}, Scope{}, true)

	now := time.Now()
	const plainIP, wgLookalikeIP = "192.168.1.80", "192.168.1.81"
	for i := 1; i <= 6; i++ {
		at := now.Add(time.Duration(i) * time.Second)
		dst := fmt.Sprintf("203.0.113.%d", i)
		plain := lanEvt(plainIP, dst, at)
		plain.InInterface = "ether1"
		d.Evaluate(plain)
		wg := lanEvt(wgLookalikeIP, dst, at)
		wg.InInterface = "wireguard1"
		d.Evaluate(wg)
	}

	var plainConf, wgConf *int
	for _, f := range fs.List() {
		if f.Type != flags.TypeOutboundAnomaly {
			continue
		}
		switch f.Target {
		case plainIP:
			plainConf = f.Confidence
		case wgLookalikeIP:
			wgConf = f.Confidence
		}
	}
	if plainConf == nil || wgConf == nil {
		t.Fatalf("expected both sources to raise outbound_anomaly, got %+v", fs.List())
	}
	if *plainConf != *wgConf {
		t.Errorf("expected identical confidence with vpnInterfaces unset regardless of InInterface, got LAN-iface=%d wireguard-iface=%d", *plainConf, *wgConf)
	}
}

// --- internal_recon ---------------------------------------------------

// lan3 is internal/detect/characterization_test.go's helper of the same
// name: distinct private destinations off the source's own /24, so a
// destination is never mistaken for the source.
func lan3(n int) string { return fmt.Sprintf("192.168.2.%d", 100+n) }

func TestShippedInternalReconFlagsManyDistinctInternalDestinations(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "internal_recon", FlagsSink(fs),
		Params{"threshold": 5, "window": time.Minute.String()}, Scope{}, true)

	now := time.Now()
	for i := 1; i <= 4; i++ {
		d.Evaluate(lanEvt("192.168.1.50", lan3(i), now.Add(time.Duration(i)*time.Second)))
	}
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected no flag below the distinct-destination threshold, got %+v", got)
	}
	d.Evaluate(lanEvt("192.168.1.50", lan3(99), now.Add(10*time.Second)))
	f := dsFlag(fs, flags.TypeInternalRecon)
	if f == nil || f.Target != "192.168.1.50" {
		t.Fatalf("expected an internal_recon flag for the LAN source, got %+v", fs.List())
	}
}

// TestShippedInternalReconIgnoresEstablishedTraffic is
// internal/detect/dest_spread_test.go's
// TestInternalReconIgnoresEstablishedTraffic: the database-server false
// positive reported in mikroview#35/#36 -- a busy server's
// established-connection return traffic to many distinct clients must
// not look like network recon.
func TestShippedInternalReconIgnoresEstablishedTraffic(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "internal_recon", FlagsSink(fs),
		Params{"threshold": 3, "window": time.Minute.String()}, Scope{}, true)

	now := time.Now()
	for i := 1; i <= 5; i++ {
		e := lanEvt("192.168.1.20", lan3(i), now.Add(time.Duration(i)*time.Second))
		e.ConnState = "established"
		d.Evaluate(e)
	}
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected established-state traffic to never trip internal_recon, got %+v", got)
	}
}

func TestShippedInternalReconIgnoresExternalSourcesAndDestinations(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "internal_recon", FlagsSink(fs),
		Params{"threshold": 2, "window": time.Minute.String()}, Scope{}, true)

	now := time.Now()
	// An external source's destination spread is never tracked.
	d.Evaluate(lanEvt("203.0.113.9", lan3(1), now))
	d.Evaluate(lanEvt("203.0.113.9", lan3(2), now))
	// A LAN source's external destinations are outbound_anomaly's, not
	// this definition's.
	for i := 1; i <= 5; i++ {
		d.Evaluate(lanEvt("192.168.1.50", fmt.Sprintf("203.0.113.%d", i), now.Add(time.Duration(i)*time.Second)))
	}
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected neither an external source nor external destinations to count, got %+v", got)
	}
}

func TestShippedInternalReconDisabledIsInert(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "internal_recon", FlagsSink(fs),
		Params{"threshold": 2, "window": time.Minute.String()}, Scope{}, false)

	now := time.Now()
	for i := 1; i <= 5; i++ {
		d.Evaluate(lanEvt("192.168.1.50", lan3(i), now.Add(time.Duration(i)*time.Second)))
	}
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected a disabled definition to never flag, got %+v", got)
	}
}

// TestShippedInternalReconConfidenceScalesWithOvershoot is
// internal/detect/dest_spread_test.go's
// TestInternalReconConfidenceScalesWithOvershoot, moved unchanged.
func TestShippedInternalReconConfidenceScalesWithOvershoot(t *testing.T) {
	now := time.Now()

	fs := newTestFlagsStore(t)
	justOver := newShippedDestSpreadDefinition(t, "internal_recon", FlagsSink(fs),
		Params{"threshold": 5, "window": time.Minute.String()}, Scope{}, true)
	for i := 1; i <= 5; i++ {
		justOver.Evaluate(lanEvt("192.168.1.50", lan3(i+10), now.Add(time.Duration(i)*time.Second)))
	}
	list := fs.List()
	if len(list) != 1 || list[0].Confidence == nil || *list[0].Confidence != 0 {
		t.Fatalf("expected 0%% confidence exactly at threshold, got %+v", list)
	}

	fs2 := newTestFlagsStore(t)
	wellOver := newShippedDestSpreadDefinition(t, "internal_recon", FlagsSink(fs2),
		Params{"threshold": 5, "window": time.Minute.String()}, Scope{}, true)
	for i := 1; i <= 15; i++ {
		wellOver.Evaluate(lanEvt("192.168.1.50", lan3(i+10), now.Add(time.Duration(i)*time.Second)))
	}
	list2 := fs2.List()
	if len(list2) != 1 || list2[0].Confidence == nil || *list2[0].Confidence != 100 {
		t.Fatalf("expected 100%% confidence at the overshoot ceiling, got %+v", list2)
	}
}

// TestShippedInternalReconVPNBoostsConfidence is
// internal/detect/vpn_test.go's
// TestInternalReconConfidenceHigherOverVPNInterfaceThanIdenticalLAN,
// moved: the same sweep arriving via a VPN-tagged interface scores
// higher than the identical one from an ordinary LAN interface.
func TestShippedInternalReconVPNBoostsConfidence(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "internal_recon", FlagsSink(fs),
		Params{"threshold": 5, "window": time.Minute.String(),
			"vpnInterfaces": []string{"wireguard*"}, "vpnConfidenceMultiplier": 2.0},
		Scope{}, true)

	now := time.Now()
	const lanIP, vpnIP = "192.168.1.70", "192.168.1.71"
	for i := 1; i <= 6; i++ {
		at := now.Add(time.Duration(i) * time.Second)
		plain := lanEvt(lanIP, lan3(i), at)
		plain.InInterface = "ether1"
		d.Evaluate(plain)
		wg := lanEvt(vpnIP, lan3(i), at)
		wg.InInterface = "wireguard1"
		d.Evaluate(wg)
	}

	var lanConf, vpnConf *int
	for _, f := range fs.List() {
		if f.Type != flags.TypeInternalRecon {
			continue
		}
		switch f.Target {
		case lanIP:
			lanConf = f.Confidence
		case vpnIP:
			vpnConf = f.Confidence
		}
	}
	if lanConf == nil || vpnConf == nil {
		t.Fatalf("expected both sources to raise internal_recon, got %+v", fs.List())
	}
	wantLAN := overshootConfidence(6, 5)
	wantVPN := vpnBoostConfidence(wantLAN, []string{"wireguard*"}, 2.0, "wireguard1")
	if *lanConf != wantLAN {
		t.Errorf("LAN confidence = %d, want %d", *lanConf, wantLAN)
	}
	if *vpnConf != wantVPN {
		t.Errorf("VPN confidence = %d, want %d", *vpnConf, wantVPN)
	}
	if *vpnConf <= *lanConf {
		t.Fatalf("expected VPN-interface confidence (%d) to exceed identical LAN-interface confidence (%d)", *vpnConf, *lanConf)
	}
}

// TestShippedInternalRecon_FieldsRefireClearRevive is
// internal/detect/characterization_test.go's
// TestCharacterizationInternalRecon_FieldsRefireClearRevive, moved.
// Every pinned value is unchanged: the 9/10 boundary at the real
// 10-destinations/60-second defaults, Target, the byte-for-byte Detail,
// Confidence 0 / 5 / 10 across boundary, re-fire and revival, and the
// exact sorted Evidence.Hosts.
func TestShippedInternalRecon_FieldsRefireClearRevive(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDestSpreadDefinition(t, "internal_recon", FlagsSink(fs), nil, Scope{}, true)
	src := "192.168.1.50"
	t0 := time.Now()

	for i := 0; i < 9; i++ {
		d.Evaluate(store.Event{SrcIP: src, DstIP: lan3(i), DstPort: 445, ReceivedAt: t0.Add(time.Duration(i) * time.Second)})
	}
	if got := dsFlag(fs, flags.TypeInternalRecon); got != nil {
		t.Fatalf("expected no flag at 9 distinct internal destinations, got %+v", got)
	}

	d.Evaluate(store.Event{SrcIP: src, DstIP: lan3(9), DstPort: 445, ReceivedAt: t0.Add(9 * time.Second)})
	f := dsFlag(fs, flags.TypeInternalRecon)
	if f == nil {
		t.Fatal("expected a flag at exactly 10 distinct internal destinations")
	}
	if f.Target != src {
		t.Errorf("Target = %q, want %q", f.Target, src)
	}
	if want := "10 distinct internal destinations in 1m0s"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0", f.Confidence)
	}
	wantHosts := make([]string, 10)
	for i := range wantHosts {
		wantHosts[i] = lan3(i)
	}
	if fmt.Sprint(f.Evidence.Hosts) != fmt.Sprint(sortedStringsCopy(wantHosts)) {
		t.Errorf("Evidence.Hosts = %v, want %v", f.Evidence.Hosts, sortedStringsCopy(wantHosts))
	}

	d.Evaluate(store.Event{SrcIP: src, DstIP: lan3(10), DstPort: 445, ReceivedAt: t0.Add(10 * time.Second)})
	f2 := dsFlag(fs, flags.TypeInternalRecon)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 5 {
		t.Errorf("Confidence after re-fire = %v, want 5 (overshootConfidence(11,10))", f2.Confidence)
	}

	if !fs.Clear(f2.ID, t0.Add(11*time.Second)) {
		t.Fatal("expected Clear to succeed")
	}
	d.Evaluate(store.Event{SrcIP: src, DstIP: lan3(11), DstPort: 445, ReceivedAt: t0.Add(12 * time.Second)})
	f3 := dsFlag(fs, flags.TypeInternalRecon)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
	if f3.Confidence == nil || *f3.Confidence != 10 {
		t.Errorf("Confidence after revival = %v, want 10 (overshootConfidence(12,10))", f3.Confidence)
	}
}

// TestShippedInternalReconIsNotAGroupReputationCandidate pins the
// deliberate asymmetry with outbound_anomaly: internal_recon's
// destinations are private by construction, so there is nothing for a
// reputation service to be asked about, and internal/detect made no
// lookup from it either.
func TestShippedInternalReconIsNotAGroupReputationCandidate(t *testing.T) {
	if shippedGroupReputationIDs["internal_recon"] {
		t.Error("internal_recon must not use the group reputation sink -- its destinations are never public")
	}
}
