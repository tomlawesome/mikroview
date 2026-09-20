// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"reflect"
	"testing"

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
}
