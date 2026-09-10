// SPDX-License-Identifier: AGPL-3.0-only

package dossier

import (
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/oui"
)

// fp builds a fingerprint carrying the given signals with enough events
// behind it that the thin-evidence rule does not fire -- the table's
// own behaviour is what these tests are about, and the downgrade has
// its own test below.
func fp(signals ...Signal) fingerprint {
	f := fingerprint{signals: map[Signal]string{}, events: 500, span: time.Hour}
	for _, s := range signals {
		f.signals[s] = "observed " + string(s)
	}
	return f
}

func TestSuggestReadsTheHeuristicTable(t *testing.T) {
	tests := []struct {
		name       string
		fingerp    fingerprint
		mac        macFacts
		wantID     string
		wantConf   Confidence
		wantNoName bool
	}{
		{
			// The issue's own example: MQTT plus a clock check plus a
			// couple of fixed endpoints outside the LAN.
			name:     "MQTT, NTP and a couple of cloud endpoints read as IoT-ish",
			fingerp:  fp(SigMQTT, SigNTP, SigFewInternetPeers),
			wantID:   "iot",
			wantConf: Fair,
		},
		{
			name:     "SMB and RDP read as Windows-ish",
			fingerp:  fp(SigSMB, SigRDP),
			wantID:   "windows",
			wantConf: Strong,
		},
		{
			name:     "Kerberos beside SMB is the more specific domain reading",
			fingerp:  fp(SigSMB, SigKerberos, SigLDAP, SigRDP),
			wantID:   "windows-domain",
			wantConf: Strong,
		},
		{
			name:     "SMB with NFS and no RDP is storage, not a desktop",
			fingerp:  fp(SigSMB, SigNFS),
			wantID:   "nas",
			wantConf: Strong,
		},
		{
			name:     "raw printing is a printer",
			fingerp:  fp(SigPrintRaw, SigIPP),
			wantID:   "printer",
			wantConf: Strong,
		},
		{
			name:     "RTSP is a camera, and only at fair confidence",
			fingerp:  fp(SigRTSP),
			wantID:   "camera",
			wantConf: Fair,
		},
		{
			name:     "Modbus is a controller",
			fingerp:  fp(SigModbus),
			wantID:   "industrial",
			wantConf: Strong,
		},
		{
			name:     "SSH beside a served web port is a Unix-ish server",
			fingerp:  fp(SigSSH, SigWebServed),
			wantID:   "unix-server",
			wantConf: Fair,
		},
		{
			name:     "NTP and nothing else outside the LAN is an appliance-ish endpoint",
			fingerp:  fp(SigNTP, SigLANOnly),
			wantID:   "quiet-endpoint",
			wantConf: Weak,
		},
		{
			// The locally-administered bit on its own is a whole
			// finding: no vendor exists, so ask which host made it.
			name:    "a locally-administered MAC alone suggests a virtual interface",
			fingerp: fp(),
			mac: macFacts{parsed: true, mac: oui.MAC{
				Address: "02:42:AC:11:00:02", OUI: "0242AC", LocallyAdministered: true,
			}},
			wantID:   "virtual",
			wantConf: Fair,
		},
		{
			name:    "a vendor hint alone is only ever a weak reading",
			fingerp: fp(),
			mac: macFacts{
				parsed: true,
				mac:    oui.MAC{Address: "D4:8A:FC:00:00:01", OUI: "D48AFC"},
				vendor: oui.Vendor{Known: true, Name: "Espressif Inc."},
			},
			wantID:   "embedded",
			wantConf: Weak,
		},
		{
			name:       "nothing recognisable suggests nothing",
			fingerp:    fp(),
			wantNoName: true,
		},
		{
			name:       "a protocol with no profile suggests nothing but still shows what was seen",
			fingerp:    fp(SigTelnet),
			wantNoName: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := suggest(tc.fingerp, tc.mac)
			if tc.wantNoName {
				if got.Suggested {
					t.Fatalf("suggested %q, want no suggestion at all", got.Label)
				}
				if got.Note == "" {
					t.Error("a refusal to suggest must still say why")
				}
				return
			}
			if !got.Suggested {
				t.Fatalf("no suggestion made, want %s (note: %s)", tc.wantID, got.Note)
			}
			if got.Profile != tc.wantID {
				t.Errorf("profile = %q, want %q", got.Profile, tc.wantID)
			}
			if got.Confidence != tc.wantConf {
				t.Errorf("confidence = %q, want %q", got.Confidence, tc.wantConf)
			}
			// The rule the whole feature rests on: a suggestion always
			// carries its evidence.
			if len(got.Evidence) == 0 {
				t.Error("a suggestion was made with no evidence behind it")
			}
			for _, e := range got.Evidence {
				if e.Signal == "" || e.Detail == "" {
					t.Errorf("evidence line %+v is missing its signal or its detail", e)
				}
			}
		})
	}
}

