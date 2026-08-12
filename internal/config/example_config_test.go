// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// README.md calls deploy/config.example.yaml "the full option
// reference". This holds it to that.
//
// It had drifted: no oidc: block, no postgres: block, no webhook:
// under notify:, and thirteen real flags fields absent even as commented
// lines -- all of them fully documented with prose and defaults in
// docs/configuration.md (#268 finding 14). An operator reading the file
// the README points them at could not discover single sign-on or the
// Postgres backend existed at all.
//
// Commented-out lines count. Every optional block in this file ships
// commented out by design, so the test looks for the key appearing at
// all, not for live YAML.
func TestExampleConfigMentionsEveryOption(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/config.example.yaml")
	if err != nil {
		t.Fatalf("reading the example config: %v", err)
	}
	text := string(raw)

	// Keys as they appear at the start of a line, with or without the
	// leading "# " every commented block uses.
	present := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if m := keyPattern.FindStringSubmatch(line); m != nil {
			present[m[1]] = true
		}
	}

	var missing []string
	for _, name := range yamlFieldNames(reflect.TypeOf(Config{})) {
		if !present[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("deploy/config.example.yaml does not mention %d option(s) -- README.md calls it the full option reference:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

var keyPattern = regexp.MustCompile(`^-?\s*([a-zA-Z][a-zA-Z0-9]*):`)

// yamlFieldNames walks a struct's yaml tags, descending into nested
// structs (and through slices/maps to their element type) so a field
// like notify.webhook.url is reached. Names only -- the nesting is not
// checked, because the example legitimately shows some blocks flattened
// under a comment rather than as live YAML.
func yamlFieldNames(t reflect.Type) []string {
	var out []string
	var walk func(reflect.Type, int)
	walk = func(t reflect.Type, depth int) {
		// Deep enough for this config, and a hard stop against a type
		// that ever refers to itself.
		if depth > 6 {
			return
		}
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("yaml")
			if tag == "" || tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			if name == "" {
				continue
			}
			out = append(out, name)
			walk(f.Type, depth+1)
		}
	}
	walk(t, 0)
	return out
}
