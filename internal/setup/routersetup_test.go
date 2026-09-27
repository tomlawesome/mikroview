// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routeros"
)

// loggingPage is the ratified #1241 document (the #1206 thread's own
// example, with this instance's address and port), at the wizard version
// given, decoded the way a real push arrives: through
// ingest.DecodePayload, so a test can never assert on a shape the
// decoder would refuse.
func loggingPage(t *testing.T, wizardVersion int, topics string) ingest.Payload {
	t.Helper()
	body := fmt.Sprintf(`{"kind":"logging","page":1,"pages":1,"routerosVersion":"7.16.1","wizardVersion":%d,
 "records":[
  {"type":"action","name":"mikroview","target":"remote","remote":"10.0.0.5","remotePort":"6514","remoteProtocol":"tls","remoteLogFormat":"syslog","checkCertificate":"yes"},
  {"type":"rule","topics":%q,"action":"mikroview","disabled":"no"}
 ]}`, wizardVersion, topics)
	p, err := ingest.DecodePayload(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	return p
}

// wantFor is what the current wizard would leave on a router for an
// instance reachable at 10.0.0.5:8443 with its syslog listener on 6514 --
// the same two values the pasted step-1 block is rendered from.
func wantFor() routeros.LoggingSetup {
	return routeros.WizardLogging("10.0.0.5:8443", "6514", "a")
}

func TestRouterSetupReadsTheCurrentWizardAsCurrent(t *testing.T) {
	s := New()
	s.NoteLoggingReport("core", loggingPage(t, routeros.WizardVersion, "firewall,info"), time.Now())

	got := s.RouterSetup("core", wantFor())
	if got.Standing != StandingCurrent {
		t.Errorf("standing = %q, want %q", got.Standing, StandingCurrent)
	}
	if got.CurrentVersion != routeros.WizardVersion || got.ScriptVersion != routeros.WizardVersion {
		t.Errorf("versions = script %d, current %d; want both %d", got.ScriptVersion, got.CurrentVersion, routeros.WizardVersion)
	}
}

func TestRouterSetupReadsAnOlderScriptVersionAsBehind(t *testing.T) {
	s := New()
	s.NoteLoggingReport("core", loggingPage(t, routeros.WizardVersion-1, "firewall,info"), time.Now())

	got := s.RouterSetup("core", wantFor())
	if got.Standing != StandingBehind {
		t.Errorf("standing = %q, want %q", got.Standing, StandingBehind)
	}
	if got.ScriptVersion != routeros.WizardVersion-1 {
		t.Errorf("scriptVersion = %d, want %d", got.ScriptVersion, routeros.WizardVersion-1)
	}
}

// A router can be on the current script and still be wrong: the operator
// edited the rule afterwards, or an older paste left topics the current
// wizard would not write. #1241 counts the rules' topics as drift, so
// this reads as behind at the current version.
func TestRouterSetupReadsTopicsDriftAtTheCurrentVersionAsBehind(t *testing.T) {
	s := New()
	s.NoteLoggingReport("core", loggingPage(t, routeros.WizardVersion, "firewall"), time.Now())

	if got := s.RouterSetup("core", wantFor()).Standing; got != StandingBehind {
		t.Errorf("standing = %q, want %q -- the rule's topics are not what the wizard writes", got, StandingBehind)
	}
}

// Topics are a set, and RouterOS prints them in its own order.
func TestRouterSetupIgnoresTopicOrder(t *testing.T) {
	s := New()
	s.NoteLoggingReport("core", loggingPage(t, routeros.WizardVersion, "info,firewall"), time.Now())

	if got := s.RouterSetup("core", wantFor()).Standing; got != StandingCurrent {
		t.Errorf("standing = %q, want %q -- the same two topics in the other order", got, StandingCurrent)
	}
}

func TestRouterSetupReadsAnUnreportedDeviceAsNeverReported(t *testing.T) {
	s := New()
	got := s.RouterSetup("core", wantFor())
	if got.Standing != StandingNeverReported {
		t.Errorf("standing = %q, want %q", got.Standing, StandingNeverReported)
	}
	if got.ScriptVersion != 0 {
		t.Errorf("scriptVersion = %d, want 0 -- nothing was ever reported", got.ScriptVersion)
	}
}

// The remote the router points at is drift too: a router logging to some
// other host is not set up the way this instance's wizard sets one up,
// whatever version wrote the script.
func TestRouterSetupReadsARemoteMismatchAsBehind(t *testing.T) {
	s := New()
	s.NoteLoggingReport("core", loggingPage(t, routeros.WizardVersion, "firewall,info"), time.Now())

	elsewhere := routeros.WizardLogging("192.0.2.77:8443", "6514", "a")
	if got := s.RouterSetup("core", elsewhere).Standing; got != StandingBehind {
		t.Errorf("standing = %q, want %q -- the action points at another host", got, StandingBehind)
	}
}

// An instance that has not answered "what address can your router reach
// me on?" has no expectation to hold a router to, and must not report
// every router behind over a question it has not answered itself.
func TestRouterSetupDoesNotCompareAnAddressTheInstanceDoesNotKnow(t *testing.T) {
	s := New()
	s.NoteLoggingReport("core", loggingPage(t, routeros.WizardVersion, "firewall,info"), time.Now())

	if got := s.RouterSetup("core", routeros.WizardLogging("", "", "a")).Standing; got != StandingCurrent {
		t.Errorf("standing = %q, want %q -- nothing known to compare the remote against", got, StandingCurrent)
	}
}

// The report is persisted, not observed: a router whose script predates
// the page never sends one, so an instance that forgot the last page at
// restart could not tell that router from one whose next push is not due
// for another twenty minutes.
func TestLoggingReportSurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.NoteLoggingReport("core", loggingPage(t, routeros.WizardVersion, "firewall,info"), time.Now())

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	if got := reopened.RouterSetup("core", wantFor()).Standing; got != StandingCurrent {
		t.Errorf("standing after a restart = %q, want %q", got, StandingCurrent)
	}
	reports := reopened.LoggingReports()
	if len(reports) != 1 || len(reports[0].Records) != 2 {
		t.Fatalf("reports after a restart = %+v, want one page of two records", reports)
	}
	if reports[0].Records[0].Remote != "10.0.0.5" {
		t.Errorf("the reloaded action's remote = %q, want the reported one", reports[0].Records[0].Remote)
	}
}

