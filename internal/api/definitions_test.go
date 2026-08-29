// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file is issue #407's test migration: watchlist_test.go drove
// /api/watchlist/entries and /api/detectors (detector_settings_test.go),
// the two surfaces this replaced. Both are gone with no compatibility
// route, so every test here drives /api/definitions instead -- the
// behaviour each old test pinned is preserved; only the wire shape
// changed.
//
// TestHandleDetectorSettingsRequiresAdminOnceAccountExists is
// deliberately NOT migrated: TestAuthorizationMatrixIsEnforced
// (authz_matrix_test.go) already drives every /api/definitions route
// with a real anonymous/user/admin session and asserts the same thing,
// generically, for every route in the table -- re-deriving it here by
// hand for one route would just be a second, narrower copy of that
// guard.

// definitionsListBody is the decode target for GET /api/definitions --
// shared with definitions_coverage_test.go, which reads the same
// response shape.
type definitionsListBody struct {
	Definitions      []definitionView `json:"definitions"`
	CoverageEvidence coverageEvidence `json:"coverageEvidence"`
}

// getDefinitions fetches and decodes GET /api/definitions.
func getDefinitions(t *testing.T, ts *httptest.Server) definitionsListBody {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/definitions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/definitions: expected 200, got %d", resp.StatusCode)
	}
	var body definitionsListBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

// findDefinition locates one view by id in a list response, the same way
// a caller filtering the one list handleDefinitionsList serves would.
func findDefinition(views []definitionView, id string) (definitionView, bool) {
	for _, v := range views {
		if v.ID == id {
			return v, true
		}
	}
	return definitionView{}, false
}

// mustDecodeDefinition decodes one definitionView from a response body,
// closing it afterward.
func mustDecodeDefinition(t *testing.T, resp *http.Response) definitionView {
	t.Helper()
	defer resp.Body.Close()
	var v definitionView
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// boolPtr builds a *bool inline -- updateDefinitionRequest.Enabled is a
// pointer so an absent field means "leave this alone" (see that type's
// own doc comment), which a composite literal cannot express by taking
// the address of a literal directly.
func boolPtr(b bool) *bool { return &b }

func TestHandleDefinitionsCreateGeneratesAnID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := createDefinitionRequest{Name: "SSH watch", Expectation: &expectationRequest{Ports: []int{22}}}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", req)
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	got := mustDecodeDefinition(t, resp)
	if got.ID == "" {
		t.Error("expected a server-generated ID, got empty")
	}
	if got.Name != "SSH watch" || got.Expectation == nil || len(got.Expectation.Ports) != 1 || got.Expectation.Ports[0] != 22 {
		t.Errorf("unexpected definition: %+v", got)
	}
	if got.Expectation.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	// Actually persisted, not just echoed back.
	stored, ok, err := s.Definitions.GetExpectation(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("entry was not persisted to the store")
	}
	if stored.Name != "SSH watch" {
		t.Errorf("stored entry = %+v, want Name=SSH watch", stored)
	}
}

// A new inverted entry must start Observing -- #243 section 5's rule,
// not an operator-settable request field.
func TestHandleDefinitionsCreateInvertedStartsObserving(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := createDefinitionRequest{
		Name: "IoT camera",
		Expectation: &expectationRequest{
			Invert: true,
			Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
		},
	}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", req)
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	got := mustDecodeDefinition(t, resp)
	if got.Expectation == nil || !got.Expectation.Observing {
		t.Error("a new inverted entry must start Observing")
	}
}

func TestHandleDefinitionsCreateRejectsInvalidEntry(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	// No ports, not inverted -- watchlist.ErrNoPorts.
	req := createDefinitionRequest{Name: "broken", Expectation: &expectationRequest{}}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for an entry with no ports, got %d", resp.StatusCode)
	}
}

// kind=programmatic can only ever be a shipped definition's -- only
// mikroview's own Go can supply programmatic logic, so no request shape
// may express a custom one (#401). See errProgrammaticIsShippedOnly.
func TestHandleDefinitionsCreateRejectsProgrammaticKind(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := createDefinitionRequest{
		Name: "nope", Kind: engine.KindProgrammatic,
		Expectation: &expectationRequest{Ports: []int{22}},
	}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for kind=programmatic, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), errProgrammaticIsShippedOnly) {
		t.Errorf("expected the refusal to state its reason, got %q", body)
	}
}

