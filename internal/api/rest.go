// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"syscall"
	"time"

	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/syslog"
)

var apiLog = logging.New("api")

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC(),
		"uptime": time.Since(s.StartTime).String(),
		// uptimeSeconds duplicates uptime for the UI's toolbar counter:
		// Go's duration string ("73h4m2.1s") is for humans reading curl
		// output, not for a client that wants to keep counting locally.
		"uptimeSeconds": int64(time.Since(s.StartTime).Seconds()),
		"version":       s.Version,
	})
}

// handleEvents serves a filtered, windowed slice of the retained buffer —
// used for the initial page load and "load older" pagination. The live
// WebSocket tail (handleWS) intentionally applies no server-side filtering;
// see ws.go.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	q, ok := parseQuery(w, r)
	if !ok {
		return
	}
	res := s.Store.Query(q)
	writeJSON(w, http.StatusOK, res)
}

// deviceView adds a read-time-computed status to device.Info for the
// fleet-health view (issue #98) -- Info itself stays raw data with no
// notion of "stale," the same separation store.Event/detect keep between
// stored fact and derived judgment. Info is embedded rather than copied
// field-by-field so its JSON tags are reused as-is and this struct only
// ever needs to know about the one field it adds.
type deviceView struct {
	device.Info
	// Status is "live" (an event within DeviceStaleAfter), "stale" (an
	// event, but not within DeviceStaleAfter -- or DeviceStaleAfter isn't
	// configured at all and one just hasn't arrived in a while, see
	// deviceStatus), or "never_seen" (Configured: true in config.yaml,
	// but zero events ever received). Distinct from -- and a superset
	// of -- TypeDeviceSilence: that flag only ever fires for a
	// Configured device that *was* active and went quiet, so a
	// never-contacted device is never flagged even though it's also not
	// "live" here.
	Status string `json:"status"`
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	infos := s.Devices.List()
	views := make([]deviceView, 0, len(infos))
	for _, info := range infos {
		views = append(views, deviceView{Info: info, Status: deviceStatus(info, s.DeviceStaleAfter, now)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": views})
}

// handleRules serves every rule label mikroview has ever seen fire
// (internal/rules.Store, issue #103's persisted, unbounded-time usage
// record), each with its first/last-seen time and count -- issue #109's
// "discovered but unnamed" source for the Entities admin panel, the same
// role GET /api/devices already plays for auto-discovered hosts. Not
// admin-gated, same as GET /api/devices: this is read-only usage data,
// not the entity records themselves (POST/DELETE /api/entities stay
// admin-only).
func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"rules": s.Rules.List()})
}

// deviceStatus computes info's fleet-health status as of now -- see
// deviceView.Status for what each value means. A device with no events
// yet is "never_seen" regardless of DeviceStaleAfter (there's no elapsed
// time to measure against a threshold); everything else is "stale" once
// DeviceStaleAfter has passed since LastSeen, "live" otherwise. An
// unconfigured DeviceStaleAfter (0, "not configured") means nothing ever
// reports "stale" here -- consistent with DeviceSilenceDetector.Check's
// own "0 disables the detector" contract, so the API and the actual
// alert never disagree about whether staleness detection is even on.
func deviceStatus(info device.Info, staleAfter time.Duration, now time.Time) string {
	if info.LastSeen.IsZero() {
		return "never_seen"
	}
	if staleAfter > 0 && now.Sub(info.LastSeen) >= staleAfter {
		return "stale"
	}
	return "live"
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.Store.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"total":            stats.Total,
		"byAction":         stats.ByAction,
		"topRules":         stats.TopRules,
		"timeSeries":       stats.TimeSeries,
		"eventsPerSecond":  stats.EventsPerSecond,
		"capacity":         stats.Capacity,
		"count":            stats.Count,
		"windowSeconds":    int(stats.Window.Seconds()),
		"connectedClients": s.Hub.ClientCount(),
		// Syslog listener saturation. Included here rather than behind
		// its own endpoint because the condition it reports -- mikroview
		// turning away a router the operator declared -- was previously
		// visible only as a repeated line in the container log, which
		// means visible to nobody. See internal/syslog.ListenerStats.
		"syslog": syslog.Stats(),
	})
}

