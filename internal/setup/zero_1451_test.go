// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routeros"
)

// #1451: sameReport decides whether a repeated logging page needs persisting.

func baseEntry() ingest.LoggingEntry {
	return ingest.LoggingEntry{Type: "rule", Name: "n", Target: "remote", Remote: "192.0.2.5", RemoteProtocol: "tls",
		Action: "mikroview", Topics: []string{"firewall", "info"}}
}

func TestSameEntryComparesEveryFieldButNotIdentity(t *testing.T) {
	if !sameEntry(baseEntry(), baseEntry()) {
		t.Fatal("identical entries differ")
	}
	mutations := map[string]func(*ingest.LoggingEntry){
		"type":     func(e *ingest.LoggingEntry) { e.Type = "action" },
		"name":     func(e *ingest.LoggingEntry) { e.Name = "x" },
		"target":   func(e *ingest.LoggingEntry) { e.Target = "disk" },
		"remote":   func(e *ingest.LoggingEntry) { e.Remote = "192.0.2.6" },
		"protocol": func(e *ingest.LoggingEntry) { e.RemoteProtocol = "udp" },
		"format":   func(e *ingest.LoggingEntry) { e.RemoteLogFormat = "cef" },
		"action":   func(e *ingest.LoggingEntry) { e.Action = "memory" },
		"src":      func(e *ingest.LoggingEntry) { e.SrcAddress = "192.0.2.9" },
		"topics":   func(e *ingest.LoggingEntry) { e.Topics = []string{"firewall"} },
		"topic":    func(e *ingest.LoggingEntry) { e.Topics = []string{"firewall", "debug"} },
	}
	for name, mutate := range mutations {
		e := baseEntry()
		mutate(&e)
		if sameEntry(baseEntry(), e) {
			t.Errorf("a changed %s was not noticed", name)
		}
	}
}

func TestSameReportIgnoresWhenButNotWhat(t *testing.T) {
	a := LoggingReport{Device: "core", At: time.Now(), WizardVersion: 3, Records: []ingest.LoggingEntry{baseEntry()}}
	b := a
	b.At = a.At.Add(time.Hour)
	if !sameReport(a, b) {
		t.Error("a later timestamp alone made the reports differ")
	}
	b.WizardVersion = 4
	if sameReport(a, b) {
		t.Error("a different wizard version was not noticed")
	}
	b = a
	b.Records = nil
	if sameReport(a, b) {
		t.Error("a different record count was not noticed")
	}
	other := baseEntry()
	other.Name = "changed"
	b.Records = []ingest.LoggingEntry{other}
	if sameReport(a, b) {
		t.Error("a changed record was not noticed")
	}
}

// Sending the same page again refreshes when it was said without changing what it says.
func TestNoteLoggingReportTwiceKeepsTheContentAndMovesTheTime(t *testing.T) {
	s := New()
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	page := loggingPage(t, routeros.WizardVersion, "firewall,info")
	s.NoteLoggingReport("core", page, t0)
	s.NoteLoggingReport("core", page, t0.Add(time.Hour))
	reports := s.LoggingReports()
	if len(reports) != 1 || !reports[0].At.Equal(t0.Add(time.Hour)) || len(reports[0].Records) != 2 {
		t.Errorf("reports = %+v", reports)
	}
}
