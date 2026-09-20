// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"reflect"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/engine"
)

// TestDetectorDefaultsMatchEnginePackage pins defaults().Flags to
// engine.DefaultDetectorDefaults(), field for field by name. main.go
// wires the former into the engine; every engine and API test seeds
// itself from the latter. They are two literals in two packages
// (config stays a dependency-free leaf, the same reason
// netclass_default_test.go exists), so without this a threshold
// changed in one place ships production on values the test suite never
// ran against. Every engine field must have a same-named Flags field:
// a new detector setting that reaches only one side fails here too.
// The two stale-rule values live outside DetectorDefaults, in
// engine.DefaultShippedDefaults(), whose comment promises they match
// config's own; the tail of this test holds it to that.
func TestDetectorDefaultsMatchEnginePackage(t *testing.T) {
	flags := reflect.ValueOf(defaults().Flags)
	want := reflect.ValueOf(engine.DefaultDetectorDefaults())
	for i := 0; i < want.NumField(); i++ {
		name := want.Type().Field(i).Name
		got := flags.FieldByName(name)
		if !got.IsValid() {
			t.Errorf("engine.DetectorDefaults.%s has no config.Flags field of that name; main.go cannot be wiring it", name)
			continue
		}
		if !reflect.DeepEqual(got.Interface(), want.Field(i).Interface()) {
			t.Errorf("config default flags.%s = %v, engine.DefaultDetectorDefaults().%s = %v -- these must agree, since main.go wires the former and the engine tests run on the latter", name, got.Interface(), name, want.Field(i).Interface())
		}
	}
	shipped := engine.DefaultShippedDefaults()
	if got, want := time.Duration(defaults().Flags.StaleRuleDays)*24*time.Hour, shipped.StaleRuleMaxAge; got != want {
		t.Errorf("config default staleRuleDays = %v, engine.DefaultShippedDefaults().StaleRuleMaxAge = %v", got, want)
	}
	if got, want := defaults().Flags.StaleRuleCheckInterval, shipped.StaleRuleCheckInterval; got != want {
		t.Errorf("config default staleRuleCheckInterval = %v, engine.DefaultShippedDefaults().StaleRuleCheckInterval = %v", got, want)
	}
}
