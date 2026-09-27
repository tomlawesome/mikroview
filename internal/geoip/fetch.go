// SPDX-License-Identifier: AGPL-3.0-only

package geoip

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"syscall"
	"time"
)

const (
	// fetchTimeout bounds a whole download. Far longer than the OUI
	// feed's minute: IPinfo Lite is tens of megabytes, and a slow home
	// uplink should get it in the end rather than fail every day.
	fetchTimeout = 10 * time.Minute
	// maxFileBytes caps both what is read off the wire and what one
	// decompressed database may grow to. Every source's file is well
	// under it today (DB-IP Lite and GeoLite2-Country ~10 MB, IPinfo
	// Lite tens of MB); it exists to bound a hostile or broken source.
	maxFileBytes = 128 << 20
	// maxTarEntries bounds how many headers a MaxMind archive may walk.
	// A real one has three (COPYRIGHT.txt, LICENSE.txt, the .mmdb).
	maxTarEntries = 64
	userAgent     = "mikroview-geoip/1 (+https://github.com/tomlawesome/mikroview)"
)

// The three download locations. Constants, not config: an operator
// enabling a feed is trusting MikroView's vetting of it, the same
// decision internal/oui.SourceURL documents. Only validated
// credentials are ever added to them (see keys.go), so no request URL
// is built from free text.
const (
	// dbipURLPattern takes the month, "2026-09". Checked 2026-09-26 on
	// https://db-ip.com/db/lite.php (#1352's licence table).
	dbipURLPattern = "https://download.db-ip.com/free/dbip-country-lite-%s.mmdb.gz"
	// ipinfoURL takes ?token=. The token rides in the URL, so every
	// string built from a request to it goes through redact.
	ipinfoURL = "https://ipinfo.io/data/ipinfo_lite.mmdb"
	// maxmindURL takes HTTP basic auth (account ID : licence key) and
	// answers with a redirect to a signed storage URL; Go's client
	// drops the Authorization header on that cross-host hop.
	maxmindURL = "https://download.maxmind.com/geoip/databases/GeoLite2-Country/download?suffix=tar.gz"
)

// endpoints is the three URLs, a field so tests can point them at an
// httptest server without opening a URL setting to the outside world.
type endpoints struct {
	dbip    func(month string) string
	ipinfo  string
	maxmind string
}

func defaultEndpoints() endpoints {
	return endpoints{
		dbip:    func(month string) string { return fmt.Sprintf(dbipURLPattern, month) },
		ipinfo:  ipinfoURL,
		maxmind: maxmindURL,
	}
}

// newFetchClient is internal/oui's SSRF-guarded client, kept as this
// package's own copy for the reason that package documents: a shared
// guard would put an SSRF-relevant predicate behind a dependency edge
// where a change for one feed silently changes another. The guard runs
// in Dialer.Control -- after DNS, immediately before connect -- so it
// sees the address actually dialled, including for every redirect hop.
func newFetchClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: guardDial,
	}
	return &http.Client{
		Timeout: fetchTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ForceAttemptHTTP2:     true,
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
		return fmt.Errorf("geoip: refusing to dial unparseable address %q", host)
	}
	addr = addr.Unmap()
	if !IsPublicUnicast(addr) {
		return fmt.Errorf("geoip: refusing to dial non-public address %s (SSRF guard)", addr)
	}
	return nil
}

// IsPublicUnicast is stricter than any single net/netip predicate: Go's
// built-ins miss CGNAT (100.64.0.0/10), 192.0.0.0/24, 198.18.0.0/15, the
// documentation ranges and the non-.0 parts of 0.0.0.0/8, so the
// reserved list backstops them. Same list internal/oui carries.
// Exported for GET /api/geo/lookup, which only looks up addresses this
// accepts.
func IsPublicUnicast(addr netip.Addr) bool {
	addr = addr.Unmap()
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
	"100.64.0.0/10",
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
			panic("geoip: bad reserved prefix " + c)
		}
		out = append(out, p)
	}
	return out
}

// archiveKind says how a response body wraps the MMDB file.
type archiveKind int

const (
	// maybeGzip: a bare .mmdb, or one gzip-wrapped -- told apart by the
	// gzip magic bytes, so a source that starts or stops compressing
	// keeps working.
	maybeGzip archiveKind = iota
	// tarGzip: MaxMind's .tar.gz holding exactly one .mmdb.
	tarGzip
)

// request is one download.
type request struct {
	url      string
	user     string // basic auth; empty for none
	pass     string
	etag     string // conditional: If-None-Match
	modified string // conditional: If-Modified-Since
	kind     archiveKind
	secrets  []string // redacted from every error string
}

// response is what a download produced.
type response struct {
	notModified  bool
	notFound     bool
	unauthorized bool
	etag         string
	lastModified string
}

// errStatus is an unexpected HTTP status. Carries only the code: a
// provider's error body can echo the request back, token included.
type errStatus int

func (e errStatus) Error() string { return fmt.Sprintf("unexpected status %d", int(e)) }