// A custom detection is created from its conditions and the aggregation
// around them, and comes back carrying both -- the whole point of #502.
func TestHandleDefinitionsCreateCustomDetection(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := createDefinitionRequest{
		Name:   "SSH hammering",
		Intent: engine.IntentDetection,
		Detection: &detectionRequest{
			Conditions: []engine.Condition{
				{Field: engine.FieldDestinationPort, Operator: engine.OpEquals, Values: []string{"22"}},
			},
			Key:            engine.KeyPerSource,
			Counting:       engine.CountingTotal,
			DetailTemplate: "{Count} attempts against port 22 from {SourceAddress}",
			Threshold:      5,
			Window:         "60s",
		},
	}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", req)
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	got := mustDecodeDefinition(t, resp)

	if got.Intent != engine.IntentDetection || got.Kind != engine.KindDeclarative {
		t.Errorf("intent/kind = %q/%q, want detection/declarative", got.Intent, got.Kind)
	}
	if got.Provenance.Origin != engine.ProvenanceCustom {
		t.Errorf("provenance = %q, want custom", got.Provenance.Origin)
	}
	if got.Detection == nil {
		t.Fatal("the created detection came back without its detection block")
	}
	if len(got.Detection.Conditions) != 1 || got.Detection.Conditions[0].Field != engine.FieldDestinationPort {
		t.Errorf("conditions = %+v, want the one destination-port condition", got.Detection.Conditions)
	}
	if got.Detection.Key != engine.KeyPerSource || got.Detection.Counting != engine.CountingTotal {
		t.Errorf("aggregation = %q/%q, want perSource/total", got.Detection.Key, got.Detection.Counting)
	}

	// Structure in the block, tunables in Params -- the split #502
	// ratified. Threshold and window must be reachable by the same
	// params editor that tunes every shipped detector, not by a second
	// editing path of their own.
	if got.Params["threshold"] != float64(5) {
		t.Errorf("threshold param = %v (%T), want 5", got.Params["threshold"], got.Params["threshold"])
	}
	if got.Params["window"] != "60s" {
		t.Errorf("window param = %v, want 60s", got.Params["window"])
	}
	if !slices.ContainsFunc(got.ParamSchema, func(p engine.ParamSchema) bool { return p.Name == "threshold" }) {
		t.Errorf("a custom detection must declare its own param schema, got %+v", got.ParamSchema)
	}

	// Replayability comes free for a declarative definition -- but only
	// if the inspection path can build this one.
	if !got.Replay.Known || !got.Replay.Capable {
		t.Errorf("replay = %+v, want a custom declarative detection to be replay-capable", got.Replay)
	}

	// Narrowable on destination port, so it is not in the
	// always-consulted bucket and must not claim to be.
	if got.Dispatch == nil || got.Dispatch.AlwaysConsulted {
		t.Errorf("dispatch = %+v, want a detector narrowed by its destination-port condition", got.Dispatch)
	}

	// Persisted and buildable, not merely echoed: an unbuildable stored
	// detection reports Available=false.
	stored, ok := s.Definitions.Get(got.ID)
	if !ok {
		t.Fatal("the detection was not persisted to the store")
	}
	if !stored.Available {
		t.Error("the stored detection is not available, so nothing would ever evaluate it")
	}
}

// A detector the operator wrote is theirs to rename, in place. The
// alternative -- delete and re-create -- discards the id every flag it
// already raised points at (#612).
func TestHandleDefinitionsUpdateRenamesACustomDetection(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := createDefinitionRequest{
		Name:   "typoed naem",
		Intent: engine.IntentDetection,
		Detection: &detectionRequest{
			Conditions: []engine.Condition{
				{Field: engine.FieldDestinationPort, Operator: engine.OpEquals, Values: []string{"22"}},
			},
			Key:            engine.KeyPerSource,
			Counting:       engine.CountingTotal,
			DetailTemplate: "{Count} from {SourceAddress}",
			Threshold:      5,
			Window:         "60s",
		},
	}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", req)
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	created := mustDecodeDefinition(t, resp)

	name := "corrected name"
	renamed := putJSON(t, &http.Client{}, ts.URL+"/api/definitions/"+created.ID, updateDefinitionRequest{Name: &name})
	if renamed.StatusCode != http.StatusOK {
		renamed.Body.Close()
		t.Fatalf("expected 200 renaming a custom detection, got %d", renamed.StatusCode)
	}
	got := mustDecodeDefinition(t, renamed)
	if got.Name != name {
		t.Errorf("Name = %q, want %q", got.Name, name)
	}
	if got.ID != created.ID {
		t.Errorf("id changed on rename: %q -> %q", created.ID, got.ID)
	}
	if got.Detection == nil || got.Params["threshold"] != float64(5) {
		t.Errorf("the rename disturbed the detector: detection=%+v params=%+v", got.Detection, got.Params)
	}
}