// handleStatsTops serves #644 round 21's top-port/top-talker table
// columns: store.Store.HourTops, computed fresh from whatever the ring
// buffer currently holds. Deliberately its own endpoint rather than a
// field folded into GET /api/stats above -- that one is polled by every
// open browser tab every STATS_REFRESH_MS regardless of which page is
// showing (App.svelte), and HourTops' backward scan of the last hour's
// events is heavier than anything else Stats() returns, none of which
// touches the buffer past its own O(1) rolling counters. Fetched only
// by the Metrics page itself (Metrics.svelte), on the same cadence but
// scoped to while that page is actually open -- the same pattern
// Fall.svelte already uses for its own per-page poll. Same access tier
// as GET /api/stats (see authzMatrix): a read over data that endpoint
// already exposes in aggregate, just broken out per minute.
func (s *Server) handleStatsTops(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tops": s.Store.HourTops()})
}

// badQueryParam is the one shape a malformed windowed-query parameter
// takes across every endpoint that accepts one.
//
// Malformed means *present and unparseable*, not absent. Absent is a
// clear "no filter"; present-and-unparseable is a caller who believes
// they filtered. Returning 200 with unfiltered results in that case is
// a silent lie, and in a tool whose whole job is showing an operator
// what happened in a window, being shown everything while believing you
// are looking at a window is the misreading that matters. It is the same
// class as #267's own Tier 1 finding about a non-numeric port filter
// blanking the live table, which was treated as High.
//
// This used to be "ignore rather than fail" here and on GET /api/audit,
// while GET /api/watchlist/matches -- taking the identical parameter
// names -- returned 400. The convention was circular ("the same
// treatment every other malformed param here gets") and the two
// behaviours could not both be right (#267 finding 8). They now all
// refuse.
//
// parseScope keeps its fallback, and is not an exception to this: scope
// is an enum where unset genuinely means ScopeAny, so there is no
// "unparseable" state to report.
func badQueryParam(w http.ResponseWriter, name, want string) {
	http.Error(w, name+" must be "+want, http.StatusBadRequest)
}

// parseQuery returns ok=false once it has written the error response.
func parseQuery(w http.ResponseWriter, r *http.Request) (store.Query, bool) {
	qs := r.URL.Query()
	q := store.Query{
		Device:    qs.Get("device"),
		Action:    store.Action(qs.Get("action")),
		Protocol:  qs.Get("protocol"),
		Chain:     qs.Get("chain"),
		Interface: qs.Get("interface"),
		IP:        qs.Get("ip"),
		SrcScope:  parseScope(qs.Get("srcScope")),
		DstScope:  parseScope(qs.Get("dstScope")),
		Rule:      qs.Get("rule"),
		RuleRegex: qs.Get("ruleRegex") == "true",
	}
	if v := qs.Get("port"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			badQueryParam(w, "port", "an integer")
			return store.Query{}, false
		}
		q.Port = n
	}
	if v := qs.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			badQueryParam(w, "since", "RFC 3339")
			return store.Query{}, false
		}
		q.Since = t
	}
	if v := qs.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			badQueryParam(w, "until", "RFC 3339")
			return store.Query{}, false
		}
		q.Until = t
	}
	// around+window (issue #29) is sugar for a bounded before/after
	// lookback centered on a timestamp -- overrides since/until if both
	// forms are present, since specifying a center point and specifying
	// explicit bounds are two ways of asking for the same thing.
	if v := qs.Get("around"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			badQueryParam(w, "around", "RFC 3339")
			return store.Query{}, false
		}
		window := 5 * time.Minute
		if wv := qs.Get("window"); wv != "" {
			d, err := time.ParseDuration(wv)
			if err != nil {
				badQueryParam(w, "window", "a duration such as 5m")
				return store.Query{}, false
			}
			window = d
		}
		q.Since = t.Add(-window)
		q.Until = t.Add(window)
	}
	if v := qs.Get("sinceId"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			badQueryParam(w, "sinceId", "a positive integer")
			return store.Query{}, false
		}
		q.SinceID = n
	}
	if v := qs.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			badQueryParam(w, "limit", "an integer")
			return store.Query{}, false
		}
		q.Limit = n
	}
	return q, true
}

