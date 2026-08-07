// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

func mailEvt(srcIP, dstIP string, dstPort int, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: dstIP, DstPort: dstPort, ReceivedAt: at}
}

func TestMailSenderFlagsUntaggedSourceOnce(t *testing.T) {
	d, fs := newTestDetector(t, DefaultConfig())

	now := time.Now()
	d.Observe(mailEvt("192.168.1.50", "203.0.113.9", 25, now))

	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeUnexpectedMailSender || list[0].Target != "192.168.1.50" {
		t.Fatalf("expected one unexpected_mail_sender flag keyed by source IP, got %+v", list)
	}
	if list[0].Confidence != nil {
		t.Errorf("expected no confidence score (deterministic, unscored like stale_rule/new_device), got %v", *list[0].Confidence)
	}

	// A second SMTP connection from the same still-untagged source
	// re-fires the same episode in place, not a second flag.
	d.Observe(mailEvt("192.168.1.50", "198.51.100.4", 587, now.Add(time.Second)))
	list = fs.List()
	if len(list) != 1 {
		t.Fatalf("expected the flag to update in place rather than duplicate, got %+v", list)
	}
	if list[0].Count != 2 {
		t.Errorf("expected Count to bump on re-fire, got %d", list[0].Count)
	}
}

func TestMailSenderSkipsTaggedSource(t *testing.T) {
	es, err := entities.Open("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := es.Upsert(entities.Entity{
		Type: entities.TypeHost, Key: "192.168.1.50", Tags: []string{"trusted-mail-sender"},
	}); err != nil {
		t.Fatal(err)
	}

	d, fs := newTestDetector(t, DefaultConfig())
	d.WithEntities(es)

	now := time.Now()
	for _, port := range []int{25, 465, 587} {
		d.Observe(mailEvt("192.168.1.50", "203.0.113.9", port, now))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected a trusted-mail-sender-tagged source to never flag, got %+v", fs.List())
	}
}

func TestMailSenderOtherHostsStillFlagWhenOneIsTagged(t *testing.T) {
	es, err := entities.Open("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := es.Upsert(entities.Entity{
		Type: entities.TypeHost, Key: "192.168.1.50", Tags: []string{"trusted-mail-sender"},
	}); err != nil {
		t.Fatal(err)
	}

	d, fs := newTestDetector(t, DefaultConfig())
	d.WithEntities(es)

	now := time.Now()
	d.Observe(mailEvt("192.168.1.50", "203.0.113.9", 25, now))
	d.Observe(mailEvt("192.168.1.99", "203.0.113.9", 25, now))

	list := fs.List()
	if len(list) != 1 || list[0].Target != "192.168.1.99" {
		t.Fatalf("expected only the untagged source to flag, got %+v", list)
	}
}

func TestMailSenderIgnoresNonSMTPPorts(t *testing.T) {
	d, fs := newTestDetector(t, DefaultConfig())

	now := time.Now()
	for _, port := range []int{22, 80, 443, 8080} {
		d.Observe(mailEvt("192.168.1.50", "203.0.113.9", port, now))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected non-SMTP ports to never flag, got %+v", fs.List())
	}
}

func TestMailSenderIgnoresExternalToInternal(t *testing.T) {
	d, fs := newTestDetector(t, DefaultConfig())

	now := time.Now()
	// source is public, destination is the LAN -- inbound mail to a
	// self-hosted mail server, not an outbound-sending LAN host.
	d.Observe(mailEvt("203.0.113.9", "192.168.1.50", 25, now))
	if len(fs.List()) != 0 {
		t.Fatalf("expected external source -> internal destination to never flag, got %+v", fs.List())
	}
}

func TestMailSenderIgnoresInternalToInternal(t *testing.T) {
	d, fs := newTestDetector(t, DefaultConfig())

	now := time.Now()
	d.Observe(mailEvt("192.168.1.50", "192.168.1.60", 25, now))
	if len(fs.List()) != 0 {
		t.Fatalf("expected internal source -> internal destination to never flag, got %+v", fs.List())
	}
}

func TestMailSenderNilEntitiesFlagsEveryUntaggedSource(t *testing.T) {
	// WithEntities is never called -- d.entities stays nil, which must
	// behave as "nothing is tagged trusted," not panic or silently skip
	// every source.
	d, fs := newTestDetector(t, DefaultConfig())

	now := time.Now()
	d.Observe(mailEvt("192.168.1.50", "203.0.113.9", 465, now))
	if len(fs.List()) != 1 {
		t.Fatalf("expected a flag with no entity store configured, got %+v", fs.List())
	}
}