// A shipped definition still refuses: its name comes from the binary
// that ships the logic, not from the deployment.
func TestHandleDefinitionsUpdateStillRefusesToRenameAShippedDefinition(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	name := "my port scan"
	resp := putJSON(t, &http.Client{}, ts.URL+"/api/definitions/port_scan", updateDefinitionRequest{Name: &name})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 renaming a shipped definition, got %d", resp.StatusCode)
	}
}

// A detector whose conditions give the pre-index nothing to narrow on is
// accepted -- watching one source address is a legitimate question --
// but it is consulted on every event, and the definition says so rather
// than absorbing the cost in silence.
func TestHandleDefinitionsCreateDetectionDisclosesAlwaysConsulted(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := createDefinitionRequest{
		Name:   "one noisy host",
		Intent: engine.IntentDetection,
		Detection: &detectionRequest{
			Conditions: []engine.Condition{
				{Field: engine.FieldSourceAddress, Operator: engine.OpEquals, Values: []string{"198.51.100.7"}},
			},
			Key:            engine.KeyPerSource,
			Counting:       engine.CountingTotal,
			DetailTemplate: "{Count} events from {SourceAddress}",
			Threshold:      3,
			Window:         "30s",
		},
	}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", req)
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("expected 201 -- an un-narrowable detector is disclosed, not refused -- got %d", resp.StatusCode)
	}
	got := mustDecodeDefinition(t, resp)
	if got.Dispatch == nil || !got.Dispatch.AlwaysConsulted {
		t.Fatalf("dispatch = %+v, want alwaysConsulted", got.Dispatch)
	}
	if got.Dispatch.Reason != alwaysConsultedReason {
		t.Errorf("reason = %q, want the stated one", got.Dispatch.Reason)
	}
}

// intent=detection with no detection block has nowhere to carry its
// match conditions, which is the definition-that-evaluates-nothing this
// feature had to avoid creating.
func TestHandleDefinitionsCreateDetectionRequiresItsBlock(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := createDefinitionRequest{Name: "nothing to match on", Intent: engine.IntentDetection}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a detection with no detection block, got %d", resp.StatusCode)
	}
}

// The detail template is operator input rendered into the interface, so
// its placeholders are a closed set checked before anything is stored --
// not at emission time, where the detector would already exist and would
// fail the moment it should have fired.
func TestHandleDefinitionsCreateDetectionRejectsUnresolvableTemplate(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	for _, tc := range []struct {
		name     string
		template string
	}{
		// A custom detection declares no evidence categories, so an
		// evidence token would never resolve.
		{"evidence token", "{Count} attempts against {Ports}"},
		// Supplied by a different key mode than this detector's.
		{"wrong key token", "{Count} attempts against {DestinationAddress}"},
		{"invented token", "{Count} attempts, {Whatever}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(s.Definitions.List())
			req := createDefinitionRequest{
				Name:   "bad template",
				Intent: engine.IntentDetection,
				Detection: &detectionRequest{
					Conditions: []engine.Condition{
						{Field: engine.FieldDestinationPort, Operator: engine.OpEquals, Values: []string{"22"}},
					},
					Key:            engine.KeyPerSource,
					Counting:       engine.CountingTotal,
					DetailTemplate: tc.template,
					Threshold:      5,
					Window:         "60s",
				},
			}
			resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", req)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400 for template %q, got %d", tc.template, resp.StatusCode)
			}
			if after := len(s.Definitions.List()); after != before {
				t.Errorf("a refused template must leave nothing behind in the store (%d definitions before, %d after)", before, after)
			}
		})
	}
}

func TestHandleDefinitionsListReturnsCreatedEntries(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/definitions", createDefinitionRequest{Name: "e1", Expectation: &expectationRequest{Ports: []int{22}}}).Body.Close()
	postJSON(t, &http.Client{}, ts.URL+"/api/definitions", createDefinitionRequest{Name: "e2", Expectation: &expectationRequest{Ports: []int{443}}}).Body.Close()

	body := getDefinitions(t, ts)
	var names []string
	for _, v := range body.Definitions {
		if v.Intent == engine.IntentExpectation {
			names = append(names, v.Name)
		}
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 expectation definitions, got %d: %v", len(names), names)
	}
}

