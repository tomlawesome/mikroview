package reputation

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// GreyNoiseClient is internal/reputation's second live source (issue
// #113 Part A) -- GreyNoise's Community API, purpose-built for a
// question Client's AbuseIPDB/Shodan sources don't answer well:
// distinguishing internet-wide background scanning/research noise from
// traffic GreyNoise has actually classified as malicious (see
// Result.Classification/Noise/Riot/ActorName's doc comment). Same
// on-demand, best-effort, briefly-cached shape as Client -- a source
// that errors, times out, or has nothing to report just returns a zero
// Result, never a partial-failure error.
//
// Despite GreyNoise's own marketing describing the Community API as
// keyless, its actual `/v3/community/{ip}` endpoint requires a free
// registered API key (obtained via a GreyNoise account) to authenticate
// requests -- confirmed by inspecting their current API docs while
// implementing this, not merely assumed. Rather than shipping a client
// that silently 401s for every deployment that hasn't done that extra
// signup step, GreyNoiseClient follows exactly the same optional-key
// gating AbuseIPDB's fetchAbuseIPDB already uses: APIKey empty means
// this source is never queried (see Aggregator/main.go, which only
// constructs a GreyNoiseClient at all when cfg.Reputation.GreyNoise.APIKey
// is set), rather than mikroview pretending a keyless community tier
// exists when in practice it doesn't.
type GreyNoiseClient struct {
	apiKey     string
	httpClient *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
}

// NewGreyNoiseClient creates a GreyNoiseClient. apiKey is required --
// see the type's doc comment for why this differs from Client's Shodan
// source, which really is keyless.
func NewGreyNoiseClient(apiKey string) *GreyNoiseClient {
	return &GreyNoiseClient{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: requestTimeout},
		cache:      make(map[string]cacheEntry),
	}
}

// Lookup returns GreyNoise's classification info for ipStr, satisfying
// the same Source interface (see aggregator.go) Client and Aggregator
// implement -- so a *GreyNoiseClient can be handed to
// internal/detect.Detector.WithReputation directly in tests/standalone
// use, even though main.go always wraps it in an Aggregator alongside
// Client in practice.
func (c *GreyNoiseClient) Lookup(ctx context.Context, ipStr string) (Result, error) {
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

	result := fetchGreyNoise(ctx, c.httpClient, c.apiKey, ipStr)

	c.mu.Lock()
	c.cache[ipStr] = cacheEntry{result: result, expires: time.Now().Add(cacheTTL)}
	c.mu.Unlock()

	return result, nil
}

// greyNoiseBaseURL is a var (not a const) purely so greynoise_test.go can
// point it at an httptest.Server and exercise fetchGreyNoise's real
// request/response handling end to end, restoring the real value
// afterward -- the only network-endpoint override in this package, the
// rest of which (Client's Shodan/AbuseIPDB URLs) has no equivalent hook
// and so stays untested against real HTTP shapes (see reputation_test.go's
// own doc comment on that gap).
var greyNoiseBaseURL = "https://api.greynoise.io/v3/community/"

// fetchGreyNoise queries GreyNoise's Community API "quick classification"
// endpoint. Any failure (missing/bad key, rate limit, network, no data
// for this IP) just returns a zero Result -- not every IP has GreyNoise
// data, and this is a best-effort enrichment source like every other one
// in this package.
func fetchGreyNoise(ctx context.Context, client *http.Client, apiKey, ip string) Result {
	if apiKey == "" {
		return Result{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, greyNoiseBaseURL+url.PathEscape(ip), nil)
	if err != nil {
		return Result{}
	}
	req.Header.Set("key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Result{}
	}
	defer resp.Body.Close()
	// 404 is GreyNoise's documented "no data for this IP" response, not
	// a failure -- same "absence of data, not an error" treatment
	// fetchShodan already gives its own 404 case.
	if resp.StatusCode != http.StatusOK {
		return Result{}
	}

	var body struct {
		Noise          bool   `json:"noise"`
		Riot           bool   `json:"riot"`
		Classification string `json:"classification"`
		Name           string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Result{}
	}

	return Result{
		Noise:          body.Noise,
		Riot:           body.Riot,
		Classification: body.Classification,
		ActorName:      body.Name,
	}
}
