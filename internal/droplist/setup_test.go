// SPDX-License-Identifier: AGPL-3.0-only

package droplist

import (
	"strings"
	"testing"
)

// TestNewSetupRendersExactCommands is issue #1225's setup card content,
// pinned exactly: an operator pastes these verbatim, so a stray
// character (a missing check-certificate=yes, a wrong list name, a
// quoting mistake that breaks the scheduler's own on-event="...") is a
// defect the moment it changes this output, not something a looser
// substring check would ever catch.
func TestNewSetupRendersExactCommands(t *testing.T) {
	got := NewSetup("mv.example:8443", "<DROP-LIST-KEY>")

	wantScheduler := `/system scheduler add name=mikroview-drop interval=5m on-event="/tool fetch url=\"https://mv.example:8443/api/droplist.rsc\" http-header-field=\"Authorization: Bearer <DROP-LIST-KEY>\" check-certificate=yes dst-path=mikroview-drop.rsc; /import file-name=mikroview-drop.rsc"`
	if got.Scheduler != wantScheduler {
		t.Errorf("Scheduler =\n%s\nwant\n%s", got.Scheduler, wantScheduler)
	}

	wantRule := `/ip firewall raw add chain=prerouting src-address-list=mikroview-drop action=drop comment="mikroview drop list" place-before=0`
	if got.Rule != wantRule {
		t.Errorf("Rule = %q, want %q", got.Rule, wantRule)
	}

	wantDisableRule := `/ip firewall raw disable [find comment="mikroview drop list"]`
	if got.DisableRule != wantDisableRule {
		t.Errorf("DisableRule = %q, want %q", got.DisableRule, wantDisableRule)
	}

	wantEmptyList := `/system scheduler disable [find name=mikroview-drop]; /ip firewall address-list remove [find list=mikroview-drop]`
	if got.EmptyList != wantEmptyList {
		t.Errorf("EmptyList = %q, want %q", got.EmptyList, wantEmptyList)
	}
}

// TestEmptyListAlsoDisablesTheScheduler covers #1260: EmptyList used to
// only remove the live list's entries, but the router's own 5-minute
// scheduler pulls mikroview's feed again on its own and refills the
// list with whatever mikroview still has stored -- an emergency stop
// that reversed itself within five minutes and left the operator
// believing traffic was unblocked when it was not. EmptyList must also
// disable the scheduler entry (by its Scheduler-assigned name) that
// would otherwise undo it.
func TestEmptyListAlsoDisablesTheScheduler(t *testing.T) {
	got := NewSetup("mv.example:8443", "<DROP-LIST-KEY>")

	if !strings.Contains(got.EmptyList, "/system scheduler disable [find name=mikroview-drop]") {
		t.Errorf("EmptyList does not disable the scheduler that would refill it: %q", got.EmptyList)
	}
	if !strings.Contains(got.EmptyList, "/ip firewall address-list remove [find list=mikroview-drop]") {
		t.Errorf("EmptyList lost the list removal: %q", got.EmptyList)
	}
}

// TestNewSetupEscapesAQuoteInTheAddress covers the injection direction a
// security review would ask about first: an address carrying a `"`
// must come out escaped through both layers of quoting (see NewSetup's
// own doc comment), never breaking out of the scheduler's on-event="..."
// or of the fetch command's own url="..." nested inside it.
func TestNewSetupEscapesAQuoteInTheAddress(t *testing.T) {
	got := NewSetup(`mv.example"evil:8443`, "<DROP-LIST-KEY>")

	want := `/system scheduler add name=mikroview-drop interval=5m on-event="/tool fetch url=\"https://mv.example\\\"evil:8443/api/droplist.rsc\" http-header-field=\"Authorization: Bearer <DROP-LIST-KEY>\" check-certificate=yes dst-path=mikroview-drop.rsc; /import file-name=mikroview-drop.rsc"`
	if got.Scheduler != want {
		t.Errorf("Scheduler =\n%s\nwant\n%s", got.Scheduler, want)
	}
}