// TestShippedDefinitionsDefaultToEnabled is the /api/definitions
// replacement for detector_settings_test.go's own list-defaults test:
// the whole shipped catalogue -- not just the twelve
// LegacyDetectorIDs the old /api/detectors endpoint exposed -- is
// enabled out of the box (engine.SeedShippedDefinitions).
func TestShippedDefinitionsDefaultToEnabled(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	body := getDefinitions(t, ts)
	shipped := 0
	for _, id := range engine.ShippedDefinitionIDs() {
		v, ok := findDefinition(body.Definitions, id)
		if !ok {
			t.Errorf("expected shipped definition %q to be listed", id)
			continue
		}
		shipped++
		if !v.Enabled {
			t.Errorf("expected %s to default to enabled, got disabled", id)
		}
	}
	if shipped != len(engine.ShippedDefinitionIDs()) {
		t.Fatalf("expected every shipped definition to be listed, found %d of %d", shipped, len(engine.ShippedDefinitionIDs()))
	}
}

// TestShippedDefinitionUpdateThenGetReflectsIt is the /api/definitions
// replacement for detector_settings_test.go's update-then-list test:
// PUT with enabled/scope on a shipped id is the door
// SetEnabledAndScope opens onto it (#405/#407).
func TestShippedDefinitionUpdateThenGetReflectsIt(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := updateDefinitionRequest{
		Enabled: boolPtr(false),
		Scope: &engine.Scope{
			Hosts:     []string{"203.0.113.0/24"},
			HostsMode: engine.ListModeDeny,
		},
	}
	resp := putJSON(t, &http.Client{}, ts.URL+"/api/definitions/rule_spike", req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	stored, ok := s.Definitions.Get("rule_spike")
	if !ok {
		t.Fatal("expected the rule_spike definition to exist")
	}
	if stored.Definition.Enabled {
		t.Error("expected rule_spike to now be disabled")
	}
	if got := stored.Definition.Scope; len(got.Hosts) != 1 || got.Hosts[0] != "203.0.113.0/24" {
		t.Errorf("expected the host scope to be stored, got %+v", got)
	}
}

// TestConcurrentEnabledOnlyUpdateDoesNotRevertAConcurrentScopeChange pins
// issue #494: handleDefinitionsUpdate used to fill in whichever of
// Enabled/Scope a request left unset from a snapshot taken *before*
// decodeJSONBody's client-paced body read, so an enabled-only request
// that was mid-flight while a scope-only request landed would write the
// enabled-only request's stale, pre-decode Scope back over the concurrent
// change -- silently reverting it.
//
// The two requests are driven straight at the handler (asAdmin(s.mux()),
// no real network) so the interleaving is deterministic rather than
// timing-dependent: request A's body is an io.Pipe that yields
// `{"enabled":` and then blocks. A pipe Write only returns once a Read
// has consumed it, and decodeJSONBody's json.Decoder only calls Read
// after handleDefinitionsUpdate's pre-decode snapshot has already been
// taken -- so the moment the goroutine below observes that first Write
// return, A is certainly past that snapshot and blocked inside decode,
// with its snapshot fixed. Request B (a complete, ordinary scope-only
// request) is then run to completion synchronously, and only afterwards
// is A released to finish decoding and write. That fixes the
// interleaving the issue describes without any sleep.
//
// Fails against the pre-fix handler: A's write lands after B's and
// carries A's snapshot's Scope (the zero value, taken before B ran),
// clobbering the Deny/203.0.113.0/24 scope B just set. Passes once
// handleDefinitionsUpdate re-reads fresh state under a lock spanning the
// read and the write (definitionsEnabledScopeMu), because that fresh
// read -- taken only after A's body is fully decoded and its turn to
// write has come -- observes B's scope rather than the pre-decode one.
func TestConcurrentEnabledOnlyUpdateDoesNotRevertAConcurrentScopeChange(t *testing.T) {
	s, _ := newTestServer(t)
	handler := asAdmin(s.mux())
	const id = "rule_spike"

	pr, pw := io.Pipe()
	firstChunkRead := make(chan struct{})
	release := make(chan struct{})
	writeErrCh := make(chan error, 1)
	go func() {
		if _, err := pw.Write([]byte(`{"enabled":`)); err != nil {
			writeErrCh <- err
			return
		}
		close(firstChunkRead)
		<-release
		if _, err := pw.Write([]byte(`false}`)); err != nil {
			writeErrCh <- err
			return
		}
		writeErrCh <- pw.Close()
	}()

	reqA := httptest.NewRequest(http.MethodPut, "/api/definitions/"+id, pr)
	reqA.Header.Set("Content-Type", "application/json")
	reqA.Header.Set(csrfHeaderName, csrfHeaderValue)
	recA := httptest.NewRecorder()
	doneA := make(chan struct{})
	go func() {
		handler.ServeHTTP(recA, reqA)
		close(doneA)
	}()

	select {
	case <-firstChunkRead:
	case <-doneA:
		t.Fatal("request A finished before its body was even half-sent")
	}

	wantScope := engine.Scope{Hosts: []string{"203.0.113.0/24"}, HostsMode: engine.ListModeDeny}
	bodyB, err := json.Marshal(updateDefinitionRequest{Scope: &wantScope})
	if err != nil {
		t.Fatal(err)
	}
	reqB := httptest.NewRequest(http.MethodPut, "/api/definitions/"+id, bytes.NewReader(bodyB))
	reqB.Header.Set("Content-Type", "application/json")
	reqB.Header.Set(csrfHeaderName, csrfHeaderValue)
	recB := httptest.NewRecorder()
	handler.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("request B (scope-only): expected 200, got %d: %s", recB.Code, recB.Body.String())
	}

	close(release)
	<-doneA
	if err := <-writeErrCh; err != nil {
		t.Fatalf("writing request A's body: %v", err)
	}
	if recA.Code != http.StatusOK {
		t.Fatalf("request A (enabled-only): expected 200, got %d: %s", recA.Code, recA.Body.String())
	}

	stored, ok := s.Definitions.Get(id)
	if !ok {
		t.Fatal("expected the definition to still exist")
	}
	if stored.Definition.Enabled {
		t.Error("expected rule_spike to be disabled after request A")
	}
	if got := stored.Definition.Scope; len(got.Hosts) != 1 || got.Hosts[0] != "203.0.113.0/24" || got.HostsMode != engine.ListModeDeny {
		t.Errorf("request B's concurrent scope change was reverted by request A's stale snapshot: got %+v, want %+v", got, wantScope)
	}
}

