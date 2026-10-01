// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/blcatalogue"
	"github.com/tomlawesome/mikroview/internal/droplist"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routeros"
)

// The blocklist builder's API (#1360, BUILD.md parts 5 and 6): the page
// that builds the RouterOS block making a router fetch known-bad lists
// straight from their sources and drop what is on them.
//
// Admin-only, both routes, on the session-gated table beside
// /api/droplist: the block re-sets the router's push script with a
// fresh ingest token and writes enforcement onto the router, so it is a
// setup task in the same tier as minting the drop list's key. MikroView
// never runs any of it -- it prints commands (AGENTS.md, "MikroView
// observes; it never scans or connects") -- and never serves, copies or
// vendors a list: the catalogue below is names, URLs, terms and recipes.
//
// The router's RouterOS version comes from its own push
// (RouterState.RouterOSVersion), never from the request: the commands
// depend on it, and a client that could name one could talk the page
// into writing syntax the router does not have.

// Standings a device can hold on the builder page.
const (
	blocklistStandingOK         = "ok"
	blocklistStandingBelowFloor = "below-floor"
	blocklistStandingNoPush     = "no-push"
)

// Ledger row states the server reports. The page adds "chosen" for a
// list that is off on the router but switched on in the page.
const (
	blocklistStateOff        = "off"
	blocklistStateHeld       = "held"
	blocklistStateBelowFloor = "below-floor"
)

// blocklistRefreshChoice is one refresh a card offers on this router,
// with the release that brought it where that is worth a tag (the drawn
// "weekdays 7.24").
type blocklistRefreshChoice struct {
	Value blcatalogue.Refresh `json:"value"`
	Since string              `json:"since,omitempty"`
}

// blocklistCatalogueEntry is one card, as this router's version offers it.
type blocklistCatalogueEntry struct {
	Key                string                   `json:"key"`
	Name               string                   `json:"name"`
	Short              string                   `json:"short"`
	URL                string                   `json:"url"`
	URL6               string                   `json:"url6,omitempty"`
	Terms              string                   `json:"terms"`
	Caveat             string                   `json:"caveat,omitempty"`
	Default            bool                     `json:"default"`
	DefaultDirection   blcatalogue.Direction    `json:"defaultDirection"`
	IPv6               bool                     `json:"ipv6"`
	Refresh            []blocklistRefreshChoice `json:"refresh"`
	RefreshDefault     blcatalogue.Refresh      `json:"refreshDefault"`
	Facts              string                   `json:"facts"`
	Guide              string                   `json:"guide"`
	FlaggedByMikroView bool                     `json:"flaggedByMikroView"`
	StartTime          string                   `json:"startTime"`
}

// blocklistLedgerRow is what the router holds of one list, from its
// own push (BUILD.md part 6).
type blocklistLedgerRow struct {
	Key   string `json:"key"`
	State string `json:"state"`
	// Count and Count6 are the router's mv-bl-<key> and mv-bl-<key>6
	// entry counts; LoadedAt the first entry's creation-time, in the
	// router's own clock and wording ("2026-10-01 04:17:02").
	Count    int    `json:"count"`
	Count6   int    `json:"count6"`
	LoadedAt string `json:"loadedAt,omitempty"`
	// FiredToday is how often the list's rules dropped a packet since
	// local midnight (part 6); nil until the router has pushed its raw
	// rules at least once.
	FiredToday *int64 `json:"firedToday"`
	// Flags24h is how many flags MikroView raised from this list in the
	// last 24 hours -- only for a list MikroView flags from; nil for the
	// rest ("MikroView does not flag from it").
	Flags24h *int `json:"flags24h"`
	// Rules are the comments of this list's rules the router holds,
	// family-qualified (routeros.RawRuleKey), so the page knows which
	// variants are on the router.
	Rules []string `json:"rules,omitempty"`
	// Undo is this list's own undo lines.
	Undo string `json:"undo"`
}

// blocklistOwnDroplist is the rail's first row: the operator's own drop
// list, as this router holds it.
type blocklistOwnDroplist struct {
	Held        int       `json:"held"`
	Total       int       `json:"total"`
	ConfirmedAt time.Time `json:"confirmedAt,omitzero"`
	FetchedAt   time.Time `json:"fetchedAt,omitzero"`
}

type blocklistDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type blocklistBuilderResponse struct {
	Device          string                    `json:"device"`
	DeviceName      string                    `json:"deviceName"`
	Devices         []blocklistDevice         `json:"devices"`
	RouterOSVersion string                    `json:"routerosVersion"`
	ReportedAt      time.Time                 `json:"reportedAt,omitzero"`
	ReviewedVersion string                    `json:"reviewedVersion"`
	MinimumVersion  string                    `json:"minimumVersion"`
	Standing        string                    `json:"standing"`
	PushCurrent     bool                      `json:"pushCurrent"`
	CatalogueDate   string                    `json:"catalogueDate"`
	Catalogue       []blocklistCatalogueEntry `json:"catalogue"`
	LeftOut         []blcatalogue.LeftOut     `json:"leftOut"`
	Lists           []blocklistLedgerRow      `json:"lists"`
	OwnDroplist     blocklistOwnDroplist      `json:"ownDroplist"`
	UndoAll         string                    `json:"undoAll"`
	DisableAll      string                    `json:"disableAll"`
	// Upgrade is the commands a router below the floor upgrades with;
	// empty otherwise.
	Upgrade string `json:"upgrade,omitempty"`
}

// blocklistVersion is device's version as its push reported it, the
// bare number without RouterOS's " (stable)" channel suffix, and when
// it was reported. ok is false when the device has never pushed one.
func (s *Server) blocklistVersion(device string) (version string, at time.Time, ok bool) {
	if s.RouterState == nil {
		return "", time.Time{}, false
	}
	v, at, ok := s.RouterState.RouterOSVersion(device)
	if !ok || strings.TrimSpace(v) == "" {
		return "", time.Time{}, false
	}
	return strings.Fields(v)[0], at, true
}

// handleBlocklistBuilder is GET /api/blocklist/builder?device=: everything
// the page draws for one router before the operator clicks anything.
func (s *Server) handleBlocklistBuilder(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	device := r.URL.Query().Get("device")
	if device == "" || !validSetupDevice(device) {
		http.Error(w, "device must be 1 to 64 characters from letters, digits, '.', '_' and '-'", http.StatusBadRequest)
		return
	}

	resp := blocklistBuilderResponse{
		Device:          device,
		DeviceName:      device,
		Devices:         s.blocklistDevices(),
		ReviewedVersion: routeros.ReviewedVersion,
		MinimumVersion:  routeros.MinimumVersion,
		CatalogueDate:   blcatalogue.Reviewed,
		LeftOut:         blcatalogue.LeftOutLists(),
		UndoAll:         routeros.BlocklistUndoAll(),
		DisableAll:      routeros.BlocklistDisableAll(),
	}
	for _, d := range resp.Devices {
		if d.ID == device {
			resp.DeviceName = d.Name
		}
	}

	version, reportedAt, pushed := s.blocklistVersion(device)
	feat, atFloor := routeros.FeaturesFor(version)
	switch {
	case !pushed:
		resp.Standing = blocklistStandingNoPush
	case !atFloor:
		resp.Standing = blocklistStandingBelowFloor
		resp.Upgrade = routeros.RouterOSUpgradeCommands()
	default:
		resp.Standing = blocklistStandingOK
	}
	resp.RouterOSVersion = version
	resp.ReportedAt = reportedAt

	for _, l := range blcatalogue.Lists() {
		e := blocklistCatalogueEntry{
			Key: l.Key, Name: l.Name, Short: l.Short, URL: l.URL, URL6: l.URL6,
			Terms: l.Terms, Caveat: l.Caveat, Default: l.Default, DefaultDirection: l.DefaultDirection,
			IPv6: l.IPv6, RefreshDefault: l.RefreshDefaultFor(feat.SchedulerDays),
			Facts: l.Facts, Guide: l.Guide, FlaggedByMikroView: l.FlaggedByMikroView,
			StartTime: routeros.BlocklistStartTime(l.Key),
		}
		for _, c := range l.RefreshChoicesFor(feat.SchedulerDays) {
			choice := blocklistRefreshChoice{Value: c}
			if c == blcatalogue.RefreshWeekdays {
				choice.Since = routeros.SchedulerDaysSince
			}
			e.Refresh = append(e.Refresh, choice)
		}
		resp.Catalogue = append(resp.Catalogue, e)
	}

	resp.Lists = s.blocklistLedger(device, resp.Standing, time.Now())
	resp.PushCurrent = s.blocklistPushCurrent(device)
	resp.OwnDroplist = s.blocklistOwnDroplist(device)
	writeJSON(w, http.StatusOK, resp)
}

