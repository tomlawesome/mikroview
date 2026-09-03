// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/routeros/export"
)

// tuneLoggingMaxBodyBytes bounds POST /api/tune-logging/{analyse,render}
// request bodies -- its own constant beside maxJSONBodyBytes rather than
// raising the shared cap, because this is the one API surface whose
// body is a whole router config export rather than small structured
// data: the 64 KiB JSON cap every other endpoint uses is too small for
// that (#435's contract). 2 MiB is generous headroom over any real
// RouterOS filter table.
const tuneLoggingMaxBodyBytes = 2 * 1024 * 1024

// decodeTuneLoggingBody is decodeJSONBody with tuneLoggingMaxBodyBytes
// in place of the shared cap -- see that constant's doc comment.
func decodeTuneLoggingBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, tuneLoggingMaxBodyBytes)
	return json.NewDecoder(r.Body).Decode(v)
}

// tuneLoggingObserving is #435 decision 5's honesty bound: the page is
// always reachable, but says how long mikroview has been watching
// rather than pretending to have an opinion before it has one.
type tuneLoggingObserving struct {
	Since string `json:"since"`
	Hours int    `json:"hours"`
}

// tuneLoggingRouterOS mirrors POST /api/setup/commands' own picked-
// version shape (#436): the version read off the export's own header
// line, its standing against the dialect table, and the dialect that
// standing resolves to.
type tuneLoggingRouterOS struct {
	Version  string `json:"version"`
	Standing string `json:"standing"`
	Dialect  string `json:"dialect"`
}

type tuneLoggingRejectReason struct {
	Reason string `json:"reason"`
}

// tuneLoggingRule is one boundary-crossing (or not) filter rule, as
// POST /api/tune-logging/analyse's contract states field for field.
type tuneLoggingRule struct {
	ID               int    `json:"id"`
	Chain            string `json:"chain"`
	Action           string `json:"action"`
	Comment          string `json:"comment"`
	InInterface      string `json:"inInterface"`
	OutInterface     string `json:"outInterface"`
	InInterfaceList  string `json:"inInterfaceList"`
	OutInterfaceList string `json:"outInterfaceList"`
	Boundary         string `json:"boundary"`
	CrossesDark      bool   `json:"crossesDark"`
	Log              bool   `json:"log"`
	LogPrefix        string `json:"logPrefix"`
	Packets          int    `json:"packets"`
	Bytes            int    `json:"bytes"`
	CountersKnown    bool   `json:"countersKnown"`
	Line             int    `json:"line"`
}

type tuneLoggingAnalyseRequest struct {
	Device         string   `json:"device"`
	Export         string   `json:"export"`
	DarkBoundaries []string `json:"darkBoundaries"`
}

type tuneLoggingAnalyseResponse struct {
	Ready     bool                     `json:"ready"`
	Observing tuneLoggingObserving     `json:"observing"`
	RouterOS  tuneLoggingRouterOS      `json:"routeros"`
	Rules     []tuneLoggingRule        `json:"rules"`
	Rejected  *tuneLoggingRejectReason `json:"rejected"`
}

type tuneLoggingRenderRequest struct {
	Device   string `json:"device"`
	Export   string `json:"export"`
	Selected []int  `json:"selected"`
}

type tuneLoggingRenderResponse struct {
	Annotated string              `json:"annotated"`
	Commands  string              `json:"commands"`
	Changed   int                 `json:"changed"`
	RouterOS  tuneLoggingRouterOS `json:"routeros"`
}

// handleTuneLoggingAnalyse is POST /api/tune-logging/analyse (#435):
// parses the uploaded export, reports how long mikroview has observed
// this device, and -- once that reaches 24 hours -- every filter rule
// that crosses one of the caller's dark boundaries, with its packet/
// byte counters from the latest push where they can be matched.
//
// Nothing about req.Export is logged, persisted, or echoed back beyond
// this response: it lives in memory for the duration of this call and
// nowhere else, per the issue's own "never persisted" invariant.
func (s *Server) handleTuneLoggingAnalyse(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}

	var req tuneLoggingAnalyseRequest
	if err := decodeTuneLoggingBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ex, err := export.Parse(req.Export)
	if err != nil {
		writeTuneLoggingRejection(w, err)
		return
	}

	dialect := defaultDialect()
	standing := routeros.VersionStanding(ex.Version).String()
	if row, ok := routeros.RowFor(ex.Version); ok {
		dialect = row.Dialect
	}

	since, hasSince := deviceObservedSince(s, req.Device)
	var observing tuneLoggingObserving
	ready := false
	if hasSince {
		observing.Since = since.UTC().Format(time.RFC3339)
		elapsed := time.Since(since)
		observing.Hours = int(elapsed.Hours())
		ready = elapsed >= 24*time.Hour
	}

	rules := make([]tuneLoggingRule, 0)
	if ready {
		rules = buildTuneLoggingRules(s, req.Device, ex, req.DarkBoundaries)
	}

	writeJSON(w, http.StatusOK, tuneLoggingAnalyseResponse{
		Ready:     ready,
		Observing: observing,
		RouterOS:  tuneLoggingRouterOS{Version: ex.Version, Standing: standing, Dialect: dialect},
		Rules:     rules,
		Rejected:  nil,
	})
}

