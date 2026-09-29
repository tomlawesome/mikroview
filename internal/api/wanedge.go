// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// GET /api/doors/internet answers issue #1319: what the WAN edge lets
// in, and where each door leads. Two honest marks, kept apart the same
// way #1018's port doors and traffic already are -- "allowed through"
// is a pushed rule naming the door, whatever traffic did; "seen
// arriving" is traffic MikroView's own event buffer actually logged.
// Neither is evaluated against the other: a door with no traffic is
// still a door, and this endpoint never judges rule order or
// precedence.
//
// wanDoorHost is who a door leads to. nil means the door ends at the
// router itself -- an input-chain accept, or one of the pushed /ip
// service rows. Set means a dst-nat rule forwards through to a host on
// the operator's own network.
type wanDoorHost struct {
	IP   string `json:"ip"`
	Name string `json:"name,omitempty"`
}

// wanDoor is one enabled rule that lets something through the WAN edge
// -- either group the note describes: an input-chain accept (To nil)
// or a dst-nat rule (To set). Never a policy judgement past "a rule
// names it": see doorsFor's own comment on the same reasoning.
type wanDoor struct {
	// Label is the rule's own ordinal, exactly as doorsFor's Label is:
	// "#12", what an operator would call it in RouterOS.
	Label   string       `json:"label"`
	Ordinal int          `json:"ordinal"`
	DstPort string       `json:"dstPort"`
	Proto   string       `json:"proto,omitempty"`
	To      *wanDoorHost `json:"to,omitempty"`
	Comment string       `json:"comment,omitempty"`
	// LastSeen is nil when nothing arrived at this door in the held
	// window -- absence, not a zero time, so a caller cannot mistake
	// "never" for the Unix epoch.
	LastSeen *time.Time `json:"lastSeen,omitempty"`
}

// wanService is one enabled /ip/service row (issue #1329), shown beside
// the input-chain doors under "to the router itself". Disabled services
// are never rows -- the same reading doorsFor gives a disabled filter
// rule.
type wanService struct {
	Name string `json:"name"`
	Port int    `json:"port"`
	// Address is the service's own allow-list, verbatim; "" means no
	// restriction -- RouterOS's own reading of an empty address
	// property, not this endpoint's invention.
	Address string `json:"address"`
}

// wanDeviceDoors is one device's whole section of the panel.
type wanDeviceDoors struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	WAN  string `json:"wan"`
	// Doors is every input-chain accept and dst-nat door together, in
	// each source table's own rule order -- the frontend splits them
	// into the two groups the note draws by whether To is set.
	Doors []wanDoor `json:"doors"`
	// Services is null when the router has never pushed /ip/service
	// (issue #1329) -- distinct from an empty, pushed table, which is a
	// real answer that the router runs none. Never invented from a
	// filter rule.
	Services      []wanService `json:"services"`
	PublicAddress string       `json:"publicAddress,omitempty"`
}

type wanDoorsResponse struct {
	Devices []wanDeviceDoors `json:"devices"`
}

// handleWANDoors serves GET /api/doors/internet (issue #1319).
//
// device and wan are both required query parameters. RouterState has no
// notion of which of a device's interfaces faces the internet --
// zones.svelte.ts's deviceWans derives that client-side, from which
// interface the estate's own events show carrying public-source
// traffic -- so the caller supplies it, the same shape the frontend
// already has in hand from deviceWans.
//
// Viewer tier, off readOnlyRoutes: same asymmetry as GET /api/ports
// beside it -- this is the operator's own address space with which
// hosts a WAN door leads to, which no bearer token has ever been able
// to read.
func (s *Server) handleWANDoors(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	device := strings.TrimSpace(qs.Get("device"))
	wan := strings.TrimSpace(qs.Get("wan"))
	if device == "" {
		badQueryParam(w, "device", "a device id")
		return
	}
	if wan == "" {
		badQueryParam(w, "wan", "the device's WAN interface name")
		return
	}
	writeJSON(w, http.StatusOK, wanDoorsResponse{Devices: []wanDeviceDoors{s.wanDoorsForDevice(device, wan)}})
}