// download fetches req into dst (the extracted .mmdb bytes). On 304 or
// 404 nothing is written and the response says which.
func (m *Manager) download(ctx context.Context, req request, dst io.Writer) (response, error) {
	hr, err := http.NewRequestWithContext(ctx, http.MethodGet, req.url, nil)
	if err != nil {
		return response{}, errors.New(redact(err.Error(), req.secrets))
	}
	hr.Header.Set("User-Agent", userAgent)
	if req.user != "" {
		hr.SetBasicAuth(req.user, req.pass)
	}
	if req.etag != "" {
		hr.Header.Set("If-None-Match", req.etag)
	}
	if req.modified != "" {
		hr.Header.Set("If-Modified-Since", req.modified)
	}

	resp, err := m.client.Do(hr)
	if err != nil {
		return response{}, errors.New(cleanErr(err, req.secrets))
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return response{notModified: true, etag: req.etag, lastModified: req.modified}, nil
	case http.StatusNotFound:
		return response{notFound: true}, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return response{unauthorized: true}, errStatus(resp.StatusCode)
	case http.StatusOK:
	default:
		return response{}, errStatus(resp.StatusCode)
	}

	body := &capReader{r: resp.Body, left: maxFileBytes}
	switch req.kind {
	case tarGzip:
		err = extractTarGz(body, dst)
	default:
		err = extractMaybeGzip(body, dst)
	}
	if err != nil {
		return response{}, errors.New(redact(err.Error(), req.secrets))
	}
	return response{etag: resp.Header.Get("ETag"), lastModified: resp.Header.Get("Last-Modified")}, nil
}

// capReader fails, rather than silently truncating, once more than left
// bytes have been read -- a truncated MMDB that happened to open would
// be worse than a refused one.
type capReader struct {
	r    io.Reader
	left int64
}

var errTooLarge = fmt.Errorf("download exceeds the %d MiB cap", maxFileBytes>>20)

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		var one [1]byte
		if n, _ := c.r.Read(one[:]); n > 0 {
			return 0, errTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// copyCapped copies at most maxFileBytes from src, and fails if src has
// more. io.CopyN rather than io.Copy so a decompression bomb stops at
// the cap (gosec G110).
func copyCapped(dst io.Writer, src io.Reader) error {
	n, err := io.CopyN(dst, src, maxFileBytes+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if n > maxFileBytes {
		return errTooLarge
	}
	if n == 0 {
		return errors.New("empty download")
	}
	return nil
}

func extractMaybeGzip(body io.Reader, dst io.Writer) error {
	br := bufio.NewReader(body)
	magic, _ := br.Peek(2)
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return fmt.Errorf("gzip: %w", err)
		}
		defer zr.Close()
		return copyCapped(dst, zr)
	}
	return copyCapped(dst, br)
}

// extractTarGz copies the archive's single .mmdb member into dst. The
// member's name is never used as a path -- the file is written to this
// package's own fixed name -- but a name that is absolute or climbs out
// with ".." marks an archive nobody should trust, and is refused.
func extractTarGz(body io.Reader, dst io.Writer) error {
	zr, err := gzip.NewReader(body)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	found := false
	for i := 0; ; i++ {
		if i >= maxTarEntries {
			return fmt.Errorf("archive has more than %d entries", maxTarEntries)
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		name := hdr.Name
		if path.IsAbs(name) || strings.Contains(name, `\`) {
			return fmt.Errorf("archive member %q has an unsafe path", name)
		}
		for _, seg := range strings.Split(name, "/") {
			if seg == ".." {
				return fmt.Errorf("archive member %q has an unsafe path", name)
			}
		}
		if !strings.HasSuffix(strings.ToLower(name), ".mmdb") {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("archive member %q is not a regular file", name)
		}
		if found {
			return errors.New("archive holds more than one .mmdb file")
		}
		if hdr.Size > maxFileBytes {
			return errTooLarge
		}
		if err := copyCapped(dst, tr); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return errors.New("archive holds no .mmdb file")
	}
	return nil
}

// redactURL drops the query string, userinfo and fragment -- where a
// token or credential can sit.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[unparseable URL]"
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.User = nil
	u.Fragment = ""
	return u.String()
}

// cleanErr renders a client error without the request URL's query
// string. A *url.Error prints the full URL -- ipinfo's token included --
// so it is rebuilt around the redacted URL rather than printed as is.
func cleanErr(err error, secrets []string) string {
	msg := err.Error()
	var ue *url.Error
	if errors.As(err, &ue) {
		inner := "request failed"
		if ue.Err != nil {
			inner = ue.Err.Error()
		}
		msg = fmt.Sprintf("%s %s: %s", ue.Op, redactURL(ue.URL), inner)
	}
	return redact(msg, secrets)
}

// redact is the belt to cleanErr's braces: whatever path a message took,
// no credential survives into it, in plain or URL-escaped form.
func redact(msg string, secrets []string) string {
	for _, s := range secrets {
		if s == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, s, "[redacted]")
		if esc := url.QueryEscape(s); esc != s {
			msg = strings.ReplaceAll(msg, esc, "[redacted]")
		}
	}
	return msg
}