// renderExport is Export.Render behind a package-level variable so a
// test can substitute a deliberately broken implementation and prove
// handleTuneLoggingRender's mechanical logging-only check actually
// bites -- see tunelogging_test.go.
var renderExport = func(ex *export.Export, selected []int, prefixFor export.LogPrefixFunc) (string, int) {
	return ex.Render(selected, prefixFor)
}

// handleTuneLoggingRender is POST /api/tune-logging/render (#435):
// switches logging on for the selected rules in the uploaded export,
// and renders the same change as one `set` command per rule.
//
// The output is mechanically checked before it is ever returned: the
// annotated text is re-parsed, every rule's Fingerprint (everything
// except log/log-prefix) is compared against the input's, and any
// difference -- meaning something other than logging changed -- refuses
// the whole response with a 500 rather than shipping a config edit
// nobody asked for. See export.Rule.Fingerprint.
func (s *Server) handleTuneLoggingRender(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}

	var req tuneLoggingRenderRequest
	if err := decodeTuneLoggingBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	before, err := export.Parse(req.Export)
	if err != nil {
		writeTuneLoggingRejection(w, err)
		return
	}

	dialect := defaultDialect()
	standing := routeros.VersionStanding(before.Version).String()
	if row, ok := routeros.RowFor(before.Version); ok {
		dialect = row.Dialect
	}
	prefixFor := func(action string) string { return routeros.LogPrefixForAction(action, dialect) }

	annotated, changed := renderExport(before, req.Selected, prefixFor)

	after, err := export.Parse(annotated)
	if err != nil || !loggingOnlyDiff(before, after) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "logging-only check failed"})
		return
	}

	writeJSON(w, http.StatusOK, tuneLoggingRenderResponse{
		Annotated: annotated,
		Commands:  buildTuneLoggingCommands(before, req.Selected, prefixFor),
		Changed:   changed,
		RouterOS:  tuneLoggingRouterOS{Version: before.Version, Standing: standing, Dialect: dialect},
	})
}

// writeTuneLoggingRejection answers a *export.SecretFieldError with the
// contract's 400 {"rejected":{"reason":...}} shape; any other error
// (Parse itself never returns one today) falls back to a plain 400.
func writeTuneLoggingRejection(w http.ResponseWriter, err error) {
	var secretErr *export.SecretFieldError
	if errors.As(err, &secretErr) {
		writeJSON(w, http.StatusBadRequest, map[string]tuneLoggingRejectReason{
			"rejected": {Reason: secretErr.Error()},
		})
		return
	}
	http.Error(w, "invalid export", http.StatusBadRequest)
}

// deviceObservedSince is #435 contract's "observed since" source: the
// device registry's FirstSeen. The contract's preferred source -- the
// updatedAt of a device's *earliest* push -- is not actually kept
// anywhere: internal/routerstate.Store only tracks each kind's latest
// update, overwritten on every push, so there is no earliest to read.
// FirstSeen is the fallback the contract names for exactly that case,
// and it is what this always uses. ok is false when the device is
// unknown to the registry, or has never actually been seen (FirstSeen
// zero) -- both read as "nothing to say yet", not as ready.
func deviceObservedSince(s *Server, deviceID string) (time.Time, bool) {
	if s.Devices == nil {
		return time.Time{}, false
	}
	for _, info := range s.Devices.List() {
		if info.ID == deviceID {
			if info.FirstSeen.IsZero() {
				return time.Time{}, false
			}
			return info.FirstSeen, true
		}
	}
	return time.Time{}, false
}

// crossesDarkBoundary reports whether a rule scoped to (in, out) can
// carry traffic across any of darkBoundaries, each in the frontend's
// "${from}|${to}" form (frontend/src/lib/policy.svelte.ts).
//
// The matching choice (#435, no design record covers this beyond the
// contract's one-line gloss): a blank side, on either the rule or the
// boundary, is a wildcard. A rule with no in-interface scopes every
// inbound direction, so it crosses a dark boundary whose "from" side is
// unspecified or matches; symmetrically for out. This is what "the rule
// names no interface on a side (matches every boundary on that side)"
// in the contract means literally -- an unscoped side matches
// everything the other side allows, in both directions, rather than
// only an exact string match on the full pair.
func crossesDarkBoundary(in, out string, darkBoundaries []string) bool {
	for _, d := range darkBoundaries {
		df, dt, _ := strings.Cut(d, "|")
		inMatch := in == df || in == "" || df == ""
		outMatch := out == dt || out == "" || dt == ""
		if inMatch && outMatch {
			return true
		}
	}
	return false
}

