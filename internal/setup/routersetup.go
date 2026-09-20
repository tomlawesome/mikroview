// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"sort"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routeros"
)

// The router-side half of the upgrade framework (#1241,
// docs/decisions/upgrade-framework.md). The wizard's pasted push script
// sends one page per cycle saying what the wizard left on that router --
// the mikroview logging action, the rules feeding it, and which version
// of the wizard wrote the script. This file keeps the last such page per
// device and compares it against what the current wizard would leave.
//
// Persisted, unlike the observations at the top of setup.go. Those are
// re-made from arriving traffic every run; this one is not, in the case
// that matters most: a router whose script is old never sends the page
// at all, so a restart with nothing on disk cannot tell "never reported"
// from "not yet this cycle" -- and a push cycle is 20 minutes. The last
// page a router sent is a fact about that router's setup, not a claim
// about a router nobody is watching, which is the line the package
// comment draws around the observations.

// Standing is how one device's setup compares to the current wizard's.
// The three answers #1241 names, and no fourth: a device either matches
// what the wizard would write, does not, or has never said.
type Standing string

const (
	// StandingCurrent means the device reported the current wizard
	// version and no drift in the fields that count.
	StandingCurrent Standing = "current"
	// StandingBehind means either an older wizard version, or the
	// current one with drift in a field that counts -- both mean the
	// same thing to an operator (paste step 1 again), so they read the
	// same.
	StandingBehind Standing = "behind"
	// StandingNeverReported means this device has never sent the page:
	// a router still running a script pasted before the page existed.
	// The absence is the signal -- see the upgrade-framework decision.
	StandingNeverReported Standing = "never reported"
)

// RouterSetup is one device's answer, as the device API serves it.
// ScriptVersion is what the router's own script reported (0 when it has
// never reported), CurrentVersion is routeros.WizardVersion -- both
// carried so a reader can say "script v3, current v5" without asking a
// second question.
type RouterSetup struct {
	Standing       Standing  `json:"standing"`
	ScriptVersion  int       `json:"scriptVersion"`
	CurrentVersion int       `json:"currentVersion"`
	ReportedAt     time.Time `json:"reportedAt,omitzero"`
}

// LoggingReport is the last logging page one device sent, kept whole
// rather than reduced to the handful of fields the comparison reads.
// The page is small and fixed (one action, a rule or two), and keeping
// it whole means a later change to what counts as drift re-reads the
// same stored page instead of needing every router to push again.
type LoggingReport struct {
	Device        string                `json:"device"`
	At            time.Time             `json:"at"`
	WizardVersion int                   `json:"wizardVersion"`
	Records       []ingest.LoggingEntry `json:"records"`
}

// NoteLoggingReport records one device's logging page, replacing
// whatever it last sent. Called from the ingest handler for a
// KindLogging payload and nowhere else: this is the router's own report
// about itself, never something a browser can assert.
//
// Persists immediately. A push is a 20-minute-cadence event, not a hot
// path, and the same reasoning NoteMark gives applies -- though here the
// cost is bounded further by only writing when the page actually changed
// (a router pushing an unchanged setup every 20 minutes would otherwise
// rewrite the ledger 72 times a day to say nothing new).
func (s *Store) NoteLoggingReport(device string, p ingest.Payload, now time.Time) {
	if device == "" || p.Kind != ingest.KindLogging {
		return
	}
	report := LoggingReport{Device: device, At: now, WizardVersion: p.WizardVersion, Records: p.Logging}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reports == nil {
		s.reports = make(map[string]LoggingReport)
	}
	prev, seen := s.reports[device]
	if !seen {
		evictOldest(s.reports, maxSources, func(r LoggingReport) time.Time { return r.At })
	}
	s.reports[device] = report
	if seen && sameReport(prev, report) {
		return
	}
	s.persistLocked()
}

