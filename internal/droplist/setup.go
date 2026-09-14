// SPDX-License-Identifier: AGPL-3.0-only

package droplist

import (
	"fmt"

	"github.com/tomlawesome/mikroview/internal/routeros"
)

// Setup is the four RouterOS command strings issue #1225's setup card
// renders for an operator to paste onto a router by hand: the scheduled
// fetch-and-import that keeps the router's drop list current, the
// firewall rule that actually blocks it, and the two commands that undo
// either one. Nothing here is ever run by mikroview itself -- see this
// package's own doc comment: stage 1 through this stage is the store,
// the feed and the rendered text only, never a connection out to a
// router.
type Setup struct {
	// Scheduler fetches the .rsc feed every 5 minutes and imports it.
	// Reaching mikroview over TLS at all depends on the router already
	// trusting mikroview's CA -- the ingest setup wizard's own
	// mikroview-ca.crt fetch (internal/routeros/commands.go's
	// CaTrustCommands) -- which the operator has necessarily already
	// done if this router's state ever reached the page this is
	// rendered on.
	Scheduler string `json:"scheduler"`
	// Rule adds the firewall rule that actually drops traffic from
	// ListName.
	Rule string `json:"rule"`
	// DisableRule turns Rule off without deleting it, so re-enabling
	// later needs no re-typing.
	DisableRule string `json:"disableRule"`
	// EmptyList clears every address ListName currently holds, without
	// touching Rule or Scheduler.
	EmptyList string `json:"emptyList"`
}

// dropListRuleComment marks the one firewall rule Rule/DisableRule ever
// touch -- named once so the two can never drift apart, the same reason
// ListName itself is a constant rather than a repeated literal.
const dropListRuleComment = "mikroview drop list"

// NewSetup renders Setup for address (the host[:port] a router reaches
// mikroview on -- GET /api/droplist's own "address" query parameter, or
// the request's Host when that is absent) and key (the droplist-pull
// bearer token: the literal placeholder "<DROP-LIST-KEY>" on the list
// response, since the real value is never shown there, and the real
// minted value on the mint response, the one place it is ever shown).
//
// Scheduler nests two layers of RouterOS string quoting: address is
// escaped first for the single-quoted context it sits in inside the
// fetch command's own url="..." (routeros.QuoteScriptString, not just
// backslash/quote, since an unescaped "$" would itself expand when the
// fetch line eventually runs -- see QuoteScriptString's own doc
// comment), and then the whole fetch-and-import command is escaped
// again for the scheduler's on-event="..." it is embedded in. Mirrors
// how internal/routeros/commands.go:267's push /tool fetch line quotes
// its own address, and how scriptSource nests a second escape around a
// whole saved script's source="...".
func NewSetup(address, key string) Setup {
	quotedAddress := routeros.QuoteScriptString(address)
	inner := fmt.Sprintf(
		`/tool fetch url="https://%s/api/droplist.rsc" http-header-field="Authorization: Bearer %s" check-certificate=yes dst-path=%s.rsc; /import file-name=%s.rsc`,
		quotedAddress, key, ListName, ListName)

	return Setup{
		Scheduler: fmt.Sprintf(`/system scheduler add name=%s interval=5m on-event="%s"`,
			ListName, routeros.QuoteScriptString(inner)),
		Rule: fmt.Sprintf(`/ip firewall raw add chain=prerouting src-address-list=%s action=drop comment="%s" place-before=0`,
			ListName, dropListRuleComment),
		DisableRule: fmt.Sprintf(`/ip firewall raw disable [find comment="%s"]`, dropListRuleComment),
		EmptyList:   fmt.Sprintf(`/ip firewall address-list remove [find list=%s]`, ListName),
	}
}
