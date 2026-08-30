// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/engine"
)

// This file is issue #639's API-side contract test: the definitions
// surface's "learning" field, byte-for-byte against the wire shape a
// frontend agent is coding against concurrently --
//
//	"learning": {
//	  "floor": { "minDurationSeconds": 1209600, "minSamples": 14 },
//	  "keys": 50,
//	  "ready": 12,
//	  "nearest": { "observedForSeconds": 259200, "samples": 3 }
//	}
//
// -- durations as integer seconds under those exact names, keys/ready
// always present alongside floor, nearest present only for a mixed
// state, and the whole field omitted (not merely null) when there is
// nothing to say.

// fakeLearningSource is a hand-rolled stand-in for Server.Learning's
// narrow interface -- never *engine.Engine, exactly the seam #639's
// design record requires -- so these tests drive the wire shape without
// standing up a real engine.Engine and its baselines.
type fakeLearningSource struct {
	states map[string]engine.LearningState
}

func (f fakeLearningSource) Learning(id string, _ time.Time) (engine.LearningState, bool) {
	st, ok := f.states[id]
	return st, ok
}

// getDefinitionRaw fetches one definition as raw, undecoded JSON bytes --
// deliberately not through definitionView, so a test can assert on the
// literal wire bytes (field names, presence/absence) rather than on
// whatever Go's own JSON decode happens to default a missing field to.
func getDefinitionRaw(t *testing.T, ts *httptest.Server, id string) []byte {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/definitions/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/definitions/%s: expected 200, got %d", id, resp.StatusCode)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// decodeToMap decodes raw into a generic map using json.Number, so an
// integer-seconds assertion below compares exact values rather than
// float64 rounding.
func decodeToMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, raw)
	}
	return m
}

// TestDefinitionsAPILearningOmittedWithNoLiveEngine pins the nil-safe
// default: newTestServer's Server.Learning is nil (no engine.Engine
// wired), which every other admin-only field on this surface already
// tolerates (MatchLog, RouterState, ...) -- the "learning" key must be
// absent entirely, not present as null, so a caller cannot mistake
// silence for an answer.
func TestDefinitionsAPILearningOmittedWithNoLiveEngine(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	m := decodeToMap(t, getDefinitionRaw(t, ts, "rule_spike"))
	if _, present := m["learning"]; present {
		t.Fatalf("expected no \"learning\" key with no live engine wired, got %v", m["learning"])
	}
}

// TestDefinitionsAPILearningOmittedForNoWarmupConcept pins the other
// omission case: a live engine is wired, but this particular definition
// has no warm-up concept (LearningReporter's own ok=false) -- e.g.
// known_bad_ip, a blocklist lookup with no baseline anywhere in it.
func TestDefinitionsAPILearningOmittedForNoWarmupConcept(t *testing.T) {
	s, _ := newTestServer(t)
	s.Learning = fakeLearningSource{states: map[string]engine.LearningState{}}
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	m := decodeToMap(t, getDefinitionRaw(t, ts, "known_bad_ip"))
	if _, present := m["learning"]; present {
		t.Fatalf("expected no \"learning\" key for a definition with no warm-up concept, got %v", m["learning"])
	}
}

// TestDefinitionsAPILearningJSONMatchesContract is issue #639's fixed
// wire contract, verified byte-for-byte: field names, integer seconds
// (not a Go duration string, not nanoseconds), and every value in the
// mixed state (some keys ready, one furthest-along not-yet-ready key).
func TestDefinitionsAPILearningJSONMatchesContract(t *testing.T) {
	s, _ := newTestServer(t)
	s.Learning = fakeLearningSource{states: map[string]engine.LearningState{
		"rule_spike": {
			Floor: engine.BaselineFloor{MinDuration: 14 * 24 * time.Hour, MinSamples: 14},
			Keys:  50,
			Ready: 12,
			Nearest: &engine.LearningProgress{
				ObservedFor: 3 * 24 * time.Hour,
				Samples:     3,
			},
		},
	}}
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	m := decodeToMap(t, getDefinitionRaw(t, ts, "rule_spike"))
	learning, ok := m["learning"].(map[string]any)
	if !ok {
		t.Fatalf("expected a \"learning\" object, got %v (%T)", m["learning"], m["learning"])
	}

	floor, ok := learning["floor"].(map[string]any)
	if !ok {
		t.Fatalf("expected learning.floor to be an object, got %v", learning["floor"])
	}
	assertJSONNumber(t, floor, "minDurationSeconds", "1209600")
	assertJSONNumber(t, floor, "minSamples", "14")
	assertJSONNumber(t, learning, "keys", "50")
	assertJSONNumber(t, learning, "ready", "12")

	nearest, ok := learning["nearest"].(map[string]any)
	if !ok {
		t.Fatalf("expected learning.nearest to be an object, got %v", learning["nearest"])
	}
	assertJSONNumber(t, nearest, "observedForSeconds", "259200")
	assertJSONNumber(t, nearest, "samples", "3")
}