// TestLoggingLeftoversFindsTheOwnersOwnRouter is #1373's reproduction:
// the owner's own router, set up across several MikroView versions,
// still had the built-in "memory" and "remote" actions repointed at
// mikroview from an older setup, each still sending from
// 192.168.254.1 -- one over TLS/6514 (so it looked, on the wire, like a
// second copy of the real thing), the other over plain UDP/514. The
// current "mikroview" action was already correct (src-address=0.0.0.0)
// and must not be named as a leftover of itself.
func TestLoggingLeftoversFindsTheOwnersOwnRouter(t *testing.T) {
	s := New()
	body := `{"kind":"logging","page":1,"pages":1,"routerosVersion":"7.16.1","wizardVersion":3,
 "records":[
  {"type":"action","name":"memory","target":"remote","remote":"10.0.0.5","remotePort":"6514","srcAddress":"192.168.254.1","remoteLogFormat":"default","remoteProtocol":"tls","checkCertificate":"no"},
  {"type":"action","name":"remote","target":"remote","remote":"10.0.0.5","remotePort":"514","srcAddress":"192.168.254.1","remoteLogFormat":"default","remoteProtocol":"udp"},
  {"type":"action","name":"mikroview","target":"remote","remote":"10.0.0.5","remotePort":"6514","srcAddress":"0.0.0.0","remoteProtocol":"tls","remoteLogFormat":"syslog","checkCertificate":"yes"},
  {"type":"rule","topics":"firewall,info","action":"mikroview","disabled":"no"}
 ]}`
	p, err := ingest.DecodePayload(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	s.NoteLoggingReport("core", p, time.Now())

	got := s.LoggingLeftovers("core", wantFor())
	if len(got) != 2 {
		t.Fatalf("leftovers = %+v, want exactly 2 (memory, remote) -- and no complaint about mikroview", got)
	}
	byName := map[string]LoggingLeftover{got[0].Name: got[0], got[1].Name: got[1]}

	memory, ok := byName["memory"]
	if !ok {
		t.Fatalf("no leftover named %q in %+v", "memory", got)
	}
	if !memory.Builtin {
		t.Errorf("memory leftover.Builtin = false, want true")
	}
	if want := `/system logging action set [find name=memory] target=memory`; len(memory.Commands) != 1 || memory.Commands[0] != want {
		t.Errorf("memory leftover.Commands = %v, want [%q]", memory.Commands, want)
	}

	remote, ok := byName["remote"]
	if !ok {
		t.Fatalf("no leftover named %q in %+v", "remote", got)
	}
	if !remote.Builtin {
		t.Errorf("remote leftover.Builtin = false, want true")
	}
	if want := `/system logging action set [find name=remote] remote=0.0.0.0 src-address=0.0.0.0`; len(remote.Commands) != 1 || remote.Commands[0] != want {
		t.Errorf("remote leftover.Commands = %v, want [%q]", remote.Commands, want)
	}

	for _, l := range got {
		if l.Name == "mikroview" {
			t.Errorf("leftovers named the current mikroview action itself: %+v", got)
		}
	}
}

// A non-built-in action left pointed at this instance is removed
// entirely, rules first -- RouterOS refuses to remove an action a rule
// still references.
func TestLoggingLeftoversRemovesANonBuiltinAction(t *testing.T) {
	s := New()
	body := `{"kind":"logging","page":1,"pages":1,"routerosVersion":"7.16.1","wizardVersion":3,
 "records":[
  {"type":"action","name":"old-syslog","target":"remote","remote":"10.0.0.5","remotePort":"514","srcAddress":"0.0.0.0","remoteProtocol":"udp","remoteLogFormat":"syslog"},
  {"type":"rule","topics":"firewall","action":"old-syslog","disabled":"no"}
 ]}`
	p, err := ingest.DecodePayload(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	s.NoteLoggingReport("core", p, time.Now())

	got := s.LoggingLeftovers("core", wantFor())
	if len(got) != 1 {
		t.Fatalf("leftovers = %+v, want exactly 1", got)
	}
	if got[0].Builtin {
		t.Errorf("old-syslog leftover.Builtin = true, want false")
	}
	want := []string{
		`/system logging remove [find action=old-syslog]`,
		`/system logging action remove [find name=old-syslog]`,
	}
	if len(got[0].Commands) != 2 || got[0].Commands[0] != want[0] || got[0].Commands[1] != want[1] {
		t.Errorf("old-syslog leftover.Commands = %v, want %v", got[0].Commands, want)
	}
}

// An instance that does not know its own address has nothing to check
// leftovers against, the same rule RouterSetup follows.
func TestLoggingLeftoversDoesNotCompareAnAddressTheInstanceDoesNotKnow(t *testing.T) {
	s := New()
	s.NoteLoggingReport("core", loggingPage(t, routeros.WizardVersion, "firewall,info"), time.Now())

	if got := s.LoggingLeftovers("core", routeros.WizardLogging("", "", "a")); got != nil {
		t.Errorf("leftovers = %+v, want nil -- nothing known to compare against", got)
	}
}

// The marks and the address the ledger already persisted must survive a
// report being written beside them -- the two halves share one document.
func TestLoggingReportDoesNotDisturbTheLedgersOtherHalves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.SetAddress("10.0.0.5:8443"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.NoteMark(3, MarkSkipped, "admin", "nothing to tag", time.Now()); err != nil {
		t.Fatal(err)
	}
	s.NoteLoggingReport("core", loggingPage(t, routeros.WizardVersion, "firewall,info"), time.Now())

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	if reopened.Address() != "10.0.0.5:8443" {
		t.Errorf("address after a restart = %q", reopened.Address())
	}
	if marks := reopened.Marks(); len(marks) != 1 || marks[0].Step != 3 {
		t.Errorf("marks after a restart = %+v, want the one step 3 skip", marks)
	}
}