// wanDoorsForDevice assembles one device's section: its own pushed
// filter and NAT tables read the same enabled-rule way doorsFor does,
// its pushed /ip/service table (or its absence), and the WAN
// interface's own pushed address for the printed nmap line.
func (s *Server) wanDoorsForDevice(device, wan string) wanDeviceDoors {
	out := wanDeviceDoors{ID: device, Name: s.Naming.Device(device), WAN: wan, Doors: []wanDoor{}}
	if s.RouterState == nil {
		return out
	}

	// Group 1a: input-chain accepts on the WAN interface. An unset
	// in-interface reads as "any" -- the same convention doorWho and
	// the forward-chain policy edges already give an unset interface
	// condition -- which includes the WAN -- but only once two other
	// conditions are ruled out, either of which would make "from the
	// internet" a claim this endpoint cannot back:
	//
	//   - a non-empty ConnectionState: `accept connection-state=
	//     established,related` answers only-already-open connections,
	//     never a fresh one arriving from the internet, and a real
	//     router's input chain is full of exactly this rule.
	//   - a non-empty SrcAddress/SrcAddressList: the rule restricts who
	//     may use it, and this endpoint has no way to tell whether that
	//     restriction names the internet or the operator's own LAN
	//     (`src-address-list=LAN` is the worked example) -- and the
	//     panel's own fixed caveat already says the list here may be
	//     incomplete, never that it may be wrong.
	//
	// A rule with no dst-port at all is still a door -- DstPort stays ""
	// and the frontend renders "any port" rather than a blank.
	if rules, _, ok := s.RouterState.FilterRules(device); ok {
		for _, rule := range rules {
			if rule.Disabled {
				continue
			}
			if !strings.EqualFold(rule.Chain, "input") || !strings.EqualFold(rule.Action, "accept") {
				continue
			}
			if len(rule.ConnectionState) > 0 {
				continue
			}
			if rule.SrcAddress != "" || rule.SrcAddressList != "" {
				continue
			}
			if rule.InInterface != "" && rule.InInterface != wan {
				continue
			}
			out.Doors = append(out.Doors, wanDoor{
				Label:   "#" + strconv.Itoa(int(rule.Ordinal)),
				Ordinal: int(rule.Ordinal),
				DstPort: string(rule.DstPort),
				Proto:   strings.ToLower(strings.TrimSpace(rule.Protocol)),
				Comment: rule.Comment,
			})
		}
	}

	// Group 2: enabled dst-nat rules forwarding through to a host. The
	// same sender-restriction exclusion as group 1a above; NATRule
	// carries no ConnectionState (dst-nat's own chain, prerouting, never
	// answers established/related the way an input accept can).
	if nat, _, ok := s.RouterState.NATRules(device); ok {
		for _, rule := range nat {
			if rule.Disabled {
				continue
			}
			if !strings.EqualFold(rule.Chain, "dstnat") || !strings.EqualFold(rule.Action, "dst-nat") {
				continue
			}
			if rule.SrcAddress != "" {
				continue
			}
			if rule.InInterface != "" && rule.InInterface != wan {
				continue
			}
			out.Doors = append(out.Doors, wanDoor{
				Label:   "#" + strconv.Itoa(int(rule.Ordinal)),
				Ordinal: int(rule.Ordinal),
				DstPort: string(rule.DstPort),
				Proto:   strings.ToLower(strings.TrimSpace(rule.Protocol)),
				Comment: rule.Comment,
				To:      &wanDoorHost{IP: rule.ToAddresses, Name: s.Naming.Host(device, rule.ToAddresses)},
			})
		}
	}

	// Group 1b: the router's own pushed services (#1329), when it pushes
	// them at all -- services stays nil (JSON null) otherwise, which the
	// panel reads as "this router does not push /ip service yet" rather
	// than "runs none".
	if services, _, ok := s.RouterState.IPServices(device); ok {
		out.Services = []wanService{}
		for _, svc := range services {
			if svc.Disabled {
				continue
			}
			out.Services = append(out.Services, wanService{
				Name:    svc.Name,
				Port:    int(svc.Port),
				Address: svc.Address.String(),
			})
		}
	}

	if addrs, _, ok := s.RouterState.IPAddresses(device); ok {
		for _, a := range addrs {
			if a.Interface == wan {
				addr, _, _ := strings.Cut(a.Address, "/")
				out.PublicAddress = addr
				break
			}
		}
	}

	if s.Store != nil && len(out.Doors) > 0 {
		want := make([]store.DoorPort, 0, len(out.Doors))
		for _, d := range out.Doors {
			for _, p := range doorPortsOf(d.DstPort) {
				want = append(want, store.DoorPort{Port: p, Proto: d.Proto})
			}
		}
		seen := s.Store.DoorLastSeen(device, wan, want)
		for i := range out.Doors {
			for _, p := range doorPortsOf(out.Doors[i].DstPort) {
				if t, ok := seen[store.DoorPort{Port: p, Proto: out.Doors[i].Proto}]; ok {
					tt := t
					out.Doors[i].LastSeen = &tt
					break
				}
			}
		}
	}

	return out
}

// doorPortsOf reads a rule's own dst-port spec into the concrete ports
// it names, the same "skip ranges, a rule table is bounded, traffic is
// not" reading namedPorts (ports.go) gives the same field: a range
// covers many ports and none of them is a single last-seen fact worth
// asking about.
func doorPortsOf(spec string) []int {
	var out []int
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" || strings.Contains(part, "-") {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil && n >= 1 && n <= 65535 {
			out = append(out, n)
		}
	}
	return out
}
