// SPDX-License-Identifier: AGPL-3.0-only

package oui

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

const (
	fetchTimeout = 60 * time.Second
	// maxFetchBytes is generous headroom over the ~4MB the MA-L listing
	// weighs today, bounding what a hostile or broken source can make
	// this process allocate.
	maxFetchBytes = 32 << 20
	userAgent     = "mikroview-oui/1 (+https://github.com/tomlawesome/mikroview)"
)

// fetchClient is internal/netclass's fetch client, kept as this
// package's own copy for the reason that package's neighbours already
// document: the dial guard is small, and a shared one would put an
// SSRF-relevant predicate behind a dependency edge where a change for
// one feed silently changes another. The guard lives in Dialer.Control,
// which runs after DNS resolution and immediately before connect, so it
// sees the address actually being dialled -- no TOCTOU window, and DNS
// rebinding cannot slip past it the way an up-front hostname check
// allows.
//
// The URL is a constant (see SourceURL), so the guard is not defending
// against operator input here -- it is defending against DNS: whoever
// answers for standards-oui.ieee.org on the operator's network could
// otherwise point this fetch at 169.254.169.254 and read the result out
// of a log line.
type fetchClient struct {
	http *http.Client
}

func newFetchClient() *fetchClient {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: guardDial,
	}
	return &fetchClient{
		http: &http.Client{
			Timeout: fetchTimeout,
			Transport: &http.Transport{
				DialContext: dialer.DialContext,
				// Left to negotiate gzip itself: Go's transport then
				// decompresses transparently, so the io.LimitReader
				// below still bounds decompressed bytes. Setting
				// Accept-Encoding by hand disables that and
				// reintroduces the decompression-bomb footgun.
				ForceAttemptHTTP2: true,
			},
		},
	}
}

// guardDial refuses any connection to an address that is not a normal
// public unicast host.
func guardDial(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("oui: refusing to dial unparseable address %q", host)
	}
	addr = addr.Unmap()
	if !isPublicUnicast(addr) {
		return fmt.Errorf("oui: refusing to dial non-public address %s (SSRF guard)", addr)
	}
	return nil
}

// isPublicUnicast is stricter than any single net/netip predicate:
// Go's built-ins miss CGNAT (100.64.0.0/10), 192.0.0.0/24,
// 198.18.0.0/15 and the non-.0 parts of 0.0.0.0/8, so the reserved list
// backstops them. Same list internal/netclass carries, same reason.
func isPublicUnicast(addr netip.Addr) bool {
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() ||
		addr.IsMulticast() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsPrivate() || addr.IsInterfaceLocalMulticast() {
		return false
	}
	list := reservedV4
	if addr.Is6() {
		list = reservedV6
	}
	host := netip.PrefixFrom(addr, addr.BitLen())
	for _, r := range list {
		if r.Overlaps(host) {
			return false
		}
	}
	return true
}

var reservedV4 = mustPrefixes(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10", // CGNAT (RFC6598) -- the one Go misses that matters most
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
)

var reservedV6 = mustPrefixes(
	"::1/128",
	"::/128",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
	"2001:db8::/32",
)

func mustPrefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			panic("oui: bad reserved prefix " + c)
		}
		out = append(out, p)
	}
	return out
}

// fetch downloads the registry with a conditional GET keyed on the
// stored ETag. It returns (body, notModified, err): on a 304 the body
// is nil and notModified is true, so the daily poll of an unchanged 4MB
// file costs a few hundred bytes. IEEE serves both ETag and
// Last-Modified on this file, checked 2026-09-09.
func (fc *fetchClient) fetch(ctx context.Context, r *Registry) ([]byte, string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return nil, "", false, err
	}
	req.Header.Set("User-Agent", userAgent)

	r.mu.RLock()
	prevETag := r.etag
	r.mu.RUnlock()
	if prevETag != "" {
		req.Header.Set("If-None-Match", prevETag)
	}

	resp, err := fc.http.Do(req)
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, prevETag, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return nil, "", false, err
	}
	return body, resp.Header.Get("ETag"), false, nil
}