// parseScope accepts only the two recognized scope values -- anything
// else (including unset/empty) falls back to store.ScopeAny rather than
// erroring, the same "ignore rather than fail" treatment every other
// malformed query param here gets.
func parseScope(v string) store.Scope {
	switch store.Scope(v) {
	case store.ScopeInternal, store.ScopeExternal:
		return store.Scope(v)
	default:
		return store.ScopeAny
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status line and headers are already on the wire by this point,
	// so a failure here cannot be turned into a 500 -- the client will
	// see a truncated body regardless. Log it rather than discard it:
	// silently serving a half-written response is the kind of fault that
	// otherwise only ever surfaces as an unreproducible frontend parse
	// error.
	//
	// Except when the *client* is what failed: a closed tab or a phone
	// locking mid-poll surfaces here as a broken pipe / connection
	// reset, and with the UI polling every few seconds that's routine
	// behaviour, not a fault (#322 item 2). Those go to DEBUG -- still
	// there when chasing something -- while real encode failures (a
	// value that can't marshal is a bug) stay WARN.
	if err := json.NewEncoder(w).Encode(v); err != nil {
		if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, context.Canceled) {
			apiLog.Debug(fmt.Sprintf("client went away mid-response (status %d): %v", status, err))
		} else {
			apiLog.Warn(fmt.Sprintf("writing JSON response failed after %d was already sent: %v", status, err))
		}
	}
}

// maxJSONBodyBytes bounds every JSON request body this API accepts --
// every payload here is small, bounded structured data (credentials, a
// username/role, one detector's Scope config), so 64 KiB is already
// generous headroom, not a tight fit. Without this, json.NewDecoder
// would read an unbounded body into memory before ever validating it,
// including on endpoints reachable before authentication (login,
// register) -- a handful of concurrent large-bodied requests from an
// unauthenticated client would mean memory allocation proportional to
// body size with no credentials required, a straightforward DoS.
const maxJSONBodyBytes = 64 * 1024

// decodeJSONBody is json.NewDecoder(r.Body).Decode(v), wrapped with
// http.MaxBytesReader -- see maxJSONBodyBytes. Every handler that
// accepts a JSON body should read it through this, not r.Body
// directly.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	return json.NewDecoder(r.Body).Decode(v)
}

// handleThirdPartyNotices serves THIRD-PARTY-NOTICES.md verbatim, as
// plain text.
//
// This is a licence-compliance surface, not a feature. The binary
// statically links seventeen Go modules and embeds a frontend bundle
// containing third-party runtime code; MIT, BSD-3-Clause, ISC and
// Apache-2.0 each require their copyright notice and licence text to
// accompany a binary distribution, and Apache-2.0 s4(d) requires any
// NOTICE file to be passed along too. The runtime image is distroless --
// the binary is the entire artefact -- so "accompany" can only mean
// "inside the binary, reachable from the running app", which is what
// this route and the About dialog's link to it provide.
//
// Session-gated (accessUser) rather than public: the notices also live
// in the public repository and in the image, so gating them withholds
// nothing from anyone entitled to them, while not handing an
// unauthenticated caller a precise dependency-and-version inventory to
// match against CVEs.
func (s *Server) handleThirdPartyNotices(w http.ResponseWriter, r *http.Request) {
	if s.ThirdPartyNotices == "" {
		// Only reachable in a test fixture or a hand-built binary; a
		// real build always embeds it (notices.go), and CI fails if the
		// file is stale.
		http.Error(w, "third-party notices were not embedded in this build", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	io.WriteString(w, s.ThirdPartyNotices)
}
