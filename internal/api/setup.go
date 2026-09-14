// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
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
	// Witnesses is #1221's floor under evidence that lives only in
	// memory: a step mikroview watched satisfied at least once, with the
	// receipt it showed at the time. Nobody decided these -- see
	// setupWitness -- so they are served separately from Marks rather
	// than folded in as a third mark outcome the frontend would have to
	// tell apart from an operator's own.
	Witnesses []setupWitness `json:"witnesses"`
}

// setupWitness is one step the server itself watched happen (#1221),
// read back from internal/setup's NoteWitnessed. It exists so a step
// whose live evidence does not survive a restart -- because the store
// behind it is memory-only -- still reads as done, with a receipt that
// says plainly it is a memory and not a current reading. See
// SetupWizard.svelte's witnessReceipt for how the frontend words that.
type setupWitness struct {
	Step int `json:"step"`
	// Receipt is the fact observed at the moment this step was first
	// seen satisfied, worded the same way the live receipts elsewhere in
	// this file are -- the frontend only has to add the "seen on ..."
	// half to make it honestly past tense.
	Receipt string    `json:"receipt"`
	At      time.Time `json:"at"`
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
	// Address is the operator's own stored answer (#1213) to "what
	// address can your router reach mikroview on?" -- empty until they
	// have answered once. Persisted in internal/setup beside the marks,
	// so a restart mid-wizard does not lose it. Every RouterOS command
	// the wizard renders is written against this value once it is set,
	// never against the browser's own host.
	Address string `json:"address"`
	// AddressCandidates are this instance's own guesses at that answer,
	// from SetupInstance.Candidates (set at startup -- see that field's
	// comment for how it is derived and why it never polls a router).
	// The wizard offers these, and the browser's own host, as starting
	// points for the field above -- never as a value sent on the
	// operator's behalf.
	AddressCandidates []string `json:"addressCandidates"`
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
	witnesses := []setupWitness{}
	if s.Setup != nil {
		// The moment this handler can see a step is satisfied is the
		// moment #1221 asks it to remember that -- so the ledger still
		// has something to say about it once whatever made it true (a
		// map in internal/setup, a device's event count, a pushed-table
		// time) is gone after a restart. NoteWitnessed is a no-op once a
		// step already has one, so calling it on every poll costs
		// nothing beyond the check itself.
		//
		// Steps 5 and 6 are left out on purpose: step 5 is config-file
		// work with no evidence to witness ("quiet" in the frontend's own
		// words), and step 6's evidence is GET /api/router-backups, a
		// different endpoint this handler does not read -- witnessing it
		// belongs there if #1221's gap is ever seen on that step too.
		now := time.Now()
		if receipt, ok := caSatisfied(sources); ok {
			s.Setup.NoteWitnessed(1, receipt, now)
		}
		if receipt, ok := syslogSatisfied(sources); ok {
			s.Setup.NoteWitnessed(2, receipt, now)
		}
		if receipt, ok := rulesSatisfied(devices); ok {
			s.Setup.NoteWitnessed(3, receipt, now)
		}
		if receipt, ok := pushSatisfied(devices); ok {
			s.Setup.NoteWitnessed(4, receipt, now)
		}

		marks = s.Setup.Marks()
		for _, m := range s.Setup.Witnessed() {
			witnesses = append(witnesses, setupWitness{Step: m.Step, Receipt: m.Note, At: m.At})
		}
	}

	address := ""
	if s.Setup != nil {
		address = s.Setup.Address()
	}
	writeJSON(w, http.StatusOK, setupStatus{
		Instance: setupInstance{
			TLSEnabled:        s.SetupInstance.TLSEnabled,
			Hosts:             nonNilStrings(s.SetupInstance.Hosts),
			SyslogPort:        s.SetupInstance.SyslogPort,
			SyslogEnabled:     s.SetupInstance.SyslogPort != "",
			Address:           address,
			AddressCandidates: nonNilStrings(s.SetupInstance.Candidates),
		},
		Sources:   sources,
		Devices:   devices,
		PushKinds: ingestKindNames,
		Marks:     marks,
		Witnesses: witnesses,
	})
}

// caSatisfied and its three siblings below decide, from the same data
// this handler just built, whether a step now has live evidence -- just
// enough of setupsteps.ts's "arrived" reading (done or partial both
// count) to know when to witness it, not a second copy of how the step
// is worded or coloured, which stays the frontend's job.
func caSatisfied(sources []setup.SourceObservation) (string, bool) {
	for _, src := range sources {
		if src.CAFetchedAt != nil {
			return fmt.Sprintf("ca.crt fetched by %s", src.Source), true
		}
	}
	return "", false
}

