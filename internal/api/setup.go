// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/setup"
)

// setupStatus is what the guided setup wizard (#320) reads to tell an
// operator whether each step has landed.
//
// Every field is an observation made on mikroview's own side -- a fetch,
// a connection, an event, a push. Nothing here polls a router, because
// mikroview never connects to one; that constraint is what the whole
// design works within, not a limitation to route around.
type setupStatus struct {
	// Instance is what the wizard needs to write correct commands: the
	// address to put in them and whether the certificate covers it.
	Instance setupInstance `json:"instance"`
	// Sources is per source address: who fetched the CA, who has a
	// syslog connection.
	Sources []setup.SourceObservation `json:"sources"`
	// Devices is per device: events seen, how many carried a decoded
	// action, and which pushed tables have arrived.
	Devices []setupDevice `json:"devices"`
	// PushKinds is every table the push script sends, in the order the
	// wizard shows them. Served rather than hard-coded in the frontend
	// so adding a kind to internal/ingest cannot silently leave the
	// wizard describing an incomplete script.
	PushKinds []string `json:"pushKinds"`
	// Marks is the other half of the claim ledger (#487): the steps the
	// operator skipped or forced past, each with who and when. Served
	// alongside the evidence because the surfaces that need it are not
	// only the wizard -- an empty stream explains its own silence with
	// the forced-past line that accounts for it.
	Marks []setup.Mark `json:"marks"`
}

type setupInstance struct {
	TLSEnabled bool `json:"tlsEnabled"`
	// Hosts is tls.hosts as configured. Empty means the generated
	// certificate covers localhost/127.0.0.1 only, which is the single
	// most common reason a router's first fetch fails.
	Hosts []string `json:"hosts"`
	// SyslogPort is the port routers should send syslog to, taken from
	// the running configuration rather than assumed to be 6514.
	SyslogPort string `json:"syslogPort"`
	// SyslogEnabled is false when listen.syslogTls is empty, in which
	// case no amount of router-side configuration will ever work and the
	// wizard should say so rather than wait.
	SyslogEnabled bool `json:"syslogEnabled"`
}

type setupDevice struct {
	Device string `json:"device"`
	// Configured reports whether this device is declared under devices:
	// in config.yaml. An undeclared one still works; it is identified by
	// its address rather than a name.
	Configured bool   `json:"configured"`
	SourceIP   string `json:"sourceIp"`
	Events     uint64 `json:"events"`
	// DecodedActions is how many of those events carried an action
	// decoded from a log-prefix. Zero, with events above zero, is the
	// "rules log but without the prefix convention" state -- which looks
	// healthy by every other measure.
	DecodedActions uint64 `json:"decodedActions"`
	// PushedKinds maps each pushed table to when it last arrived, so the
	// wizard can say which of the four blocks in the push script are
	// working rather than just "pushes are happening".
	PushedKinds map[string]time.Time `json:"pushedKinds,omitempty"`
}