func TestThinEvidenceLowersConfidenceAndSaysSo(t *testing.T) {
	thin := fingerprint{
		signals: map[Signal]string{SigSMB: "reached by 1 host on port 445", SigRDP: "reached by 1 host on port 3389"},
		events:  3,
		span:    30 * time.Second,
	}
	got := suggest(thin, macFacts{})
	if !got.Suggested {
		t.Fatal("SMB+RDP should still suggest something on thin evidence")
	}
	if got.Confidence != Fair {
		t.Errorf("confidence = %q, want the Strong ceiling lowered to fair", got.Confidence)
	}
	if !strings.Contains(got.Note, "lowered") {
		t.Errorf("note = %q, want it to state the downgrade", got.Note)
	}
}

func TestSuggestionNamesTheReadingsItBeat(t *testing.T) {
	got := suggest(fp(SigSMB, SigRDP, SigKerberos), macFacts{})
	if got.Profile != "windows-domain" {
		t.Fatalf("profile = %q, want the most specific match", got.Profile)
	}
	if len(got.Alternatives) == 0 {
		t.Error("a suggestion that beat other readings must name them")
	}
}

func TestSignalForPortRespectsDirection(t *testing.T) {
	// Reaching 1883 is speaking MQTT to a broker; the table only reads
	// that direction as the IoT signal on the outbound side, and treats
	// being reached on 445 and reaching 445 alike because both say the
	// host is in a Windows file-sharing world.
	if s, ok := signalForPort(1883, "tcp", "out"); !ok || s != SigMQTT {
		t.Errorf("outbound 1883 = (%q, %v), want mqtt", s, ok)
	}
	if s, ok := signalForPort(123, "udp", "in"); ok {
		t.Errorf("inbound 123 = %q, want no signal -- serving NTP is not the same fact as checking the time", s)
	}
	if s, ok := signalForPort(445, "tcp", "in"); !ok || s != SigSMB {
		t.Errorf("inbound 445 = (%q, %v), want smb", s, ok)
	}
	if _, ok := signalForPort(64999, "tcp", "out"); ok {
		t.Error("an unknown port produced a signal")
	}
}

// TestProfileTableIsWellFormed guards the table itself: every row needs
// an id, a label, a reason an operator can read, and at least one
// signal that can match it. A row with no All and no Any would match
// every host.
func TestProfileTableIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range profiles {
		if p.ID == "" || p.Label == "" {
			t.Errorf("profile %+v is missing its id or label", p)
		}
		if seen[p.ID] {
			t.Errorf("profile id %q appears twice", p.ID)
		}
		seen[p.ID] = true
		if p.Because == "" {
			t.Errorf("profile %q has no reason written for the operator", p.ID)
		}
		if len(p.All) == 0 && len(p.Any) == 0 {
			t.Errorf("profile %q would match every host", p.ID)
		}
		switch p.Ceiling {
		case Weak, Fair, Strong:
		default:
			t.Errorf("profile %q has confidence %q, which is not one of the three words", p.ID, p.Ceiling)
		}
	}
}

func TestSuggestedProbeIsPrintedNeverRun(t *testing.T) {
	f := fingerprint{
		signals:     map[Signal]string{},
		servedPorts: []int{80, 445, 3389},
		webPorts:    []int{80},
	}
	p := suggestProbe("10.0.10.5", f)
	if p == nil {
		t.Fatal("no probe suggested")
	}
	if p.Command != "nmap -Pn -sV -p 80,445,3389 10.0.10.5" {
		t.Errorf("command = %q, want a scan of the ports actually seen answering", p.Command)
	}
	if p.URL != "http://10.0.10.5/" {
		t.Errorf("url = %q, want the host's own web interface", p.URL)
	}
	if !strings.Contains(p.Note, "never connects") {
		t.Errorf("note = %q, want it to state that mikroview does not run this", p.Note)
	}

	// Nothing observed: a bounded fallback, not a full sweep.
	if p := suggestProbe("10.0.10.9", fingerprint{signals: map[Signal]string{}}); p == nil || !strings.Contains(p.Command, "--top-ports") {
		t.Errorf("fallback probe = %+v, want a bounded top-ports scan", p)
	}

	// Never build a command around something that is not an address.
	if p := suggestProbe("10.0.10.5; rm -rf /", fingerprint{}); p != nil {
		t.Errorf("probe built for a non-address: %+v", p)
	}
}