// blocklistDevices is every router the registry knows, for the page's
// router picker, sorted by name.
func (s *Server) blocklistDevices() []blocklistDevice {
	out := []blocklistDevice{}
	if s.Devices == nil {
		return out
	}
	for _, info := range s.Devices.List() {
		name := info.Name
		if name == "" {
			name = info.ID
		}
		out = append(out, blocklistDevice{ID: info.ID, Name: name})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// blocklistPushCurrent reports whether device's push already carries the
// two kinds the builder needs, so part 1 of the block changes nothing
// it does.
func (s *Server) blocklistPushCurrent(device string) bool {
	if s.RouterState == nil {
		return false
	}
	kinds := s.RouterState.PushedKinds(device)
	_, raw := kinds[ingest.KindRawRule]
	_, counts := kinds[ingest.KindAddressListCount]
	return raw && counts
}

// blocklistOwnDroplist is device's hold on the operator's own drop list
// (#1225's drift figure), and when the pull key last fetched it.
func (s *Server) blocklistOwnDroplist(device string) blocklistOwnDroplist {
	var out blocklistOwnDroplist
	if s.Droplist == nil {
		return out
	}
	entries := s.Droplist.List()
	out.Total = len(entries)
	if s.RouterState != nil {
		if snapshot, at, ok := s.RouterState.AddressLists(device); ok {
			out.Held = droplist.Held(entries, snapshot)
			out.ConfirmedAt = at
		}
	}
	if s.Tokens != nil {
		out.FetchedAt = s.droplistKeyStatus().LastUsedAt
	}
	return out
}

// blocklistLedger is one row per catalogue list: what device holds of
// it, from its own push.
func (s *Server) blocklistLedger(device, standing string, now time.Time) []blocklistLedgerRow {
	var counts []ingest.AddressListCount
	var rules []ingest.RawRule
	if s.RouterState != nil {
		counts, _, _ = s.RouterState.AddressListCounts(device)
		rules, _, _ = s.RouterState.RawRules(device)
	}
	rows := []blocklistLedgerRow{}
	for _, l := range blcatalogue.Lists() {
		undo, _ := routeros.BlocklistUndo(l.Key)
		row := blocklistLedgerRow{Key: l.Key, State: blocklistStateOff, Undo: undo}
		for _, c := range counts {
			switch c.List {
			case blcatalogue.ListName(l.Key):
				if c.Family == ingest.FamilyIP {
					row.Count = int(c.Count)
					row.LoadedAt = c.LoadedAt
				}
			case blcatalogue.ListName6(l.Key):
				if c.Family == ingest.FamilyIPv6 {
					row.Count6 = int(c.Count)
				}
			}
		}
		for _, rr := range rules {
			if strings.HasPrefix(rr.Comment, routeros.BlocklistRuleCommentPrefix(l.Key)) {
				row.Rules = append(row.Rules, routeros.RawRuleKey(rr.Family, rr.Comment))
			}
		}
		if row.Count > 0 || row.Count6 > 0 {
			row.State = blocklistStateHeld
		}
		if standing == blocklistStandingBelowFloor {
			row.State = blocklistStateBelowFloor
		}
		s.blocklistActivity(device, l, &row, now)
		rows = append(rows, row)
	}
	return rows
}

// blocklistCommandsRequest is POST /api/blocklist/builder/commands' body.
// No version field: the version is the router's own (see the file
// comment), and an unknown field is refused outright rather than
// ignored.
type blocklistCommandsRequest struct {
	Device  string                     `json:"device"`
	Address string                     `json:"address"`
	Token   string                     `json:"token"`
	Lists   []routeros.BlocklistChoice `json:"lists"`
}

type blocklistCommandsResponse struct {
	Parts    []routeros.Part `json:"parts"`
	CopyText string          `json:"copyText"`
	// Blocked names why the block, or its part 1, is missing:
	// "no-push" or "below-floor" (no block at all), "no-address" or
	// "no-token" (the lists' parts without the push re-set).
	Blocked []string `json:"blocked,omitempty"`
}

// maxBlocklistChoices bounds the lists a request may carry: one per
// catalogue entry. A longer list is a client bug or a probe.
var maxBlocklistChoices = len(blcatalogue.Lists())

// validateBlocklistCommandsRequest is #1095's rule for this route: every
// value that reaches a RouterOS command is either an enum the generator
// checks against the catalogue, or goes through the same validators the
// setup wizard's commands use.
func validateBlocklistCommandsRequest(req blocklistCommandsRequest) error {
	if req.Device == "" || !validSetupDevice(req.Device) {
		return errors.New("device must be 1 to 64 characters from letters, digits, '.', '_' and '-'")
	}
	if req.Address != "" && !validSetupAddress(req.Address) {
		return errors.New("address must be empty, a hostname, or an IP address, optionally with :port")
	}
	if !validSetupToken(req.Token) {
		return errors.New("token must be letters, digits, '_' or '-', up to 256 characters")
	}
	if len(req.Lists) > maxBlocklistChoices {
		return errors.New("more lists than the catalogue holds")
	}
	for _, c := range req.Lists {
		if _, ok := blcatalogue.Lookup(c.Key); !ok {
			return errors.New("a list that is not in the catalogue")
		}
	}
	return nil
}

// handleBlocklistCommands is POST /api/blocklist/builder/commands: the
// block for the lists the operator switched on, re-rendered on every
// click.
func (s *Server) handleBlocklistCommands(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	var req blocklistCommandsRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Address == "" && s.Setup != nil {
		req.Address = s.Setup.Address()
	}
	if err := validateBlocklistCommandsRequest(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	version, _, pushed := s.blocklistVersion(req.Device)
	if !pushed {
		writeJSON(w, http.StatusOK, blocklistCommandsResponse{Parts: []routeros.Part{}, Blocked: []string{blocklistStandingNoPush}})
		return
	}
	if !routeros.AtFloor(version) {
		writeJSON(w, http.StatusOK, blocklistCommandsResponse{Parts: []routeros.Part{}, Blocked: []string{blocklistStandingBelowFloor}})
		return
	}

	breq := routeros.BlocklistRequest{
		Device:          req.Device,
		RouterOSVersion: version,
		Lists:           req.Lists,
		OnRouter:        map[string]bool{},
		HeldIPv6:        map[string]bool{},
	}
	var blocked []string
	switch {
	case req.Address == "":
		blocked = append(blocked, "no-address")
	case req.Token == "":
		blocked = append(blocked, "no-token")
	default:
		breq.Push = routeros.BlocklistPush{Address: req.Address, Token: req.Token, Kinds: s.blocklistPushedKinds(req.Device)}
	}
	if s.RouterState != nil {
		if rules, _, ok := s.RouterState.RawRules(req.Device); ok {
			for _, rr := range rules {
				breq.OnRouter[routeros.RawRuleKey(rr.Family, rr.Comment)] = true
			}
		}
		if counts, _, ok := s.RouterState.AddressListCounts(req.Device); ok {
			for _, c := range counts {
				if c.Family != ingest.FamilyIPv6 || c.Count <= 0 {
					continue
				}
				for _, l := range blcatalogue.Lists() {
					if l.IPv6 && c.List == blcatalogue.ListName6(l.Key) {
						breq.HeldIPv6[l.Key] = true
					}
				}
			}
		}
	}

	block, err := routeros.BlocklistBlock(breq)
	if err != nil {
		// Every error here is the request's: a choice the list does not
		// offer on this version, or a list chosen twice. Fixed wording,
		// so nothing the caller sent is echoed back.
		http.Error(w, "a choice this list does not offer on this router", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, blocklistCommandsResponse{Parts: block.Parts, CopyText: block.CopyText(), Blocked: blocked})
}

// blocklistPushedKinds is the kinds device pushes today, sorted, which
// part 1 re-renders the push script with (plus the builder's two).
func (s *Server) blocklistPushedKinds(device string) []string {
	if s.RouterState == nil {
		return nil
	}
	var kinds []string
	for k := range s.RouterState.PushedKinds(device) {
		kinds = append(kinds, string(k))
	}
	sort.Strings(kinds)
	return kinds
}
