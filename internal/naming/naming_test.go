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
