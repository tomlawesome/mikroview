// SPDX-License-Identifier: AGPL-3.0-only

package naming

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/entities"
)

func TestResolverReturnsConfiguredNames(t *testing.T) {
	r := Resolver{
		Rules: map[string]string{"r13": "Block known scanners"},
		Hosts: map[string]string{"192.168.1.50": "Living room NAS"},
	}

	if got := r.Rule("r13"); got != "Block known scanners" {
		t.Errorf("Rule(\"r13\") = %q, want \"Block known scanners\"", got)
	}
	if got := r.Host("192.168.1.50"); got != "Living room NAS" {
		t.Errorf("Host(\"192.168.1.50\") = %q, want \"Living room NAS\"", got)
	}
}

func TestResolverMissReturnsEmptyString(t *testing.T) {
	r := Resolver{
		Rules: map[string]string{"r13": "Block known scanners"},
		Hosts: map[string]string{"192.168.1.50": "Living room NAS"},
	}

	if got := r.Rule("r99"); got != "" {
		t.Errorf("Rule(\"r99\") = %q, want empty", got)
	}
	if got := r.Host("10.0.0.1"); got != "" {
		t.Errorf("Host(\"10.0.0.1\") = %q, want empty", got)
	}
}

func TestZeroValueResolverIsUsable(t *testing.T) {
	var r Resolver
	if got := r.Rule("r13"); got != "" {
		t.Errorf("Rule() on zero value = %q, want empty", got)
	}
	if got := r.Host("192.168.1.50"); got != "" {
		t.Errorf("Host() on zero value = %q, want empty", got)
	}
}

// TestEntityLabelTakesPrecedenceOverConfigMap is issue #107's actual
// point: an admin-managed entity label, edited live via the UI, must
// override the YAML-configured ruleNames/hostNames for the same key --
// not merge with it, not lose to it.
func TestEntityLabelTakesPrecedenceOverConfigMap(t *testing.T) {
	es, _ := entities.Open("")
	if _, err := es.Upsert(entities.Entity{Type: entities.TypeRule, Key: "r13", Label: "UI label wins"}); err != nil {
		t.Fatal(err)
	}
	if _, err := es.Upsert(entities.Entity{Type: entities.TypeHost, Key: "192.168.1.50", Label: "UI host label wins"}); err != nil {
		t.Fatal(err)
	}

	r := Resolver{
		Rules:    map[string]string{"r13": "YAML label loses"},
		Hosts:    map[string]string{"192.168.1.50": "YAML host label loses"},
		Entities: es,
	}

	if got := r.Rule("r13"); got != "UI label wins" {
		t.Errorf("Rule(\"r13\") = %q, want the entity's label to take precedence", got)
	}
	if got := r.Host("192.168.1.50"); got != "UI host label wins" {
		t.Errorf("Host(\"192.168.1.50\") = %q, want the entity's label to take precedence", got)
	}
}

// TestConfigMapIsFallbackWhenNoEntityExists proves the config maps still
// work as documented for any key that hasn't (yet) been given an entity
// -- the migration/precedence design explicitly keeps them as a
// lower-priority fallback, not something replaced outright.
func TestConfigMapIsFallbackWhenNoEntityExists(t *testing.T) {
	es, _ := entities.Open("")
	if _, err := es.Upsert(entities.Entity{Type: entities.TypeRule, Key: "r13", Label: "only r13 has an entity"}); err != nil {
		t.Fatal(err)
	}

	r := Resolver{
		Rules:    map[string]string{"r13": "unused", "r99": "still from config.yaml"},
		Entities: es,
	}

	if got := r.Rule("r99"); got != "still from config.yaml" {
		t.Errorf("Rule(\"r99\") = %q, want the config.yaml fallback since no entity exists for it", got)
	}
}

