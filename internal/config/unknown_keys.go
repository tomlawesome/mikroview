// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// removedKey documents a configuration key that once existed and no
// longer does, so an unknown-key error can say why instead of just
// naming the key. Version is the release it stopped being read in.
//
// Every entry here must trace to a real, dated line in CHANGELOG.md --
// see the comment above the map. A guessed or invented mapping would
// send an operator confidently at the wrong fix.
type removedKey struct {
	Version string
	Why     string
}

// removedOrRenamedKeys is every configuration key CHANGELOG.md records
// as removed or renamed, keyed by its full dotted yaml path. Sourced
// from CHANGELOG.md's "### Removed" sections as of 2026-09-13:
//
//   - listen.syslogUdp / listen.syslogTcp: "## [0.2.0] - 2026-08-14",
//     "The plaintext syslog listeners" (#189) -- removed wholesale,
//     including their env vars and CLI flags, leaving syslogTls as the
//     only syslog listener.
//   - store.maxEvents: same section, "`store.maxEvents`" (#244) --
//     replaced by store.maxMemory, a byte-size budget rather than a
//     raw event count.
//   - watchlist.storePath / flags.detectorSettingsStorePath:
//     "## [0.5.0] - 2026-09-10", "`watchlist.storePath` and
//     `flags.detectorSettingsStorePath` are gone" (#873) -- the stores
//     they configured were deleted; both document kinds now live under
//     engine.definitionsStorePath.
//
// Do not add an entry without a matching CHANGELOG.md line: an unknown
// key with no entry here still refuses to start (see explainYAMLError),
// it just gets the generic message instead of a specific one.
var removedOrRenamedKeys = map[string]removedKey{
	"listen.syslogUdp": {
		Version: "v0.2.0",
		Why:     "plaintext syslog is gone (#189) -- the only syslog listener left is TLS. Use listen.syslogTls instead.",
	},
	"listen.syslogTcp": {
		Version: "v0.2.0",
		Why:     "plaintext syslog is gone (#189) -- the only syslog listener left is TLS. Use listen.syslogTls instead.",
	},
	"store.maxEvents": {
		Version: "v0.2.0",
		Why:     `replaced by store.maxMemory (#244): a memory budget (e.g. "120MiB") rather than a raw event count.`,
	},
	"watchlist.storePath": {
		Version: "v0.5.0",
		Why:     "the store it configured was deleted (#873) -- watchlist entries now live under engine.definitionsStorePath. Remove this key.",
	},
	"flags.detectorSettingsStorePath": {
		Version: "v0.5.0",
		Why:     "the store it configured was deleted (#873) -- detector settings now live under engine.definitionsStorePath. Remove this key.",
	},
}

// unknownFieldPattern matches one line of a yaml.v3 *yaml.TypeError
// produced by KnownFields(true): "line N: field X not found in type
// pkg.Type". Confirmed against go.yaml.in/yaml/v3 (see explainYAMLError's
// doc comment) rather than assumed -- a decoder upgrade that changes
// this wording would fall back to the library's own generic error text
// via the "no match" branch below, not silently mis-parse.
var unknownFieldPattern = regexp.MustCompile(`^line (\d+): field (\S+) not found in type (\S+)$`)

// yamlPathPrefixes maps every struct type reachable from Config to the
// dotted yaml path it sits at, e.g. reflect.TypeOf(Webhook{}) ->
// "notify.webhook". Built once by walking Config's own field tags.
//
// This is what turns a yaml.v3 error -- which names only the immediate
// Go type ("config.Webhook") and the bare offending field ("bogus") --
// into the full path ("notify.webhook.bogus") an operator can actually
// find in their file. Reflection over the live struct tags rather than
// a hand-maintained table, so it cannot drift from the fields that
// actually exist.
func yamlPathPrefixes() map[reflect.Type]string {
	paths := map[reflect.Type]string{}
	var walk func(t reflect.Type, prefix string)
	walk = func(t reflect.Type, prefix string) {
		for t.Kind() == reflect.Slice || t.Kind() == reflect.Ptr || t.Kind() == reflect.Array {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return
		}
		if _, seen := paths[t]; seen {
			return
		}
		paths[t] = prefix
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			childPrefix := name
			if prefix != "" {
				childPrefix = prefix + "." + name
			}
			walk(f.Type, childPrefix)
		}
	}
	walk(reflect.TypeOf(Config{}), "")
	return paths
}

// explainYAMLError turns a yaml.v3 decode error into one that names the
// offending key by its full dotted path, the line it's on, and -- for a
// key removedOrRenamedKeys knows about -- what changed and what to use
// instead. Every other decode error (a type mismatch, bad indentation,
// and so on) passes through unchanged: this only rewrites the specific
// "field not found" shape KnownFields(true) produces.
func explainYAMLError(path string, err error) error {
	typeErr, ok := err.(*yaml.TypeError)
	if !ok {
		// Not the KnownFields(true) shape -- a syntax error or a type
		// mismatch. The caller (load) already wraps this in "loading
		// config file %s: %w", so leave it as-is rather than naming the
		// path twice.
		return err
	}

	prefixes := yamlPathPrefixes()
	var lines []string
	for _, e := range typeErr.Errors {
		m := unknownFieldPattern.FindStringSubmatch(e)
		if m == nil {
			// Some other *yaml.TypeError shape (a type mismatch, for
			// instance) -- pass it through rather than guess at it.
			lines = append(lines, e)
			continue
		}
		lineNo, field, goType := m[1], m[2], m[3]

		prefix := ""
		found := false
		for t, p := range prefixes {
			if t.String() == goType {
				prefix, found = p, true
				break
			}
		}
		key := field
		if found && prefix != "" {
			key = prefix + "." + field
		}

		if removed, ok := removedOrRenamedKeys[key]; ok {
			lines = append(lines, fmt.Sprintf(
				"%s line %s: unknown key %q -- %s was removed in %s: %s",
				path, lineNo, key, key, removed.Version, removed.Why))
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"%s line %s: unknown key %q -- not a recognised mikroview configuration key; check for a typo or a stale key from an old release "+
				"(see CHANGELOG.md, or run mikroview -validate-config after removing it)",
			path, lineNo, key))
	}
	return fmt.Errorf("%s", strings.Join(lines, "; "))
}
