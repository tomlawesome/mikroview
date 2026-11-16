// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ipscope"
)

// Assumed API (spec gives none): (*Store).SetHomeScope(*ipscope.Scope).
func TestQueryScopeInternalIncludesAConfiguredHomeRange(t *testing.T) {
	s := New(100, time.Hour)
	e := mkEvent(time.Now(), "core", ActionAccept)
	e.SrcIP, e.DstIP = "198.51.100.66", "203.0.113.44"
	s.Insert(e)

	if got := s.Query(Query{SrcScope: ScopeInternal, Limit: 10}); len(got.Events) != 0 {
		t.Fatalf("before a home scope is set, 198.51.100.66 must be external, got %d internal-src events", len(got.Events))
	}

	sc, err := ipscope.New([]string{"198.51.100.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	s.SetHomeScope(sc)

	if got := s.Query(Query{SrcScope: ScopeInternal, Limit: 10}); len(got.Events) != 1 {
		t.Errorf("with home 198.51.100.0/24, a 198.51.100.66 source must query as internal, got %d events", len(got.Events))
	}
	if got := s.Query(Query{SrcScope: ScopeExternal, Limit: 10}); len(got.Events) != 0 {
		t.Errorf("with home 198.51.100.0/24, a 198.51.100.66 source must not query as external, got %d events", len(got.Events))
	}
	if got := s.Query(Query{DstScope: ScopeExternal, Limit: 10}); len(got.Events) != 1 {
		t.Errorf("203.0.113.44 must stay external with the home scope set, got %d events", len(got.Events))
	}
}
