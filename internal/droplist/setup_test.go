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

	onEvent := `interval=5m on-event="/tool fetch url=\"https://mv.example:8443/api/droplist.rsc\" http-header-field=\"Authorization: Bearer <DROP-LIST-KEY>\" check-certificate=yes dst-path=mikroview-drop.rsc; /import file-name=mikroview-drop.rsc"`
	wantScheduler := `:if ([:len [/system scheduler find name=mikroview-drop]] = 0) do={ /system scheduler add name=mikroview-drop ` + onEvent + ` } else={ /system scheduler set [find name=mikroview-drop] ` + onEvent + ` disabled=no }`
	if got.Scheduler != wantScheduler {
		t.Errorf("Scheduler =\n%s\nwant\n%s", got.Scheduler, wantScheduler)
	}

	wantRule := `:if ([:len [/ip firewall raw find comment="mikroview drop list"]] = 0) do={ /ip firewall raw add chain=prerouting src-address-list=mikroview-drop action=drop comment="mikroview drop list" place-before=0 } else={ /ip firewall raw set [find comment="mikroview drop list"] chain=prerouting src-address-list=mikroview-drop action=drop disabled=no }`
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

	onEvent := `interval=5m on-event="/tool fetch url=\"https://mv.example\\\"evil:8443/api/droplist.rsc\" http-header-field=\"Authorization: Bearer <DROP-LIST-KEY>\" check-certificate=yes dst-path=mikroview-drop.rsc; /import file-name=mikroview-drop.rsc"`
	// The guard repeats the settings in both branches, so the escaping
	// has to survive in both -- a re-paste runs the `set`, not the `add`.
	want := `:if ([:len [/system scheduler find name=mikroview-drop]] = 0) do={ /system scheduler add name=mikroview-drop ` + onEvent + ` } else={ /system scheduler set [find name=mikroview-drop] ` + onEvent + ` disabled=no }`
	if got.Scheduler != want {
		t.Errorf("Scheduler =\n%s\nwant\n%s", got.Scheduler, want)
	}
}

// TestNewSetupCommandsAreSafeToPasteTwice is the rule these commands
// live under, said once rather than left implicit in the pinned text
// above. docs/configuration.md tells the operator that minting a key
// again replaces the existing one, so rotating a key hands them this
// same block a second time and they paste it. RouterOS does not
// deduplicate: unguarded, that left two mikroview-drop schedulers --
// the older one still fetching with the key that had just been
// replaced, failing every five minutes -- and a second identical raw
// rule, with nothing on the router to say which was which.
//
// #1266 fixed this for the setup wizard's own scheduler entries. The
// drop list generates its own and was missed.
func TestNewSetupCommandsAreSafeToPasteTwice(t *testing.T) {
	got := NewSetup("mv.example:8443", "<DROP-LIST-KEY>")

	for _, c := range []struct {
		what    string
		command string
		find    string
	}{
		{"Scheduler", got.Scheduler, `[:len [/system scheduler find name=mikroview-drop]] = 0`},
		{"Rule", got.Rule, `[:len [/ip firewall raw find comment="mikroview drop list"]] = 0`},
	} {
		if !strings.HasPrefix(c.command, ":if ("+c.find+") do={") {
			t.Errorf("%s is not guarded by a find check, so pasting it twice adds a second entry:\n%s", c.what, c.command)
		}
		if !strings.Contains(c.command, "} else={") {
			t.Errorf("%s has no else branch, so a re-paste leaves an existing entry as it was rather than converging it:\n%s", c.what, c.command)
		}
	}

	// place-before is where a rule goes, not a property it carries, so
	// it must not reach the else branch -- a rule already on the router
	// keeps the position the operator gave it.
	_, after, found := strings.Cut(got.Rule, "} else={")
	if !found {
		t.Fatalf("Rule has no else branch: %s", got.Rule)
	}
	if strings.Contains(after, "place-before") {
		t.Errorf("the else branch re-places an existing rule; it should only set its properties:\n%s", after)
	}
}

// TestRePasteResumesDisabledEnforcement covers the v0.6.0 fix-batch
// audit's Security stage. #1260 made the setup card safe to paste twice
// by guarding each add with a find and setting the existing entry
// otherwise. But RouterOS's `set` touches only the properties it names,
// and neither branch names `disabled` -- while MikroView itself renders
// two commands that turn enforcement off: DisableRule for the raw rule,
// EmptyList for the scheduler. An operator who stops the drop list
// during an incident and then re-pastes the setup card to resume it got
// no error, and no enforcement either: the rule and the scheduler stayed
// disabled, with nothing in the app saying so.
func TestRePasteResumesDisabledEnforcement(t *testing.T) {
	got := NewSetup("mv.example:8443", "<DROP-LIST-KEY>")

	for _, c := range []struct {
		what    string
		command string
		off     string
	}{
		{"Scheduler", got.Scheduler, "EmptyList"},
		{"Rule", got.Rule, "DisableRule"},
	} {
		_, elseBranch, found := strings.Cut(c.command, "} else={")
		if !found {
			t.Fatalf("%s has no else branch: %s", c.what, c.command)
		}
		if !strings.Contains(elseBranch, "disabled=no") {
			t.Errorf("%s's else branch does not re-enable the entry, so re-pasting after %s reports success and leaves enforcement off:\n%s",
				c.what, c.off, elseBranch)
		}
	}
}
