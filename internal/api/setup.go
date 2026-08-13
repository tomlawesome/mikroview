// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
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

// handleSetupStatus serves the wizard's view. Admin-only: it enumerates
// every device and source mikroview knows about, which is the same map
// of the deployment GET /api/auth/users is admin-gated for.
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

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
	})
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
