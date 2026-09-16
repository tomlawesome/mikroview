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

// The marks and the address the ledger already persisted must survive a
// report being written beside them -- the two halves share one document.
func TestLoggingReportDoesNotDisturbTheLedgersOtherHalves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.SetAddress("10.0.0.5:8443")
	s.NoteMark(3, MarkSkipped, "admin", "nothing to tag", time.Now())
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