// buildTuneLoggingRules derives every rule in ex's filter section into
// the wire shape, matching each against the device's latest push for
// its packet/byte counters (ordinal == id, chain and action agreeing --
// the contract's stated match).
func buildTuneLoggingRules(s *Server, device string, ex *export.Export, darkBoundaries []string) []tuneLoggingRule {
	var pushed []ingest.FilterRule
	havePushed := false
	if s.RouterState != nil {
		pushed, _, havePushed = s.RouterState.FilterRules(device)
	}
	pushedByOrdinal := make(map[int]ingest.FilterRule, len(pushed))
	for _, p := range pushed {
		pushedByOrdinal[int(p.Ordinal)] = p
	}

	out := make([]tuneLoggingRule, 0, len(ex.FilterRules))
	for _, rule := range ex.FilterRules {
		boundary := ""
		crosses := false
		if rule.Chain == "forward" {
			boundary = rule.InInterface + "|" + rule.OutInterface
			crosses = crossesDarkBoundary(rule.InInterface, rule.OutInterface, darkBoundaries)
		}

		packets, bytesCount, known := 0, 0, false
		if havePushed {
			if p, ok := pushedByOrdinal[rule.Index]; ok && p.Chain == rule.Chain && p.Action == rule.Action {
				packets, bytesCount, known = int(p.Packets), int(p.Bytes), true
			}
		}

		out = append(out, tuneLoggingRule{
			ID:               rule.Index,
			Chain:            rule.Chain,
			Action:           rule.Action,
			Comment:          rule.Comment,
			InInterface:      rule.InInterface,
			OutInterface:     rule.OutInterface,
			InInterfaceList:  rule.InInterfaceList,
			OutInterfaceList: rule.OutInterfaceList,
			Boundary:         boundary,
			CrossesDark:      crosses,
			Log:              rule.Log,
			LogPrefix:        rule.LogPrefix,
			Packets:          packets,
			Bytes:            bytesCount,
			CountersKnown:    known,
			Line:             rule.Line,
		})
	}
	return out
}

// loggingOnlyDiff is POST /api/tune-logging/render's mechanical
// enforcement of the issue's central invariant: before and after must
// decode to the same number of filter rules, each fingerprinting equal
// (i.e. identical in everything but log/log-prefix -- see
// export.Rule.Fingerprint).
func loggingOnlyDiff(before, after *export.Export) bool {
	if len(before.FilterRules) != len(after.FilterRules) {
		return false
	}
	for i := range before.FilterRules {
		if before.FilterRules[i].Fingerprint() != after.FilterRules[i].Fingerprint() {
			return false
		}
	}
	return true
}

// matcherFor picks how POST /api/tune-logging/render's per-rule `set`
// command addresses r: `[find comment=...]` when r's comment is set and
// unique among the filter section's rules -- the readable choice, and
// the one RouterOS's own `find` accepts unambiguously -- and
// `[find numbers=N]`, N being r's own 0-based position among the filter
// section's add lines, for every rule that has no comment or shares one
// with another rule (a comment match there could hit the wrong line).
// `numbers=` addressing by that position is exactly what RouterOS means
// by it for a config with no dynamic filter rules, which /export never
// emits.
func matcherFor(rules []export.Rule, r export.Rule) string {
	if r.Comment != "" {
		count := 0
		for _, other := range rules {
			if other.Comment == r.Comment {
				count++
			}
		}
		if count == 1 {
			return fmt.Sprintf("[find comment=%s]", export.Quote(r.Comment))
		}
	}
	return fmt.Sprintf("[find numbers=%d]", r.Index)
}

// buildTuneLoggingCommands renders the render response's "commands"
// field: one `/ip firewall filter set ... log=yes log-prefix=...` line
// per selected rule, using the identical prefix resolution Render
// itself used (empty log-prefix only) so the two outputs can never
// disagree about what a rule ends up logging as.
func buildTuneLoggingCommands(ex *export.Export, selected []int, prefixFor export.LogPrefixFunc) string {
	byIndex := make(map[int]export.Rule, len(ex.FilterRules))
	for _, r := range ex.FilterRules {
		byIndex[r.Index] = r
	}

	seen := make(map[int]bool, len(selected))
	var ids []int
	for _, id := range selected {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, ok := byIndex[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)

	var lines []string
	for _, id := range ids {
		r := byIndex[id]
		prefix := r.LogPrefix
		if prefix == "" {
			prefix = prefixFor(r.Action)
		}
		lines = append(lines, fmt.Sprintf("/ip firewall filter set %s log=yes log-prefix=%s", matcherFor(ex.FilterRules, r), export.Quote(prefix)))
	}
	return strings.Join(lines, "\n")
}
