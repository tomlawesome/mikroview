// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	// A route-table entry: {http.MethodGet, "/api/x", handler} or {"GET", "/api/x", ...}.
	tableRouteRE = regexp.MustCompile(`\{\s*(?:http\.Method([A-Za-z]+)|"([A-Z]+)")\s*,\s*"(/[^"]*)"`)
	// A direct registration: mux.Handle("POST /api/x", ...) or HandleFunc("/x", ...).
	muxRouteRE = regexp.MustCompile(`\.Handle(?:Func)?\(\s*"(?:([A-Z]+) )?(/[^"]*)"`)
	// A row of the API reference table.
	docRouteRE = regexp.MustCompile("^\\|\\s*`(?:([A-Z]+) )?(/[^`\\s]*)`")
	wildcardRE = regexp.MustCompile(`\{[^}]*\}`)
)

// normaliseRoute reduces a Go 1.22 pattern to a comparable form: a
// trailing "{$}" is dropped and every wildcard ({id}, {ref}, {rest...})
// becomes "{}", so the docs may name a segment differently from the code.
func normaliseRoute(method, path string) string {
	path = strings.TrimSuffix(path, "{$}")
	path = wildcardRE.ReplaceAllString(path, "{}")
	if method == "" {
		return path
	}
	return method + " " + path
}

// TestEveryRegisteredRouteIsInTheAPIReference covers #1499: the "## API
// reference" table in docs/configuration.md must name every route the
// server registers. Routes come from the route tables (server.go,
// configeditor.go -- test hooks are excluded, a shipped image never
// serves them) and from the method-qualified direct registrations in
// gauntlet.go. Pattern-only registrations (the gauntlet-owned /api/auth/
// and /api/tokens mounts) are gauntlet's and are not required.
func TestEveryRegisteredRouteIsInTheAPIReference(t *testing.T) {
	registered := map[string]string{} // normalised -> as written in source
	for _, file := range []string{"server.go", "configeditor.go", "setuponly.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		for _, m := range tableRouteRE.FindAllStringSubmatch(string(src), -1) {
			method := strings.ToUpper(m[1])
			if method == "" {
				method = m[2]
			}
			registered[normaliseRoute(method, m[3])] = method + " " + m[3]
		}
	}
	gsrc, err := os.ReadFile("gauntlet.go")
	if err != nil {
		t.Fatalf("reading gauntlet.go: %v", err)
	}
	for _, m := range muxRouteRE.FindAllStringSubmatch(string(gsrc), -1) {
		if m[1] == "" {
			continue // pattern-only mounts belong to gauntlet
		}
		registered[normaliseRoute(m[1], m[2])] = m[1] + " " + m[2]
	}
	if len(registered) < 50 {
		t.Fatalf("found only %d registered routes -- this test is not looking where it thinks it is", len(registered))
	}

	docs, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatalf("reading the configuration reference: %v", err)
	}
	var section []string
	in := false
	for _, line := range strings.Split(string(docs), "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.TrimSpace(line) == "## API reference"
			continue
		}
		if in {
			section = append(section, line)
		}
	}
	documented := map[string]bool{}
	for _, line := range section {
		if m := docRouteRE.FindStringSubmatch(line); m != nil {
			documented[normaliseRoute(m[1], m[2])] = true
			documented[normaliseRoute("", m[2])] = true
		}
	}
	if len(documented) < 50 {
		t.Fatalf("found only %d routes in the \"## API reference\" section of docs/configuration.md -- this test is not looking where it thinks it is", len(documented))
	}

	var missing []string
	for norm, written := range registered {
		// A pattern-only entry is matched by path alone; a method-qualified
		// one needs its own method.
		if !documented[norm] {
			missing = append(missing, written)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d registered route(s) are absent from the API reference in docs/configuration.md:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}