func assertJSONNumber(t *testing.T, obj map[string]any, key, want string) {
	t.Helper()
	got, ok := obj[key]
	if !ok {
		t.Fatalf("expected key %q, was absent (object: %v)", key, obj)
	}
	num, ok := got.(json.Number)
	if !ok {
		t.Fatalf("expected key %q to be a JSON number, got %v (%T) -- not a duration string, not a float", key, got, got)
	}
	if num.String() != want {
		t.Fatalf("%s = %s, want %s", key, num.String(), want)
	}
}

// TestDefinitionsAPILearningNearestOmittedWhenAllReady pins the first of
// the two nearest-omission cases: every observed key is ready. floor/
// keys/ready still appear; nearest does not.
func TestDefinitionsAPILearningNearestOmittedWhenAllReady(t *testing.T) {
	s, _ := newTestServer(t)
	s.Learning = fakeLearningSource{states: map[string]engine.LearningState{
		"rule_spike": {
			Floor: engine.BaselineFloor{MinSamples: 5},
			Keys:  5,
			Ready: 5,
		},
	}}
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	m := decodeToMap(t, getDefinitionRaw(t, ts, "rule_spike"))
	learning, ok := m["learning"].(map[string]any)
	if !ok {
		t.Fatalf("expected a \"learning\" object, got %v", m["learning"])
	}
	if _, present := learning["nearest"]; present {
		t.Fatalf("expected no \"nearest\" key when every observed key is ready, got %v", learning["nearest"])
	}
	assertJSONNumber(t, learning, "keys", "5")
	assertJSONNumber(t, learning, "ready", "5")
}

// TestDefinitionsAPILearningFreshInstallHasFloorWithZeroKeys pins the
// fresh-install case #639 exists for: no traffic seen yet (Keys: 0), but
// the floor is known statically and must still be sent, so an operator
// can be told what is needed before anything has been observed. keys and
// ready are sent as literal 0, never omitted, and nearest is absent (no
// keys observed at all).
func TestDefinitionsAPILearningFreshInstallHasFloorWithZeroKeys(t *testing.T) {
	s, _ := newTestServer(t)
	s.Learning = fakeLearningSource{states: map[string]engine.LearningState{
		"rule_spike": {
			Floor: engine.BaselineFloor{MinDuration: 14 * 24 * time.Hour, MinSamples: 14},
		},
	}}
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	m := decodeToMap(t, getDefinitionRaw(t, ts, "rule_spike"))
	learning, ok := m["learning"].(map[string]any)
	if !ok {
		t.Fatalf("expected a \"learning\" object even with zero keys, got %v", m["learning"])
	}
	if _, present := learning["floor"]; !present {
		t.Fatal("expected \"floor\" to be present with zero keys -- it is known statically")
	}
	assertJSONNumber(t, learning, "keys", "0")
	assertJSONNumber(t, learning, "ready", "0")
	if _, present := learning["nearest"]; present {
		t.Fatalf("expected no \"nearest\" key when no keys have been observed at all, got %v", learning["nearest"])
	}
}

// TestDefinitionsAPILearningListedAlongsideEveryDefinition drives GET
// /api/definitions (the list, not one id) to confirm the same wiring
// applies there -- the handler every operator's Engine Room actually
// calls, not just the single-definition GET the tests above use for
// precision.
func TestDefinitionsAPILearningListedAlongsideEveryDefinition(t *testing.T) {
	s, _ := newTestServer(t)
	s.Learning = fakeLearningSource{states: map[string]engine.LearningState{
		"rule_spike": {Floor: engine.BaselineFloor{MinDuration: 60 * time.Second}, Keys: 2, Ready: 1,
			Nearest: &engine.LearningProgress{ObservedFor: 30 * time.Second}},
	}}
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	body := getDefinitions(t, ts)
	v, ok := findDefinition(body.Definitions, "rule_spike")
	if !ok {
		t.Fatal("rule_spike not found in the definitions list")
	}
	if v.Learning == nil {
		t.Fatal("expected rule_spike's Learning to be populated in the list response")
	}
	if v.Learning.Keys != 2 || v.Learning.Ready != 1 {
		t.Fatalf("Learning = %+v, want Keys=2 Ready=1", v.Learning)
	}

	other, ok := findDefinition(body.Definitions, "known_bad_ip")
	if !ok {
		t.Fatal("known_bad_ip not found in the definitions list")
	}
	if other.Learning != nil {
		t.Fatalf("expected known_bad_ip's Learning to be omitted (no warm-up concept, and not in the fake source's states), got %+v", other.Learning)
	}
}