// handleSetupStatus serves the wizard's view: every device and source
// mikroview knows about.
//
// Open to any signed-in user (#490): the settings page's viewer-readable
// widening reaches setup status too, same as the other three GETs it
// widens alongside. There is no corresponding write endpoint here to
// keep closed.
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	var sources []setup.SourceObservation
	prefixByDevice := map[string]setup.DeviceObservation{}
	if s.Setup != nil {
		var devObs []setup.DeviceObservation
		sources, devObs = s.Setup.Snapshot()
		for _, d := range devObs {
			prefixByDevice[d.Device] = d
		}
	}
	if sources == nil {
		sources = []setup.SourceObservation{}
	}

	devices := make([]setupDevice, 0)
	if s.Devices != nil {
		for _, info := range s.Devices.List() {
			d := setupDevice{
				Device:     info.ID,
				Configured: info.Configured,
				SourceIP:   info.SourceIP,
				Events:     uint64(info.EventCount),
			}
			if obs, ok := prefixByDevice[info.ID]; ok {
				d.DecodedActions = obs.Decoded
			}
			if s.RouterState != nil {
				if kinds := s.RouterState.PushedKinds(info.ID); len(kinds) > 0 {
					d.PushedKinds = make(map[string]time.Time, len(kinds))
					for k, at := range kinds {
						d.PushedKinds[string(k)] = at
					}
				}
			}
			devices = append(devices, d)
		}
	}

	marks := []setup.Mark{}
	if s.Setup != nil {
		marks = s.Setup.Marks()
	}

	writeJSON(w, http.StatusOK, setupStatus{
		Instance: setupInstance{
			TLSEnabled:    s.SetupInstance.TLSEnabled,
			Hosts:         nonNilStrings(s.SetupInstance.Hosts),
			SyslogPort:    s.SetupInstance.SyslogPort,
			SyslogEnabled: s.SetupInstance.SyslogPort != "",
		},
		Sources:   sources,
		Devices:   devices,
		PushKinds: ingestKindNames,
		Marks:     marks,
	})
}

// setupMarkRequest is one step decision from the wizard's footer.
//
// Deliberately no actor field: who did this is resolved from the session
// (auditActor), never from the body. A ledger a client can sign with
// somebody else's name is not a record of anything.
type setupMarkRequest struct {
	Step    int    `json:"step"`
	Outcome string `json:"outcome"`
	// Note is what had not arrived at the moment the decision was made,
	// as the wizard's own observation line worded it. It is what turns
	// "step 2 forced past" into a line that explains a silence.
	Note string `json:"note"`
}

// handleSetupMark records that a step was skipped or forced past.
//
// Admin-only, matching the wizard itself: #490 keeps "Run setup…" absent
// for viewers and there is no read-only wizard, so a viewer has no way
// to reach this and no business writing to the ledger.
//
// Two writes, deliberately, because they answer different questions.
// The mark goes to internal/setup, where it sits beside the evidence it
// qualifies and is read back by every surface that has a silence to
// explain; it is persisted there, so the explanation survives the
// restart an upgrade brings. The audit entry goes to internal/audit,
// which is where diagnostics look and where the line stays as history
// even after evidence arrives and the step turns green -- the design
// record's "forced is not failed". Neither derives from the other:
// internal/audit prunes to maxEntries, so marks read back out of the
// log would silently vanish once enough entries accumulated.
func (s *Server) handleSetupMark(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	var req setupMarkRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if s.Setup == nil {
		http.Error(w, "setup observations are not available", http.StatusServiceUnavailable)
		return
	}
	mark, ok := s.Setup.NoteMark(req.Step, setup.MarkOutcome(req.Outcome), auditActor(r), req.Note, time.Now())
	if !ok {
		http.Error(w, "step must be 1-5 and outcome one of skipped, forced", http.StatusBadRequest)
		return
	}
	// The audit vocabulary is owned by the caller (see internal/audit's
	// Entry.Action), and these two are worded so a reader scanning the
	// log sees the difference the design record insists on: skip is
	// quiet, force is loud.
	action := "setup.step_skipped"
	if mark.Outcome == setup.MarkForced {
		action = "setup.step_forced"
	}
	s.Audit.Record(mark.Actor, action, fmt.Sprintf("step %d", mark.Step), mark.Note)
	writeJSON(w, http.StatusOK, mark)
}

// SetupInstance is the running configuration the wizard needs to write
// commands that work. Set once at startup by main.go.
type SetupInstance struct {
	TLSEnabled bool
	Hosts      []string
	SyslogPort string
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// ingestKindNames is every table the push script can send, in the order
// the wizard presents them. Named here rather than in the frontend so
// adding a kind to internal/ingest cannot silently leave the wizard
// describing an incomplete script.
var ingestKindNames = []string{
	string(ingest.KindFilterRule),
	string(ingest.KindAddressList),
	string(ingest.KindDHCPLease),
	string(ingest.KindARP),
}
