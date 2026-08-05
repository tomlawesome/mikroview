// Package naming resolves user-configured friendly display names for
// firewall rule labels and host IPs (internal/config's RuleNames and
// HostNames) -- config-driven only, no auto-discovery or liveness
// tracking the way internal/device's Registry has, since a rule label or
// a host IP doesn't have a "connection" to observe the way a syslog
// source does; it's just a lookup.
package naming

// Resolver holds the configured name maps and resolves against them. The
// zero value is usable -- nil maps just miss every lookup -- so this
// works identically whether or not naming is configured at all.
type Resolver struct {
	Rules map[string]string
	Hosts map[string]string
}

// Rule returns the friendly name configured for a raw rule label, or ""
// if none is configured.
func (r Resolver) Rule(label string) string {
	return r.Rules[label]
}

// Host returns the friendly name configured for a host IP, or "" if none
// is configured.
func (r Resolver) Host(ip string) string {
	return r.Hosts[ip]
}
