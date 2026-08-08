// SPDX-License-Identifier: AGPL-3.0-only

package netclass

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
	fetchTimeout  = 30 * time.Second
	maxFetchBytes = 32 << 20 // cloud range docs run to a few MB; this is headroom
	userAgent     = "mikroview-netclass/1 (+https://github.com/tomlawesome/mikroview)"
)

// fetchClient wraps an http.Client whose dialer refuses to connect to any
// non-public address. The guard lives in Dialer.Control, which runs after
// DNS resolution and immediately before connect -- so it sees the actual
// IP being dialled, with no TOCTOU window and immune to DNS rebinding. A
// hostname check up front cannot offer either property.
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
				// decompresses transparently, so the io.LimitReader below
				// already bounds decompressed bytes. Setting Accept-
				// Encoding by hand disables that and reintroduces the
				// decompression-bomb footgun.
				ForceAttemptHTTP2: true,
			},
		},
	}
}

// guardDial rejects a connection to any address that is not a normal
// public unicast host. Same-host redirects are allowed by the client
// (see fetch), but every hop's target IP still passes through here.
func guardDial(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("netclass: refusing to dial unparseable address %q", host)
	}
	addr = addr.Unmap()
	if !isPublicUnicast(addr) {
		return fmt.Errorf("netclass: refusing to dial non-public address %s (SSRF guard)", addr)
	}
	return nil
}

// isPublicUnicast is stricter than any single net/netip predicate. Go's
// built-ins miss CGNAT (100.64.0.0/10), 192.0.0.0/24, 198.18.0.0/15 and
// the non-.0 parts of 0.0.0.0/8 -- all verified as returning false from
// every Is* predicate -- so the reserved-prefix list backstops them.
func isPublicUnicast(addr netip.Addr) bool {
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() ||
		addr.IsMulticast() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsPrivate() || addr.IsInterfaceLocalMulticast() {
		return false
	}
	host := netip.PrefixFrom(addr, addr.BitLen())
	return !overlapsReserved(host)
}

// fetch downloads url with a conditional GET keyed on the source's stored
// ETag. It returns (body, notModified, err): on a 304 the body is nil and
// notModified is true, so a daily poll of an unchanged multi-MB feed
// costs a few hundred bytes.
func (fc *fetchClient) fetch(ctx context.Context, c *Classifier, src Source, url string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", userAgent)

	c.mu.RLock()
	prevETag := c.etag[src]
	c.mu.RUnlock()
	if prevETag != "" {
		req.Header.Set("If-None-Match", prevETag)
	}

	resp, err := fc.http.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return nil, false, err
	}

	if etag := resp.Header.Get("ETag"); etag != "" {
		c.mu.Lock()
		c.etag[src] = etag
		c.mu.Unlock()
	}
	return body, false, nil
}
