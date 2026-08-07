package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// tagTrustedMailSender is the entity tag (internal/entities, issue #107)
// an admin attaches to a host entity (entities.TypeHost) to mark it as
// a known, legitimate outbound mail sender -- a self-hoster running
// their own mail server sets this once, in the UI, so mikroview never
// flags that specific host for TypeUnexpectedMailSender again. A plain
// string, not a package-level enum, for the same reason entities.Type
// itself is a plain string (see that package's doc comment): the tag
// vocabulary is open-ended, and internal/entities deliberately doesn't
// gatekeep it.
const tagTrustedMailSender = "trusted-mail-sender"

// mailPorts are the destination ports treated as outbound SMTP: the
// unencrypted, implicit-TLS, and STARTTLS submission ports respectively
// -- the same fixed-list simplicity as Config.CriticalPorts, just for
// the opposite direction (a LAN source originating a connection out,
// not an external source connecting in).
var mailPorts = map[int]bool{25: true, 465: true, 587: true}

func isMailPort(port int) bool {
	return mailPorts[port]
}

// observeMailSender implements issue #108: a LAN host that's never been
// tagged tagTrustedMailSender originating an outbound connection to an
// external destination on one of mailPorts is a strong, simple,
// deterministic compromised-device/spambot signal -- a host with no
// established, admin-acknowledged reason to send mail suddenly doing so.
// Deliberately deterministic, unlike most of this package's other
// detectors: no threshold, window, or EMA baseline to tune, matching
// TypeNewDevice/TypeStaleRule's "have I ever seen this before" shape --
// see flags.TypeUnexpectedMailSender's doc comment. Only called for
// events already known to be internal-source/external-destination on a
// mail port (see Observe's dispatch), so this never needs to re-check
// direction or port itself.
//
// Always on, unlike every DetectorName-backed detector in this package
// -- there's no per-source threshold or window to scope, so a live
// enable/scope toggle would have nothing meaningful to restrict beyond
// what the entity-store allowlist already provides. See settings.go's
// DetectorName doc comment and flags.TypeNewDevice/TypeStaleRule for the
// same "raised outside the generic per-event pipeline" precedent.
func (d *Detector) observeMailSender(e store.Event, now time.Time) {
	if d.entities != nil && d.entities.HasTag(entities.TypeHost, e.SrcIP, tagTrustedMailSender) {
		return
	}
	d.fs.Add(flags.TypeUnexpectedMailSender, e.SrcIP,
		fmt.Sprintf("outbound connection to %s:%d (SMTP)", e.DstIP, e.DstPort),
		now)
}