func TestShippedDefinitionUpdateUnknownID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/definitions/not_a_real_definition", updateDefinitionRequest{Enabled: boolPtr(true)})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown definition id, got %d", resp.StatusCode)
	}
}

func TestShippedDefinitionUpdateInvalidScopeMode(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := updateDefinitionRequest{
		Enabled: boolPtr(true),
		Scope:   &engine.Scope{HostsMode: engine.ListMode("maybe")},
	}
	resp := putJSON(t, &http.Client{}, ts.URL+"/api/definitions/port_scan", req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for an invalid hostsMode, got %d", resp.StatusCode)
	}
}

// TestHandleDefinitionsUpdateParamOverrideOutOfBoundsIsRejected is
// #407's new contract on PUT: a param outside its own definition's
// declared ParamSchema bounds is a 400, and the bad value is never
// stored -- engine.DefinitionsStore.SetParams validates on the way in,
// so a rejected value never reaches the document (see
// handleDefinitionsUpdate's own doc comment). Before #407 an
// out-of-range detector setting could only be caught by whatever read
// the zero value back later; this is the boundary that now refuses it
// outright.
func TestHandleDefinitionsUpdateParamOverrideOutOfBoundsIsRejected(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	before, ok := s.Definitions.Get("rule_spike")
	if !ok {
		t.Fatal("expected rule_spike to exist")
	}
	originalMultiplier := before.Definition.Params["multiplier"]

	// Copy every current param, then push multiplier below its declared
	// Min(0) -- everything else stays valid, so the rejection is
	// specifically the bound, not a missing required field.
	badParams := engine.Params{}
	for k, v := range before.Definition.Params {
		badParams[k] = v
	}
	badParams["multiplier"] = -1.0

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/definitions/rule_spike", updateDefinitionRequest{Params: badParams})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a param below its schema's Min, got %d", resp.StatusCode)
	}

	after, ok := s.Definitions.Get("rule_spike")
	if !ok {
		t.Fatal("expected rule_spike to still exist")
	}
	if after.Definition.Params["multiplier"] != originalMultiplier {
		t.Errorf("a rejected param override must not be stored: multiplier = %v, want unchanged %v",
			after.Definition.Params["multiplier"], originalMultiplier)
	}
	if dist := after.Definition.Distance(); len(dist) != 0 {
		t.Errorf("expected no distance from stock after a rejected override, got %+v", dist)
	}
}

