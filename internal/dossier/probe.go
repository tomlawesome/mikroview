// SPDX-License-Identifier: AGPL-3.0-only

package dossier

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// probeNote is attached to every suggested probe. It is written for the
// operator rather than for a developer, because it is the sentence that
// has to stop somebody assuming mikroview ran the scan itself.
const probeNote = "mikroview does not run this and never connects to a host on your network -- it only reads what your routers send it. Copy the command if you want to look yourself."

// maxProbePorts bounds the port list in a suggested nmap command:
// enough to be worth running, short enough to read at a glance.
const maxProbePorts = 12

// suggestProbe prints the command an operator might run next, and never
// runs it (see the Probe type for why that line exists).
//
// It prefers the ports something has actually been seen reaching on
// this host, because a scan of what is known to answer is faster and
// far less noisy than a blind sweep. With nothing observed, it falls
// back to nmap's own top-ports scan rather than a full one, for the
// same reason.
//
// -Pn is not a detail: mikroview is suggesting this because the host is
// unidentified, and a host that ignores ping would otherwise be skipped
// before a single port was tried.
func suggestProbe(ip string, fp fingerprint) *Probe {
	if _, err := netip.ParseAddr(ip); err != nil {
		// Never build a shell command around something that is not an
		// address. A caller pasting the result would be running
		// whatever this string was instead.
		return nil
	}

	p := &Probe{Note: probeNote}
	if len(fp.servedPorts) > 0 {
		ports := fp.servedPorts
		if len(ports) > maxProbePorts {
			ports = ports[:maxProbePorts]
		}
		parts := make([]string, 0, len(ports))
		for _, port := range ports {
			parts = append(parts, strconv.Itoa(port))
		}
		p.Command = fmt.Sprintf("nmap -Pn -sV -p %s %s", strings.Join(parts, ","), ip)
	} else {
		p.Command = fmt.Sprintf("nmap -Pn -sV --top-ports 100 %s", ip)
	}

	// A web port that has been reached is the shortest path to an
	// answer: most appliances identify themselves on their own landing
	// page.
	for _, port := range fp.webPorts {
		switch port {
		case 443, 8443:
			p.URL = fmt.Sprintf("https://%s:%d/", ip, port)
		case 80:
			p.URL = fmt.Sprintf("http://%s/", ip)
		default:
			p.URL = fmt.Sprintf("http://%s:%d/", ip, port)
		}
		if port == 80 || port == 443 {
			// The standard ports are the likeliest to be the real
			// interface, so stop at one rather than letting a high
			// port overwrite it.
			break
		}
	}
	return p
}
