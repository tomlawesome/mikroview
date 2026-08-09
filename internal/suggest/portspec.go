// SPDX-License-Identifier: AGPL-3.0-only

package suggest

import (
	"strconv"
	"strings"
)

// maxPortsPerRule bounds how many individual ports one rule's dst-port
// expands into. A rule scoping a genuinely broad range (e.g. an
// operator's own "log everything above 1024" catch-all) is not what
// this feature means by "watch this port" -- it is closer to "watch
// almost everything", which would make the resulting watchlist entry
// indistinguishable from turning the feature off. Such a rule produces
// no candidate at all rather than a candidate covering a huge and
// mostly-meaningless port set.
const maxPortsPerRule = 32

// parsePortSpec expands RouterOS's own dst-port syntax -- a single port
// ("22"), a comma-separated list ("22,23,3389"), a range ("1000-2000"),
// or a mix of those ("22,1000-2000") -- into individual port numbers.
// Returns nil for an empty spec (a rule with no dst-port set, matching
// every port -- not something this feature can turn into a specific
// port suggestion) or one that would expand past maxPortsPerRule.
func parsePortSpec(spec string) []int {
	if spec == "" {
		return nil
	}
	var ports []int
	for _, segment := range strings.Split(spec, ",") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		if lo, hi, ok := strings.Cut(segment, "-"); ok {
			loN, err1 := strconv.Atoi(strings.TrimSpace(lo))
			hiN, err2 := strconv.Atoi(strings.TrimSpace(hi))
			if err1 != nil || err2 != nil || loN < 0 || hiN < loN || hiN > 65535 {
				return nil // malformed or out-of-range -- refuse the whole rule rather than guess
			}
			for p := loN; p <= hiN; p++ {
				ports = append(ports, p)
				if len(ports) > maxPortsPerRule {
					return nil
				}
			}
			continue
		}
		n, err := strconv.Atoi(segment)
		if err != nil || n < 0 || n > 65535 {
			return nil
		}
		ports = append(ports, n)
		if len(ports) > maxPortsPerRule {
			return nil
		}
	}
	return ports
}