// TestHandleDefinitionsResetPutsShippedParamsBackToStock covers POST
// /api/definitions/{id}/reset: clearing every override and "resetting to
// default" are the same state (engine.Definition.Distance), not two
// operations that could fall out of sync.
func TestHandleDefinitionsResetPutsShippedParamsBackToStock(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	before, ok := s.Definitions.Get("rule_spike")
	if !ok {
		t.Fatal("expected rule_spike to exist")
	}
	originalMultiplier, ok := before.Definition.Params["multiplier"].(float64)
	if !ok {
		t.Fatalf("expected multiplier to be a float64, got %T", before.Definition.Params["multiplier"])
	}

	overridden := engine.Params{}
	for k, v := range before.Definition.Params {
		overridden[k] = v
	}
	overridden["multiplier"] = originalMultiplier + 1

	putJSON(t, &http.Client{}, ts.URL+"/api/definitions/rule_spike", updateDefinitionRequest{Params: overridden}).Body.Close()
	mid, _ := s.Definitions.Get("rule_spike")
	if dist := mid.Definition.Distance(); len(dist) == 0 {
		t.Fatal("setup failed: expected the override to register as a distance from stock")
	}

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions/rule_spike/reset", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	after, _ := s.Definitions.Get("rule_spike")
	if dist := after.Definition.Distance(); len(dist) != 0 {
		t.Errorf("expected reset to clear every override, got distance %+v", dist)
	}
	if got := after.Definition.Params["multiplier"]; got != originalMultiplier {
		t.Errorf("expected multiplier back to %v after reset, got %v", originalMultiplier, got)
	}
}

func TestHandleDefinitionsUpdate(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	created := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", createDefinitionRequest{Name: "before", Expectation: &expectationRequest{Ports: []int{22}}})
	entry := mustDecodeDefinition(t, created)

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/definitions/"+entry.ID, updateDefinitionRequest{
		Expectation: &expectationRequest{Ports: []int{22, 2222}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got, _, err := s.Definitions.GetExpectation(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Ports) != 2 {
		t.Errorf("update did not apply: %+v", got)
	}
	if !got.CreatedAt.Equal(entry.Expectation.CreatedAt) {
		t.Error("update must not change CreatedAt")
	}
}

func TestHandleDefinitionsUpdateUnknownID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/definitions/never-existed", updateDefinitionRequest{Expectation: &expectationRequest{Ports: []int{22}}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// Switching Invert true->false must clear the observe/promote state --
// it no longer means anything for a non-inverted entry.
func TestHandleDefinitionsUpdateClearsStateWhenUninverted(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	entry := mustCreateInvertedDefinition(t, ts)
	s.Definitions.RecordObservation(entry.ID, "1.2.3.4", 443, entry.Expectation.CreatedAt)
	if e, _, _ := s.Definitions.GetExpectation(entry.ID); len(e.Observed) != 1 {
		t.Fatal("setup failed: expected an observation to exist before the update")
	}

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/definitions/"+entry.ID, updateDefinitionRequest{
		Expectation: &expectationRequest{Invert: false, Ports: []int{22}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got, _, _ := s.Definitions.GetExpectation(entry.ID)
	if got.Observing || len(got.Observed) != 0 || len(got.Permitted) != 0 {
		t.Errorf("expected Observing/Observed/Permitted cleared after un-inverting, got %+v", got)
	}
}

// Switching Invert false->true must start Observing, the same rule
// Create applies -- there is no meaningful permitted set yet.
func TestHandleDefinitionsUpdateStartsObservingWhenInverted(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	created := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", createDefinitionRequest{Name: "e", Expectation: &expectationRequest{Ports: []int{22}}})
	entry := mustDecodeDefinition(t, created)

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/definitions/"+entry.ID, updateDefinitionRequest{
		Expectation: &expectationRequest{Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got, _, _ := s.Definitions.GetExpectation(entry.ID)
	if !got.Observing {
		t.Error("expected Observing=true after switching to inverted")
	}
}

func TestHandleDefinitionsDelete(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	created := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", createDefinitionRequest{Name: "e", Expectation: &expectationRequest{Ports: []int{22}}})
	entry := mustDecodeDefinition(t, created)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/definitions/"+entry.ID, nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if _, ok := s.Definitions.Get(entry.ID); ok {
		t.Error("entry still present after delete")
	}
}

func TestHandleDefinitionsDeleteUnknownID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/definitions/never-existed", nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// TestHandleDefinitionsDeleteRefusesShipped pins #401's ratified
// invariant at the API boundary: "shipped definitions are disabled,
// never deleted." A DELETE against one is a 409 carrying that reason,
// and the definition stays exactly as it was -- listed, and enabled
// unless an operator had already disabled it.
func TestHandleDefinitionsDeleteRefusesShipped(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	before, ok := s.Definitions.Get("port_scan")
	if !ok || !before.Definition.Enabled {
		t.Fatal("setup failed: expected port_scan to exist and be enabled")
	}

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/definitions/port_scan", nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for deleting a shipped definition, got %d", resp.StatusCode)
	}
	reason, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(reason), "shipped definition") || !strings.Contains(string(reason), "never deleted") {
		t.Errorf("expected the refusal to state its reason, got %q", reason)
	}

	after, ok := s.Definitions.Get("port_scan")
	if !ok {
		t.Fatal("expected port_scan to still exist after the refused delete")
	}
	if !after.Definition.Enabled {
		t.Error("expected port_scan to remain enabled, unchanged by the refused delete")
	}

	list := getDefinitions(t, ts)
	if _, ok := findDefinition(list.Definitions, "port_scan"); !ok {
		t.Error("expected port_scan to still be listed after the refused delete")
	}
}

