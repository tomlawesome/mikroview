// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every route readOnlyRoutes registers must be named in the
// configuration reference's bearer-token sentence.
//
// That sentence is where someone assessing the blast radius of a leaked
// token looks, so a route missing from it reads as out of reach when it
// is not. The list has now gone stale twice for the same reason -- #326
// (four listed, five served) and again in #382, where the addition of
// GET /api/matches left the persisted watchlist match log, a record of
// which of the operator's devices touched what, undocumented as
// token-reachable.
//
// Deliberately scoped to this one sentence rather than the whole file:
// a route mentioned anywhere in a 2900-line reference proves nothing
// about the summary an operator actually reads. TokensOverlay.svelte's
// hint text carries the same list and is checked by eye, per the note in
// readOnlyRoutes' own comment -- pinning that too is a bigger harness
// than this defect warrants (#382 records the reasoning).
func TestBearerTokenRouteListMatchesReadOnlyRoutes(t *testing.T) {
	source, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatalf("reading auth.go: %v", err)
	}

	body := regexp.MustCompile(`(?s)func \(s \*Server\) readOnlyRoutes\(\) http\.Handler \{(.*?)\n\}`).FindSubmatch(source)
	if body == nil {
		t.Fatal("found no readOnlyRoutes function in auth.go -- this test is not looking where it thinks it is")
	}

	var registered []string
	for _, m := range regexp.MustCompile(`mux\.HandleFunc\("GET (/[^"]+)"`).FindAllSubmatch(body[1], -1) {
		registered = append(registered, string(m[1]))
	}
	if len(registered) == 0 {
		t.Fatal("found no GET routes inside readOnlyRoutes -- this test is not looking where it thinks it is")
	}

	docs, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatalf("reading the configuration reference: %v", err)
	}

	// The passage that tells the reader which routes accept a bearer
	// token instead of a session. Anchored on its closing claim and read
	// backwards, because the route list is what comes *before* that
	// claim and any regexp reaching forwards from a route name would
	// happily start at the API table hundreds of lines above.
	end := bytes.Index(docs, []byte("no other route accepts one."))
	if end < 0 {
		t.Fatal("found no bearer-token route sentence in docs/configuration.md -- this test is not looking where it thinks it is")
	}
	start := end - 400
	if start < 0 {
		start = 0
	}
	sentence := docs[start:end]

	var missing []string
	for _, route := range registered {
		// Written as `/api/events`, `/flags`, `/stats`, ... -- the
		// shared prefix is stated once, so match on the tail.
		if !strings.Contains(string(sentence), strings.TrimPrefix(route, "/api")) {
			missing = append(missing, route)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d route(s) a bearer API token can reach are absent from the bearer-token sentence in docs/configuration.md, so a leaked token reads as narrower than it is: %s",
			len(missing), strings.Join(missing, ", "))
	}
}
