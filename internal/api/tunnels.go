// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
)

// wireguardHandshakeWindow is issue #874's rule for a WireGuard peer's
// own up/down state: WireGuard rehandshakes about every two minutes
// while traffic flows, so a last-handshake age under three minutes is
// the conventional reading that the tunnel is carrying traffic -- the
// number the owner ruling adopted, not a fresh judgement call made
// here.
const wireguardHandshakeWindow = 3 * time.Minute

// routerOSDurationPart matches one unit segment of a RouterOS
// time-elapsed string ("1m23s", "2h13m5s", "4w2d5h24m35s"). The
// prefix-conflicting units (ms, us, ns) are listed before the
// single-letter ones they would otherwise be swallowed by -- "m" is a
// prefix of "ms" -- because Go's regexp package documents leftmost-first
// alternative selection (the same choice a backtracking engine would
// make), so trying "ms" before "m" is what makes "50ms" parse as
// milliseconds rather than as "50m" followed by a stray "s" that then
// fails to parse.
var routerOSDurationPart = regexp.MustCompile(`^(\d+)(ns|us|ms|s|m|h|d|w)`)

// parseRouterOSDuration parses a RouterOS time-elapsed string --
// WireguardPeer.LastHandshake or PPPActiveSession.Uptime -- into a
// time.Duration. The standard library's time.ParseDuration refuses
// RouterOS's own day and week units ("d", "w"), which both fields reach
// past a few weeks of continuous operation, so this package cannot just
// call it. Malformed input, including "", is refused rather than
// guessed at: this data decides an up/down state shown to an operator,
// and a wrong guess there is worse than a state this build declines to
// compute at all.
func parseRouterOSDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("api: empty RouterOS duration")
	}
	var total time.Duration
	rest := s
	for rest != "" {
		m := routerOSDurationPart.FindStringSubmatch(rest)
		if m == nil {
			return 0, fmt.Errorf("api: unparseable RouterOS duration %q", s)
		}
		n, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("api: RouterOS duration %q: %w", s, err)
		}
		var unit time.Duration
		switch m[2] {
		case "ns":
			unit = time.Nanosecond
		case "us":
			unit = time.Microsecond
		case "ms":
			unit = time.Millisecond
		case "s":
			unit = time.Second
		case "m":
			unit = time.Minute
		case "h":
			unit = time.Hour
		case "d":
			unit = 24 * time.Hour
		case "w":
			unit = 7 * 24 * time.Hour
		}
		total += time.Duration(n) * unit
		rest = rest[len(m[0]):]
	}
	return total, nil
}

// wireguardPeerView is one pushed wireguard-peer record plus its
// derived state (issue #874). Embeds the record so every pushed field
// rides along unchanged; State and Since are the only additions.
type wireguardPeerView struct {
	ingest.WireguardPeer
	// State is "up" when LastHandshake parses to under
	// wireguardHandshakeWindow, "down" otherwise -- including when
	// LastHandshake is absent or this build cannot parse it, since a
	// peer this data cannot show as recently handshaken is not shown as
	// up. Computed once from the reported value itself: LastHandshake
	// is already "time elapsed as of push time" (#874), so no
	// additional clock reference is needed to classify it.
	State string `json:"state"`
	// Since is the last handshake's estimated absolute time -- this
	// table's push time minus LastHandshake -- present only when
	// LastHandshake parsed. A peer with no parseable LastHandshake
	// carries no Since: "since when" is not a question this data
	// answers for it.
	Since *time.Time `json:"since,omitempty"`
}

// wireguardInterfaceView is one pushed wireguard-interface record plus
// its derived state and the device's peers.
//
// Peers are attributed to a specific interface by ingest.WireguardPeer's
// Interface field when it is present -- a follow-up to #874's original
// cut, which shipped without it because RouterOS's peer record carries
// it as its own "interface" property, and only listing every peer under
// every interface once that field is populated does. A push script old
// enough to predate it (or a #874-shaped push that itself predates this
// follow-up) leaves every peer's Interface empty; when that is true of
// every peer for the device, this falls back to the original
// every-peer-under-every-interface reading rather than attributing zero
// peers to every interface, which would be strictly less honest than
// the old behaviour. An interface's State is "up" if any peer attached
// to it is.
type wireguardInterfaceView struct {
	ingest.WireguardInterface
	// State is "unknown" when the wireguard-peer kind has never been
	// pushed for this device at all -- this interface's own up/down
	// cannot be computed from anything mikroview has seen, and issue
	// #874 is explicit that a kind never pushed yields unknown, never a
	// guess. Otherwise "up" when any pushed peer is up, "down"
	// otherwise.
	State string              `json:"state"`
	Peers []wireguardPeerView `json:"peers"`
}

// wireguardTunnelsResponse is GET /api/routeros/{device}/wireguard's
// body. Available/UpdatedAt describe the wireguard-interface table
// (the "no data pushed yet" convention routerTableResponse already
// uses); PeersAvailable/PeersUpdatedAt describe the independent
// wireguard-peer table, since a device can push one kind without the
// other and a caller needs to tell "no interfaces named" apart from
// "interfaces named, but handshake state unknown".
type wireguardTunnelsResponse struct {
	Available      bool                     `json:"available"`
	UpdatedAt      *time.Time               `json:"updatedAt,omitempty"`
	PeersAvailable bool                     `json:"peersAvailable"`
	PeersUpdatedAt *time.Time               `json:"peersUpdatedAt,omitempty"`
	Interfaces     []wireguardInterfaceView `json:"interfaces"`
}

