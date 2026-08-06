// Package reputation provides on-demand IP reputation/threat-intel
// lookups for public addresses, combining a free keyless source (Shodan
// InternetDB) with an optional source that needs an API key (AbuseIPDB).
// Like internal/geoip, this is entirely best-effort: a source that isn't
// configured, errors, times out, or has nothing to report just omits its
// fields from the result rather than failing the whole lookup.
package reputation

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	requestTimeout = 5 * time.Second
	cacheTTL       = 15 * time.Minute
)

// ErrNotPublic is returned for an unparseable or private/loopback/
// link-local address -- there's nothing meaningful to look up.
var ErrNotPublic = errors.New("not a public IP address")

// Result is the combined reputation/intel info for one IP. Fields are
// left at their zero value per-source if that source wasn't configured,
// errored, or had nothing to report -- never a partial-failure error.
type Result struct {
	IP           string   `json:"ip"`
	Ports        []int    `json:"ports,omitempty"`
	Hostnames    []string `json:"hostnames,omitempty"`
	Vulns        []string `json:"vulns,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	AbuseScore   *int     `json:"abuseScore,omitempty"`
	TotalReports *int     `json:"totalReports,omitempty"`
	CountryCode  string   `json:"countryCode,omitempty"`
	ISP          string   `json:"isp,omitempty"`
	// UsageType and IsTor (issue #58) are AbuseIPDB fields already
	// present in its "check" response but not previously parsed --
	// AbuseIPDB does classify hosting/data-center IP space via
	// UsageType (e.g. "Data Center/Web Hosting/Transit"), resolving
	// that issue's open question about whether a usable signal exists
	// in a source mikroview already calls. Both are AbuseIPDB-only,
	// same as AbuseScore/TotalReports -- empty/false if that source
	// isn't configured.
	UsageType string `json:"usageType,omitempty"`
	IsTor     bool   `json:"isTor,omitempty"`
}

// TorExitNodeFloor/HostingProviderFloor: starting-point confidence
// floors for RiskFloor's two signals -- deliberately smaller than a
// real AbuseIPDB abuse score, since neither is proof of malice on its
// own (Tor use isn't illegal, and plenty of legitimate scanners/CDNs/
// bots run from hosting providers too). Tuned as a reasonable starting
// point, not a calibrated value -- worth revisiting once there's real
// usage data to judge false-positive rate against.
const (
	TorExitNodeFloor     = 60
	HostingProviderFloor = 30
)

// RiskFloor returns a confidence floor derived from IsTor/UsageType, if
// either signal applies (issue #58) -- checked in descending floor
// order so the strongest applicable signal wins outright rather than
// being averaged with a weaker one. ok is false if neither applies,
// meaning this result contributes no floor from either field (a caller
// should not treat that as "confirmed clean," same absence-of-evidence
// reasoning flags.Store.RaiseConfidenceFloor already documents for
// AbuseScore).
func (r Result) RiskFloor() (floor int, ok bool) {
	if r.IsTor {
		return TorExitNodeFloor, true
	}
	if isHostingUsageType(r.UsageType) {
		return HostingProviderFloor, true
	}
	return 0, false
}

// isHostingUsageType matches AbuseIPDB's documented "Data Center/Web
// Hosting/Transit" usageType value (and near variants) rather than an
// exact string equality -- resilient to AbuseIPDB tweaking casing/
// wording without needing a mikroview release to keep matching.
func isHostingUsageType(usageType string) bool {
	lower := strings.ToLower(usageType)
	return strings.Contains(lower, "hosting") || strings.Contains(lower, "data center")
}

type cacheEntry struct {
	result  Result
	expires time.Time
}

// Client looks up IP reputation, caching results briefly so repeat
// clicks on the same IP don't re-hit AbuseIPDB's free-tier daily quota
// or Shodan's rate limit.
type Client struct {
	abuseIPDBKey string
	httpClient   *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
}

// New creates a Client. abuseIPDBKey may be empty, in which case only
// the free, keyless Shodan InternetDB source is queried.
func New(abuseIPDBKey string) *Client {
	return &Client{
		abuseIPDBKey: abuseIPDBKey,
		httpClient:   &http.Client{Timeout: requestTimeout},
		cache:        make(map[string]cacheEntry),
	}
}

// Lookup returns combined reputation info for ipStr.
func (c *Client) Lookup(ctx context.Context, ipStr string) (Result, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil || !isPublic(ip) {
		return Result{}, ErrNotPublic
	}

	c.mu.Lock()
	entry, cached := c.cache[ipStr]
	c.mu.Unlock()
	if cached && time.Now().Before(entry.expires) {
		return entry.result, nil
	}

	// Each goroutine writes only to its own local variable; the two
	// results are merged sequentially after wg.Wait() establishes a
	// happens-before relationship, so there's no concurrent write to
	// shared state here.
	var shodan, abuse Result
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		shodan = fetchShodan(ctx, c.httpClient, ipStr)
	}()

	if c.abuseIPDBKey != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			abuse = fetchAbuseIPDB(ctx, c.httpClient, c.abuseIPDBKey, ipStr)
		}()
	}

	wg.Wait()

	result := Result{
		IP:           ipStr,
		Ports:        shodan.Ports,
		Hostnames:    shodan.Hostnames,
		Vulns:        shodan.Vulns,
		Tags:         shodan.Tags,
		AbuseScore:   abuse.AbuseScore,
		TotalReports: abuse.TotalReports,
		CountryCode:  abuse.CountryCode,
		ISP:          abuse.ISP,
		UsageType:    abuse.UsageType,
		IsTor:        abuse.IsTor,
	}

	c.mu.Lock()
	c.cache[ipStr] = cacheEntry{result: result, expires: time.Now().Add(cacheTTL)}
	c.mu.Unlock()

	return result, nil
}

// fetchShodan queries Shodan's free, keyless InternetDB for open ports,
// hostnames, and known CVEs. A 404 (no data for this IP) or any other
// failure just returns a zero Result -- not every IP has InternetDB data.
func fetchShodan(ctx context.Context, client *http.Client, ip string) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://internetdb.shodan.io/"+url.PathEscape(ip), nil)
	if err != nil {
		return Result{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}
	}

	var body struct {
		Ports     []int    `json:"ports"`
		Hostnames []string `json:"hostnames"`
		Vulns     []string `json:"vulns"`
		Tags      []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Result{}
	}
	return Result{Ports: body.Ports, Hostnames: body.Hostnames, Vulns: body.Vulns, Tags: body.Tags}
}

// fetchAbuseIPDB queries AbuseIPDB's "check" endpoint. Requires a caller-
// supplied API key; any failure (bad key, rate limit, network) just
// returns a zero Result.
func fetchAbuseIPDB(ctx context.Context, client *http.Client, key, ip string) Result {
	q := url.Values{"ipAddress": {ip}, "maxAgeInDays": {"90"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.abuseipdb.com/api/v2/check?"+q.Encode(), nil)
	if err != nil {
		return Result{}
	}
	req.Header.Set("Key", key)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Result{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}
	}

	var body struct {
		Data struct {
			AbuseConfidenceScore int    `json:"abuseConfidenceScore"`
			TotalReports         int    `json:"totalReports"`
			CountryCode          string `json:"countryCode"`
			ISP                  string `json:"isp"`
			UsageType            string `json:"usageType"`
			IsTor                bool   `json:"isTor"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Result{}
	}

	score := body.Data.AbuseConfidenceScore
	reports := body.Data.TotalReports
	return Result{
		AbuseScore:   &score,
		TotalReports: &reports,
		CountryCode:  body.Data.CountryCode,
		ISP:          body.Data.ISP,
		UsageType:    body.Data.UsageType,
		IsTor:        body.Data.IsTor,
	}
}

func isPublic(ip net.IP) bool {
	return !ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast()
}