// sameReport reports whether two pages say the same thing about a
// router's setup -- everything but when it was said.
func sameReport(a, b LoggingReport) bool {
	if a.WizardVersion != b.WizardVersion || len(a.Records) != len(b.Records) {
		return false
	}
	for i := range a.Records {
		if !sameEntry(a.Records[i], b.Records[i]) {
			return false
		}
	}
	return true
}

func sameEntry(a, b ingest.LoggingEntry) bool {
	if a.Type != b.Type || a.Name != b.Name || a.Target != b.Target ||
		a.Remote != b.Remote || a.RemotePort != b.RemotePort ||
		a.RemoteProtocol != b.RemoteProtocol || a.RemoteLogFormat != b.RemoteLogFormat ||
		a.CheckCertificate != b.CheckCertificate || a.Action != b.Action || a.Disabled != b.Disabled {
		return false
	}
	if len(a.Topics) != len(b.Topics) {
		return false
	}
	for i := range a.Topics {
		if a.Topics[i] != b.Topics[i] {
			return false
		}
	}
	return true
}

// LoggingReports returns every stored page, device order, for a caller
// that wants the evidence rather than the verdict.
func (s *Store) LoggingReports() []LoggingReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.reportsLocked()
}

func (s *Store) reportsLocked() []LoggingReport {
	out := make([]LoggingReport, 0, len(s.reports))
	for _, r := range s.reports {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Device < out[j].Device })
	return out
}

// RouterSetup compares what device last reported against want -- what
// the current wizard would leave on a router for this instance
// (routeros.WizardLogging).
//
// Drift is #1241's list and only that: the action's remote, remote-port
// and remote-log-format, and the rules' topics. A router may carry any
// other logging configuration, and any other property on the mikroview
// action itself, without being called behind for it -- mikroview checks
// that its own setup is still what it would write, not that the router
// is arranged to its liking.
//
// A want field left empty is not compared: an instance that does not
// know its own address or syslog port has no expectation to hold a
// router to, and inventing one would report every router behind over a
// question mikroview has not answered itself.
func (s *Store) RouterSetup(device string, want routeros.LoggingSetup) RouterSetup {
	out := RouterSetup{Standing: StandingNeverReported, CurrentVersion: routeros.WizardVersion}

	s.mu.RLock()
	report, ok := s.reports[device]
	s.mu.RUnlock()
	if !ok {
		return out
	}

	out.ScriptVersion = report.WizardVersion
	out.ReportedAt = report.At
	out.Standing = StandingCurrent
	if report.WizardVersion < routeros.WizardVersion || driftsFrom(report, want) {
		out.Standing = StandingBehind
	}
	return out
}

// driftsFrom reports whether a reported page differs from what the
// current wizard would leave, in the fields that count.
//
// A page carrying no mikroview action at all, or no rule feeding it, is
// drift: the wizard writes both, so a router missing either is not set
// up the way the current wizard sets one up -- and the operator's remedy
// is the same paste either way.
func driftsFrom(report LoggingReport, want routeros.LoggingSetup) bool {
	actions, rules := 0, 0
	for _, rec := range report.Records {
		switch rec.Type {
		case ingest.LoggingTypeAction:
			actions++
			if want.Remote != "" && rec.Remote != want.Remote {
				return true
			}
			if want.RemotePort != "" && string(rec.RemotePort) != want.RemotePort {
				return true
			}
			if want.RemoteLogFormat != "" && rec.RemoteLogFormat != want.RemoteLogFormat {
				return true
			}
		case ingest.LoggingTypeRule:
			rules++
			if len(want.Topics) > 0 && !sameTopics(rec.Topics, want.Topics) {
				return true
			}
		}
	}
	return actions == 0 || rules == 0
}

// sameTopics compares two topic sets as sets: RouterOS prints them in
// its own order, and "info,firewall" is the same rule as
// "firewall,info". Duplicates are not a case RouterOS produces, so a
// count plus a membership check is the whole comparison.
func sameTopics(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