// handleRouterOSWireguard serves device's pushed WireGuard tables with
// per-tunnel state derived (issue #874, City 9's ingest side): a peer
// is up when it last handshook under three minutes ago at push time, an
// interface is up if any of its peers is, and "never pushed" reports
// unknown rather than a guessed-at down. Reads only mikroview's own
// RouterState -- nothing contacts the router on request, the same
// reasoning handleRouterOSRules gives.
func (s *Server) handleRouterOSWireguard(w http.ResponseWriter, r *http.Request) {
	device := r.PathValue("device")
	interfaces, ifUpdatedAt, ifOK := s.RouterState.WireguardInterfaces(device)
	peers, peersUpdatedAt, peersOK := s.RouterState.WireguardPeers(device)

	peerViews := make([]wireguardPeerView, 0, len(peers))
	// attributed is true the moment any peer names its interface --
	// which this build treats as "the whole device's peers carry
	// attribution", since a push script either reports the property for
	// every peer or (predating it) for none. See wireguardInterfaceView's
	// doc comment for what happens when it is false.
	attributed := false
	for _, p := range peers {
		v := wireguardPeerView{WireguardPeer: p, State: "down"}
		if p.LastHandshake != "" {
			if d, err := parseRouterOSDuration(p.LastHandshake); err == nil {
				if d < wireguardHandshakeWindow {
					v.State = "up"
				}
				since := peersUpdatedAt.Add(-d)
				v.Since = &since
			}
		}
		if p.Interface != "" {
			attributed = true
		}
		peerViews = append(peerViews, v)
	}

	ifaceViews := make([]wireguardInterfaceView, 0, len(interfaces))
	for _, iface := range interfaces {
		var attached []wireguardPeerView
		if attributed {
			attached = make([]wireguardPeerView, 0, len(peerViews))
			for _, v := range peerViews {
				if v.Interface == iface.Name {
					attached = append(attached, v)
				}
			}
		} else {
			// No peer named its interface (or there are no peers at
			// all) -- attribution is unavailable, so every peer is
			// attached to every interface rather than none, the
			// pre-attribution reading #874 originally shipped.
			attached = peerViews
		}

		anyUp := false
		for _, v := range attached {
			if v.State == "up" {
				anyUp = true
				break
			}
		}

		state := "unknown"
		if peersOK {
			if anyUp {
				state = "up"
			} else {
				state = "down"
			}
		}
		ifaceViews = append(ifaceViews, wireguardInterfaceView{
			WireguardInterface: iface,
			State:              state,
			Peers:              attached,
		})
	}

	resp := wireguardTunnelsResponse{
		Available:      ifOK,
		PeersAvailable: peersOK,
		Interfaces:     ifaceViews,
	}
	if ifOK {
		resp.UpdatedAt = &ifUpdatedAt
	}
	if peersOK {
		resp.PeersUpdatedAt = &peersUpdatedAt
	}
	writeJSON(w, http.StatusOK, resp)
}

// pppSessionView is one pushed ppp-active record plus its derived state
// (issue #874).
type pppSessionView struct {
	ingest.PPPActiveSession
	// State is always "up": a row exists in this table only while
	// RouterOS considers the session active ("a session present = up,
	// absent = down" -- #874). This build holds no separate roster of
	// configured PPP tunnels, so it can only assert "up" for sessions
	// currently present; a caller that already knows a tunnel's name
	// from elsewhere reads its absence from this list as down, per the
	// same rule.
	State string `json:"state"`
	// Since is this session's estimated start time -- this table's push
	// time minus Uptime -- present only when Uptime parsed.
	Since *time.Time `json:"since,omitempty"`
}

// pppActiveResponse is GET /api/routeros/{device}/ppp-active's body.
type pppActiveResponse struct {
	Available bool             `json:"available"`
	UpdatedAt *time.Time       `json:"updatedAt,omitempty"`
	Sessions  []pppSessionView `json:"sessions"`
}

// handleRouterOSPPPActive serves device's pushed /ppp/active table
// (issue #874) -- the state source for L2TP, PPTP, SSTP and OVPN
// tunnels alike, all surfaced through the same RouterOS menu.
func (s *Server) handleRouterOSPPPActive(w http.ResponseWriter, r *http.Request) {
	device := r.PathValue("device")
	sessions, updatedAt, ok := s.RouterState.PPPActive(device)

	views := make([]pppSessionView, 0, len(sessions))
	for _, sess := range sessions {
		v := pppSessionView{PPPActiveSession: sess, State: "up"}
		if sess.Uptime != "" {
			if d, err := parseRouterOSDuration(sess.Uptime); err == nil {
				since := updatedAt.Add(-d)
				v.Since = &since
			}
		}
		views = append(views, v)
	}

	resp := pppActiveResponse{Available: ok, Sessions: views}
	if ok {
		resp.UpdatedAt = &updatedAt
	}
	writeJSON(w, http.StatusOK, resp)
}
