// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"
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
	// RenamedTo is the key's new full dotted path when the value carries
	// over unchanged under a new name -- the one case the config editor's
	// Carry forward (#1347) can move rather than drop. Empty for every
	// entry today: each removal so far either retired the setting or
	// replaced it with one of a different shape (a count became a byte
	// budget, two plaintext listeners became one TLS one), where moving
	// the old value across would carry a wrong value, not a right one.
	RenamedTo string
}

// removedOrRenamedKeys is every configuration key CHANGELOG.md records
// as removed or renamed, keyed by its full dotted yaml path. Sourced
// from CHANGELOG.md's "### Removed" sections as of 2026-09-20:
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
//   - configDrift: "## [Unreleased]", "`configDrift.storePath` is gone"
//     (#1277) -- the whole configDrift section is removed (it only ever
//     had the one key), so an unknown key here is the top-level
//     "configDrift" itself, never a dotted "configDrift.storePath": the
//     backend it configured (the config-upgrade notice's per-version
//     dismissal, #1218) was removed in favor of a plain close button;
//     nothing reads or writes it any more.
//   - geoip.dbPath: "## [Unreleased]", "`geoip.dbPath` is gone" (#1352)
//     -- together with its -geoip-db flag and MIKROVIEW_GEOIP_DB_PATH;
//     MaxMind now works only through the account ID and licence key
//     entered on the Engine Room's "Country and network owner" card,
//     and MikroView downloads the file itself.
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
	"configDrift": {
		Version: "v0.6.1",
		Why: "configDrift.storePath, its only key, backed the config-upgrade notice's per-version " +
			"dismissal (#1218), removed in favor of a plain close button (#1277); nothing reads or writes " +
			"it any more. Remove this section.",
	},
	"geoip.dbPath": {
		Version: "v0.7.0",
		Why: "MikroView now downloads country data itself (#1352): flags work with no setup, and MaxMind " +
			"GeoLite2 is used by entering your account ID and licence key on the Engine Room's \"Country and " +
			"network owner\" card, not by pointing at a file. Remove this key.",
	},
}

// unknownFieldPattern matches the Message of one *yaml.LoadError
// produced by KnownFields(true): "field X not found in type pkg.Type"
// (the line is in its Mark). Confirmed against go.yaml.in/yaml/v4
// v4.0.0, whose wording is unchanged from v3 (#1498), rather than
// assumed -- a decoder upgrade that changes this wording would fall
// back to the library's own text via the "no match" branch below, not
// silently mis-parse.
var unknownFieldPattern = regexp.MustCompile(`^field (\S+) not found in type (\S+)$`)

// yamlPathPrefixes maps every struct type reachable from Config to the
// dotted yaml path it sits at, e.g. reflect.TypeOf(Webhook{}) ->
// "notify.webhook". Built once by walking Config's own field tags.
//
// This is what turns a yaml error -- which names only the immediate
// Go type ("config.Webhook") and the bare offending field ("bogus") --
// into the full path ("notify.webhook.bogus") an operator can actually
// find in their file. Reflection over the live struct tags rather than
// a hand-maintained table, so it cannot drift from the fields that
// actually exist.
func yamlPathPrefixes() map[reflect.Type]string {
	paths := map[reflect.Type]string{}
	var walk func(t reflect.Type, prefix string)
	walk = func(t reflect.Type, prefix string) {
		for t.Kind() == reflect.Slice || t.Kind() == reflect.Pointer || t.Kind() == reflect.Array {
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

// keyIssue is one decode problem the yaml decoder reported, pulled apart so the
// start-up refusal (explainYAMLError) and the config editor's Problems
// rail (ValidateText, #1347) word it identically: the rail adds the line
// as a field, the refusal as a "path line N:" prefix.
type keyIssue struct {
	// Line is the 1-based line the decoder named, 0 when it named none.
	Line int
	// Key is the full dotted path of an unknown key, empty for any
	// other kind of decode error.
	Key string
	// Msg is the explanation without any path or line prefix.
	Msg string
	// Raw marks a decode error passed through as the library worded it
	// (a type mismatch, say), which the refusal prints unprefixed.
	Raw bool
}

// yamlLinePattern pulls the line number out of any yaml message shaped
// "line N: ...", which its syntax errors ("yaml: line N: ...") use.
var yamlLinePattern = regexp.MustCompile(`line (\d+):`)

// keyIssues explains every entry of a *yaml.LoadErrors: an unknown key
// by its full dotted path, and -- for a key removedOrRenamedKeys knows
// about -- what changed and what to use instead. Every other entry
// passes through as the library worded it.
func keyIssues(loadErr *yaml.LoadErrors) []keyIssue {
	prefixes := yamlPathPrefixes()
	var issues []keyIssue
	for _, e := range loadErr.Errors {
		line := e.Mark.Line
		m := unknownFieldPattern.FindStringSubmatch(e.Message)
		if m == nil {
			// Some other decode failure (a type mismatch, for
			// instance) -- pass it through rather than guess at it,
			// in the library's own "line N: ..." entry form.
			msg := e.Message
			if line > 0 {
				msg = fmt.Sprintf("line %d: %s", line, msg)
			}
			issues = append(issues, keyIssue{Line: line, Msg: msg, Raw: true})
			continue
		}
		field, goType := m[1], m[2]

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
			issues = append(issues, keyIssue{Line: line, Key: key, Msg: fmt.Sprintf(
				"unknown key %q -- %s was removed in %s: %s", key, key, removed.Version, removed.Why)})
			continue
		}
		issues = append(issues, keyIssue{Line: line, Key: key, Msg: fmt.Sprintf(
			"unknown key %q -- not a recognised mikroview configuration key; check for a typo or a stale key from an old release "+
				"(see CHANGELOG.md, or run mikroview -validate-config after removing it)", key)})
	}
	return issues
}

// explainYAMLError turns a yaml decode error into one that names the
// offending key by its full dotted path, the line it's on, and -- for a
// key removedOrRenamedKeys knows about -- what changed and what to use
// instead. Every other decode error (a type mismatch, bad indentation,
// and so on) passes through unchanged: this only rewrites the specific
// "field not found" shape KnownFields(true) produces.
func explainYAMLError(path string, err error) error {
	// yaml/v4 reports decode failures as *yaml.LoadErrors; v3's
	// *yaml.TypeError is deprecated there (#1498).
	var loadErr *yaml.LoadErrors
	if !errors.As(err, &loadErr) {
		// Not the KnownFields(true) shape -- a syntax error or a type
		// mismatch. The caller (load) already wraps this in "loading
		// config file %s: %w", so leave it as-is rather than naming the
		// path twice.
		return err
	}

	var lines []string
	for _, issue := range keyIssues(loadErr) {
		if issue.Raw {
			lines = append(lines, issue.Msg)
			continue
		}
		lines = append(lines, fmt.Sprintf("%s line %d: %s", path, issue.Line, issue.Msg))
	}
	return fmt.Errorf("%s", strings.Join(lines, "; "))
}
