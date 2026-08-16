// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// entityTagLookupAdapter is main.go's entityTagLookup, small enough to
// keep a test-local copy of rather than exporting -- the same
// per-package-fake convention every other dependency here follows.
type entityTagLookupAdapter struct{ es *entities.Store }

func (a entityTagLookupAdapter) HasTag(entityType, id, tag string) bool {
	return a.es.HasTag(entityType, id, tag)
}

func newShippedMailSenderDefinition(t *testing.T, fs *flags.Store, es EntityTagLookup) *mailSenderDefinition {
	t.Helper()
	params := Params{
		"ports":      []int{25, 465, 587},
		"trustedTag": []string{"trusted-mail-sender"},
	}
	def := Definition{
		ID:          "unexpected_mail_sender",
		Name:        "Unexpected mail sender",
		Intent:      IntentDetection,
		Kind:        KindProgrammatic,
		Enabled:     true,
		Params:      params,
		ParamSchema: UnexpectedMailSenderParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{Entities: es})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(unexpected_mail_sender): %v", err)
	}
	d := built.(*mailSenderDefinition)
	d.SetSink(FlagsSink(fs))
	return d
}

// mailEvt is internal/detect/mail_sender_test.go's helper of the same
// name.
func mailEvt(srcIP, dstIP string, dstPort int, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: dstIP, DstPort: dstPort, ReceivedAt: at}
}

func taggedEntityStore(t *testing.T, host string) EntityTagLookup {
	t.Helper()
	es, err := entities.Open("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := es.Upsert(entities.Entity{
		Type: entities.TypeHost, Key: host, Tags: []string{"trusted-mail-sender"},
	}); err != nil {
		t.Fatal(err)
	}
	return entityTagLookupAdapter{es: es}
}

func TestShippedMailSenderFlagsUntaggedSourceOnce(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedMailSenderDefinition(t, fs, nil)

	now := time.Now()
	d.Evaluate(mailEvt("192.168.1.50", "203.0.113.9", 25, now))

	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeUnexpectedMailSender || list[0].Target != "192.168.1.50" {
		t.Fatalf("expected one unexpected_mail_sender flag keyed by source IP, got %+v", list)
	}
	if want := "outbound connection to 203.0.113.9:25 (SMTP)"; list[0].Detail != want {
		t.Errorf("Detail = %q, want %q", list[0].Detail, want)
	}
	if list[0].Confidence != nil {
		t.Errorf("expected no confidence score (deterministic, unscored like stale_rule/new_device), got %v", *list[0].Confidence)
	}
	if len(list[0].Evidence.Ports) != 0 || len(list[0].Evidence.Hosts) != 0 {
		t.Errorf("expected empty Evidence, got %+v", list[0].Evidence)
	}
	if list[0].Country != "" {
		t.Errorf("expected no country badge for a LAN source, got %q", list[0].Country)
	}

	// A second SMTP connection from the same still-untagged source
	// re-fires the same episode in place, not a second flag.
	d.Evaluate(mailEvt("192.168.1.50", "198.51.100.4", 587, now.Add(time.Second)))
	list = fs.List()
	if len(list) != 1 {
		t.Fatalf("expected the flag to update in place rather than duplicate, got %+v", list)
	}
	if list[0].Count != 2 {
		t.Errorf("expected Count to bump on re-fire, got %d", list[0].Count)
	}
}

func TestShippedMailSenderSkipsTaggedSource(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedMailSenderDefinition(t, fs, taggedEntityStore(t, "192.168.1.50"))

	now := time.Now()
	for _, port := range []int{25, 465, 587} {
		d.Evaluate(mailEvt("192.168.1.50", "203.0.113.9", port, now))
	}
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected a trusted-mail-sender-tagged source to never flag, got %+v", got)
	}
}

func TestShippedMailSenderOtherHostsStillFlagWhenOneIsTagged(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedMailSenderDefinition(t, fs, taggedEntityStore(t, "192.168.1.50"))

	now := time.Now()
	d.Evaluate(mailEvt("192.168.1.50", "203.0.113.9", 25, now))
	d.Evaluate(mailEvt("192.168.1.99", "203.0.113.9", 25, now))

	list := fs.List()
	if len(list) != 1 || list[0].Target != "192.168.1.99" {
		t.Fatalf("expected only the untagged source to flag, got %+v", list)
	}
}

func TestShippedMailSenderIgnoresNonSMTPPorts(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedMailSenderDefinition(t, fs, nil)

	now := time.Now()
	for _, port := range []int{22, 80, 443, 8080} {
		d.Evaluate(mailEvt("192.168.1.50", "203.0.113.9", port, now))
	}
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected non-SMTP ports to never flag, got %+v", got)
	}
}

func TestShippedMailSenderIgnoresExternalToInternal(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedMailSenderDefinition(t, fs, nil)

	// Source is public, destination is the LAN -- inbound mail to a
	// self-hosted mail server, not an outbound-sending LAN host.
	d.Evaluate(mailEvt("203.0.113.9", "192.168.1.50", 25, time.Now()))
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected external source -> internal destination to never flag, got %+v", got)
	}
}

func TestShippedMailSenderIgnoresInternalToInternal(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedMailSenderDefinition(t, fs, nil)

	d.Evaluate(mailEvt("192.168.1.50", "192.168.1.60", 25, time.Now()))
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected internal source -> internal destination to never flag, got %+v", got)
	}
}

// TestShippedMailSenderNilEntitiesFlagsEveryUntaggedSource pins the
// nil-is-inert contract: no entity store configured must mean "nothing
// is tagged trusted", never a panic and never a silent skip of every
// source.
func TestShippedMailSenderNilEntitiesFlagsEveryUntaggedSource(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedMailSenderDefinition(t, fs, nil)

	d.Evaluate(mailEvt("192.168.1.50", "203.0.113.9", 465, time.Now()))
	if got := fs.List(); len(got) != 1 {
		t.Fatalf("expected a flag with no entity store configured, got %+v", got)
	}
}

// TestShippedMailSenderIsNonReplayable pins the declaration, not just
// its existence: an entity-tag allowlist has no history, so a replay
// could only apply today's tags to yesterday's events.
func TestShippedMailSenderIsNonReplayable(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedMailSenderDefinition(t, fs, nil)

	receiptCapable, reason, ok := Replayability(d)
	if !ok {
		t.Fatal("Replayability could not classify unexpected_mail_sender")
	}
	if receiptCapable {
		t.Fatal("expected unexpected_mail_sender to declare itself non-replayable")
	}
	if reason == "" {
		t.Error("a non-replayable declaration with no reason is the thing the contract exists to prevent")
	}
}