// TestEntityWithNoLabelFallsThroughToConfigMap covers an entity that
// exists (e.g. tagged for a sibling feature like the mail-sender
// allowlist) but has no Label set -- an empty Label must not shadow a
// real config.yaml name with blank text.
func TestEntityWithNoLabelFallsThroughToConfigMap(t *testing.T) {
	es, _ := entities.Open("")
	if _, err := es.Upsert(entities.Entity{Type: entities.TypeHost, Key: "192.168.1.50", Tags: []string{"trusted-mail-sender"}}); err != nil {
		t.Fatal(err)
	}

	r := Resolver{
		Hosts:    map[string]string{"192.168.1.50": "config name still shows"},
		Entities: es,
	}

	if got := r.Host("192.168.1.50"); got != "config name still shows" {
		t.Errorf("Host(\"192.168.1.50\") = %q, want the config.yaml fallback since the entity has no label", got)
	}
}

// TestResolverPort covers issue #109's addition: a port entity resolves
// by its decimal-string key, and -- unlike Rule/Host -- there is no
// config.yaml map to fall back to, so a miss is always "".
func TestResolverPort(t *testing.T) {
	es, _ := entities.Open("")
	if _, err := es.Upsert(entities.Entity{Type: entities.TypePort, Key: "8291", Label: "Winbox"}); err != nil {
		t.Fatal(err)
	}

	r := Resolver{Entities: es}

	if got := r.Port(8291); got != "Winbox" {
		t.Errorf("Port(8291) = %q, want \"Winbox\"", got)
	}
	if got := r.Port(443); got != "" {
		t.Errorf("Port(443) = %q, want empty (no entity, no fallback map)", got)
	}
}

// TestResolverPortZeroAndNegativeAlwaysMiss covers SrcPort/DstPort's own
// "0 means no port" convention (internal/store/event.go) -- Port must
// never look up "0" as though it were a real port, even if some future
// caller mistakenly upserted an entity keyed "0" or a negative value.
func TestResolverPortZeroAndNegativeAlwaysMiss(t *testing.T) {
	es, _ := entities.Open("")
	if _, err := es.Upsert(entities.Entity{Type: entities.TypePort, Key: "0", Label: "should never be returned"}); err != nil {
		t.Fatal(err)
	}
	r := Resolver{Entities: es}

	if got := r.Port(0); got != "" {
		t.Errorf("Port(0) = %q, want empty", got)
	}
	if got := r.Port(-1); got != "" {
		t.Errorf("Port(-1) = %q, want empty", got)
	}
}

// TestResolverPortZeroValueIsUsable mirrors TestZeroValueResolverIsUsable
// for Port -- a nil Entities store must miss cleanly, not panic.
func TestResolverPortZeroValueIsUsable(t *testing.T) {
	var r Resolver
	if got := r.Port(8291); got != "" {
		t.Errorf("Port() on zero value = %q, want empty", got)
	}
}

// fakeRouterHosts backs the RouterHosts precedence tests without a real
// routerstate.Store -- the interface exists for exactly this.
type fakeRouterHosts map[string]string

func (f fakeRouterHosts) HostName(ip string) string { return f[ip] }

// TestRouterHostsWinOverEverything is issue #186 step 4c's owner
// decision as a test: a router-pushed name out-ranks both an
// admin-managed entity label and the config map for the same address --
// and an address the router does not name falls through to them
// untouched.
func TestRouterHostsWinOverEverything(t *testing.T) {
	es, _ := entities.Open("")
	if _, err := es.Upsert(entities.Entity{Type: entities.TypeHost, Key: "192.168.1.50", Label: "UI label"}); err != nil {
		t.Fatal(err)
	}

	r := Resolver{
		Hosts:       map[string]string{"192.168.1.50": "config label", "192.168.1.60": "config-only host"},
		Entities:    es,
		RouterHosts: fakeRouterHosts{"192.168.1.50": "router-name"},
	}

	if got := r.Host("192.168.1.50"); got != "router-name" {
		t.Errorf("Host() = %q, want the router-pushed name to win over entity and config labels", got)
	}
	if got := r.Host("192.168.1.60"); got != "config-only host" {
		t.Errorf("Host() = %q -- an address the router does not name must fall through untouched", got)
	}
}