// A PUT that omits Permitted/Observed (expectationRequest has no such
// fields at all) must not be able to wipe an entry's accumulated
// observations -- the whole reason those fields aren't in the request
// type.
func TestHandleDefinitionsUpdateDoesNotClearObservedWhenStayingInverted(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	entry := mustCreateInvertedDefinition(t, ts)
	s.Definitions.RecordObservation(entry.ID, "1.2.3.4", 443, entry.Expectation.CreatedAt)

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/definitions/"+entry.ID, updateDefinitionRequest{
		Expectation: &expectationRequest{Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got, _, _ := s.Definitions.GetExpectation(entry.ID)
	if len(got.Observed) != 1 {
		t.Errorf("expected the observation to survive an update that keeps Invert=true, got %+v", got.Observed)
	}
}

// --- Clone --------------------------------------------------------------

// TestHandleDefinitionsCloneExpectation covers cloning an
// operator-authored expectation: data all the way down, so a clone is a
// second, independently editable copy that starts with no observations
// of its own.
func TestHandleDefinitionsCloneExpectation(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	entry := mustCreateInvertedDefinition(t, ts)
	s.Definitions.RecordObservation(entry.ID, "1.2.3.4", 443, entry.Expectation.CreatedAt)

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions/"+entry.ID+"/clone", cloneRequest{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	clone := mustDecodeDefinition(t, resp)
	if clone.ID == entry.ID || clone.ID == "" {
		t.Errorf("expected a fresh id, got %q (original %q)", clone.ID, entry.ID)
	}
	if clone.Name != entry.Name+" (copy)" {
		t.Errorf("expected the default copy suffix, got %q", clone.Name)
	}
	if clone.Expectation == nil || len(clone.Expectation.Observed) != 0 {
		t.Errorf("expected a clone to start with no observations of its own, got %+v", clone.Expectation)
	}

	if _, ok := s.Definitions.Get(entry.ID); !ok {
		t.Error("the original must still exist after cloning it")
	}
}

// TestHandleDefinitionsCloneRefusesShipped pins the other half of #407's
// clone contract: a shipped detection definition's logic is Go keyed by
// its own id, so a "clone" would be an envelope evaluating nothing.
// Refused with the reason, not silently accepted.
func TestHandleDefinitionsCloneRefusesShipped(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions/port_scan/clone", cloneRequest{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for cloning a shipped detection definition, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "cannot be cloned") {
		t.Errorf("expected the refusal to state its reason, got %q", body)
	}
}

// --- Schema ---------------------------------------------------------------

