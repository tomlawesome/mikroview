package detect

import "sort"

// maxEvidencePorts/maxEvidenceHosts cap how many entries flags.Evidence
// ever carries -- a port scan or a distributed brute-force can
// legitimately involve far more than is useful to display; the flag's
// own Detail string already states the true total, this is a bounded
// illustrative sample, not a complete dump.
const maxEvidencePorts = 50
const maxEvidenceHosts = 20

// sortedPortsCapped returns m's keys sorted ascending, capped at
// maxEvidencePorts. Sorted (unlike the reputation-sampling path's
// deliberate randomization) -- this is user-facing display data, it
// wants a stable, deterministic order, not a random sample.
func sortedPortsCapped(m map[int]struct{}) []int {
	out := make([]int, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	sort.Ints(out)
	if len(out) > maxEvidencePorts {
		out = out[:maxEvidencePorts]
	}
	return out
}

// sortedHostsCapped is sortedPortsCapped for a set of host IPs, capped
// at maxEvidenceHosts.
func sortedHostsCapped(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for h := range m {
		out = append(out, h)
	}
	sort.Strings(out)
	if len(out) > maxEvidenceHosts {
		out = out[:maxEvidenceHosts]
	}
	return out
}