func syslogSatisfied(sources []setup.SourceObservation) (string, bool) {
	for _, src := range sources {
		if src.SyslogFirstSeenAt != nil {
			return fmt.Sprintf("syslog connected from %s", src.Source), true
		}
	}
	return "", false
}

// rulesSatisfied counts every device with at least one event, the same
// "any events at all" test rulesStep uses to leave 'waiting' -- a
// partial decode rate still counts, matching arrived()'s reading of
// 'partial' as evidence.
func rulesSatisfied(devices []setupDevice) (string, bool) {
	var total, decoded uint64
	var any bool
	for _, d := range devices {
		if d.Events == 0 {
			continue
		}
		any = true
		total += d.Events
		decoded += d.DecodedActions
	}
	if !any {
		return "", false
	}
	return fmt.Sprintf("%d of %d events carried a decoded action", decoded, total), true
}

// pushSatisfied is satisfied the moment any table has ever been pushed,
// the same "waiting until the first one" test pushStep uses -- a partial
// set still counts, matching arrived()'s reading of 'partial' as
// evidence.
func pushSatisfied(devices []setupDevice) (string, bool) {
	kinds := map[string]bool{}
	for _, d := range devices {
		for k := range d.PushedKinds {
			kinds[k] = true
		}
	}
	if len(kinds) == 0 {
		return "", false
	}
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sort.Strings(names)
	return fmt.Sprintf("arrived: %s", strings.Join(names, ", ")), true
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

// setupAddressRequest is the wizard header field's answer (#1213): what
// address a router can reach this instance on.
type setupAddressRequest struct {
	Address string `json:"address"`
}

// setupAddressResponse echoes the stored value back, the same shape
// GET /api/setup/status's Instance.Address already reports it in.
type setupAddressResponse struct {
	Address string `json:"address"`
}

// handleSetupAddress records the operator's answer to "what address can
// your router reach mikroview on?" -- persisted in internal/setup beside
// the marks, so it survives the restart an upgrade brings, and so it
// need not be re-asked on every visit (it is editable afterwards from
// the same field, for an instance that moves).
//
// Admin-only, the same gate as handleSetupMark just above: there is no
// read-only wizard, so a viewer has no business changing what every
// RouterOS command in it is written against.
//
// validSetupAddress (setupcommands.go, #1095) is reused rather than
// reimplemented here: it already rejects anything that is not a
// well-formed hostname or IP literal, optionally with a port, which is
// exactly what a value about to sit bare inside routeros.CaTrustCommands,
// SyslogCommands, PushScript and BackupScript needs -- a newline or
// space here would smuggle a second command into what the operator
// pastes.
func (s *Server) handleSetupAddress(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	var req setupAddressRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !validSetupAddress(req.Address) {
		http.Error(w, "address must be a hostname or IP address, optionally with :port", http.StatusBadRequest)
		return
	}
	if s.Setup == nil {
		http.Error(w, "setup observations are not available", http.StatusServiceUnavailable)
		return
	}
	if !s.Setup.SetAddress(req.Address) {
		http.Error(w, "address could not be stored", http.StatusBadRequest)
		return
	}
	s.Audit.Record(auditActor(r), "setup.address_set", req.Address, "")
	writeJSON(w, http.StatusOK, setupAddressResponse{Address: req.Address})
}

// SetupInstance is the running configuration the wizard needs to write
// commands that work. Set once at startup by main.go.
type SetupInstance struct {
	TLSEnabled bool
	Hosts      []string
	SyslogPort string
	// BackupPort is the router-backup SFTP drop box's own port (#394,
	// config's backup.listen) -- separate from SyslogPort, since it is
	// a different listener entirely. Empty when backup.enabled is
	// false: step 6 has no address to render a script for.
	BackupPort string
	// Candidates are this instance's own guesses at the address a router
	// could reach it on (#1213): every real address it is bound to, on
	// the configured HTTPS port. Set once at startup by
	// main.setupAddressCandidates -- see that function's comment for how
	// it is derived (net.InterfaceAddrs plus the configured listen
	// address) and why it stops there rather than inventing a richer
	// discovery mechanism. May be empty on a host with nothing to
	// enumerate; the wizard still has the browser's own host to fall
	// back to.
	Candidates []string
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
	string(ingest.KindIPAddress),
}