// TestHandleDefinitionsSchema covers GET /api/definitions/schema: every
// param schema a definition declares, keyed by id, so a builder UI can
// render tuning controls without re-listing every definition's knobs in
// TypeScript (docs/decisions/evaluation-engine.md section 4).
func TestHandleDefinitionsSchema(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/definitions/schema")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Schemas map[string][]engine.ParamSchema `json:"schemas"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	schema, ok := body.Schemas["rule_spike"]
	if !ok {
		t.Fatal("expected rule_spike's schema to be present")
	}
	if len(schema) != len(engine.RuleSpikeParamSchema) {
		t.Errorf("expected %d params for rule_spike, got %d", len(engine.RuleSpikeParamSchema), len(schema))
	}
}

// --- Promote / SetObserving ------------------------------------------

func mustCreateInvertedDefinition(t *testing.T, ts *httptest.Server) definitionView {
	t.Helper()
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", createDefinitionRequest{
		Name: "cam",
		Expectation: &expectationRequest{
			Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
		},
	})
	return mustDecodeDefinition(t, resp)
}

func TestHandleDefinitionsPromote(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	entry := mustCreateInvertedDefinition(t, ts)
	s.Definitions.RecordObservation(entry.ID, "10.0.0.5", 8883, entry.Expectation.CreatedAt)

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions/"+entry.ID+"/promote",
		promoteRequest{Destinations: []watchlist.PermittedDest{{DestIP: "10.0.0.5", Port: 8883}}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got, _, _ := s.Definitions.GetExpectation(entry.ID)
	if len(got.Permitted) != 1 || got.Permitted[0].DestIP != "10.0.0.5" {
		t.Errorf("Permitted = %+v, want the promoted pair", got.Permitted)
	}
	if len(got.Observed) != 0 {
		t.Errorf("Observed = %+v, want the promoted pair removed from the review list", got.Observed)
	}
}

func TestHandleDefinitionsPromoteUnknownID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions/never-existed/promote", promoteRequest{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleDefinitionsPromoteNonInverted(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	created := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", createDefinitionRequest{Name: "e", Expectation: &expectationRequest{Ports: []int{22}}})
	entry := mustDecodeDefinition(t, created)

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions/"+entry.ID+"/promote", promoteRequest{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for promoting on a non-inverted entry, got %d", resp.StatusCode)
	}
}

func TestHandleDefinitionsSetObserving(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	entry := mustCreateInvertedDefinition(t, ts) // starts Observing: true

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions/"+entry.ID+"/observing", setObservingRequest{Observing: false})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got, _, _ := s.Definitions.GetExpectation(entry.ID)
	if got.Observing {
		t.Error("expected Observing=false after the request")
	}
}

func TestHandleDefinitionsSetObservingUnknownID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions/never-existed/observing", setObservingRequest{Observing: true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// --- Bearer token boundary (mirrors tokens_test.go's own shape) --------

// A read-only bearer token must never reach definitions CRUD -- creating,
// promoting or observing-toggling a definition is a mutation (and even a
// bare list reveals the deployment's whole watchlist scope), the same
// boundary TestBearerTokenCannotReachWriteEndpoint pins for flags.
func TestBearerTokenCannotReachDefinitions(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := setUpAdmin(t, ts)
	raw := createToken(t, ts, admin, "birdcage")

	resp := bearerGet(t, ts.URL+"/api/definitions", raw)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("expected a read-only bearer token to be unable to reach the definitions surface, got 200")
	}
}

// TestDefinitionsListOpenToViewer pins the viewer-readable settings
// widening (#490) for GET /api/definitions: a signed-in non-admin now
// gets 200, a signed-out caller is still refused, and creating a
// definition -- the write that sits right beside this GET -- stays
// admin-only.
func TestDefinitionsListOpenToViewer(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

	resp, err := viewerClient.Get(ts.URL + "/api/definitions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected a viewer to read the definitions list (#490), got %d", resp.StatusCode)
	}

	anonResp, err := http.Get(ts.URL + "/api/definitions")
	if err != nil {
		t.Fatal(err)
	}
	anonResp.Body.Close()
	if anonResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected a signed-out caller to still be refused, got %d", anonResp.StatusCode)
	}

	writeResp := postJSON(t, viewerClient, ts.URL+"/api/definitions",
		createDefinitionRequest{Name: "should-fail", Expectation: &expectationRequest{Ports: []int{22}}})
	defer writeResp.Body.Close()
	if writeResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected definition creation to stay admin-only, got %d", writeResp.StatusCode)
	}
}
