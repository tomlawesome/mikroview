// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The config file's own version marker (#1347): four comment lines at
// the top of every file MikroView writes, and of
// deploy/config.example.yaml, saying which settings the file should have
// (schema), which build wrote it (written-by) and which order and
// banners it is laid out in (layout). Comments rather than a YAML key so
// a file carrying them still loads on every release before this one,
// and so KnownFields(true) never has to learn about them.

// CurrentSchema is the key set this build reads. It goes up by one in
// any release whose key set changed -- a key added, removed or renamed
// -- and is the number the editor writes into every file it produces.
//
// Still 7 with #1347: the editor added no config key (its snapshots
// live beside the accounts store, like the Postgres adoption marker, not
// at a path of their own), and no key has changed since v0.6.1.
const CurrentSchema = 7

// CurrentLayout is the order and banners the editor writes a file in:
// header, the settings that must be set, optional features, then the
// defaults under their ASCII banner (owner, 2026-09-25). Goes up only if
// that arrangement itself changes.
const CurrentLayout = 1

// schemaByVersion is the schema each release reads, from the key sets
// at each tag (the facts note on #1347, re-derived from each tag's
// config.go). A release whose key set did not change shares the schema
// before it.
var schemaByVersion = []struct {
	Version string
	Schema  int
}{
	{"v0.1.0", 1},
	{"v0.2.0", 2},
	{"v0.3.0", 3},
	{"v0.3.1", 3},
	{"v0.4.0", 4},
	{"v0.5.0", 5},
	{"v0.5.1", 5},
	{"v0.6.0", 6},
	{"v0.6.1", 7},
}

// SchemaForVersion reports the schema a release reads, false for a
// version the table does not name.
func SchemaForVersion(version string) (int, bool) {
	v := "v" + strings.TrimPrefix(strings.TrimSpace(version), "v")
	for _, e := range schemaByVersion {
		if e.Version == v {
			return e.Schema, true
		}
	}
	return 0, false
}

// keysIntroducedAt is every leaf key a schema added over the one before
// it, from diffing each tag's config.go. Schema 1's own keys are not
// listed: a file using only those says nothing beyond "1 or later".
//
// GuessSchema reads this to date a file with no header. Keys that were
// later removed are listed at the schema that added them, same as any
// other: a file still carrying one was written no earlier than that.
var keysIntroducedAt = map[int][]string{
	2: {
		"auth.sessionMaxLifetime", "listen.syslogTls", "netClass.sources", "store.maxMemory",
		"watchlist.matchLogCapacity", "watchlist.matchLogPath", "watchlist.matchLogRetention",
		"watchlist.storePath", "watchlist.suggestionsStorePath",
	},
	3: {"engine.definitionsStorePath", "engine.storePath"},
	4: {"setup.storePath"},
	5: {
		"backup.enabled", "backup.listen", "backup.vaultDir",
		"baseline.days", "baseline.hostQuietAfter", "baseline.of", "baseline.storePath",
		"coverage.storePath", "engine.decommissionCleanWindow", "engine.decommissionStorePath",
		"history.days", "history.dir", "history.enabled", "history.keyFile", "history.maxBytes",
		"hosts.storePath", "oui.cachePath", "oui.enabled",
		"snapshot.dir", "snapshot.interval", "snapshot.keep", "store.settingsStorePath",
	},
	6: {"configDrift.storePath", "deviceRegistry.storePath", "droplist.storePath", "seen.storePath"},
	7: {"prefs.storePath", "publicUrl", "ui.allow"},
}

// Header is what the four header lines say.
type Header struct {
	Schema int `json:"schema"`
	// WrittenBy is the release that wrote the file, as "v0.6.1" -- the
	// "mikroview " the line itself carries is not repeated here.
	WrittenBy string `json:"writtenBy"`
	Layout    int    `json:"layout"`
}

// headerTitle opens the header block. ParseHeader keys on its first few
// words rather than the whole line, so a file whose title line was
// re-wrapped or trimmed by hand still reads.
const headerTitle = "# MikroView configuration -- keep these four lines; MikroView reads them."

var (
	headerTitlePattern     = regexp.MustCompile(`^#\s*MikroView configuration\b`)
	headerSchemaPattern    = regexp.MustCompile(`^#\s*schema:\s*(\d+)\b`)
	headerWrittenByPattern = regexp.MustCompile(`^#\s*written-by:\s*(?:mikroview\s+)?(\S+)`)
	headerLayoutPattern    = regexp.MustCompile(`^#\s*layout:\s*(\d+)\b`)
)

// headerSpan finds the header block at the top of lines: the title line
// (after any leading blank lines) and the schema/written-by/layout lines
// directly under it. It returns the half-open range of lines it covers,
// or ok=false when the file opens with anything else.
func headerSpan(lines []string) (start, end int, h Header, ok bool) {
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || !headerTitlePattern.MatchString(lines[i]) {
		return 0, 0, Header{}, false
	}
	start = i
	end = i + 1
	for end < len(lines) {
		line := strings.TrimSpace(lines[end])
		if m := headerSchemaPattern.FindStringSubmatch(line); m != nil {
			h.Schema, _ = strconv.Atoi(m[1])
		} else if m := headerWrittenByPattern.FindStringSubmatch(line); m != nil {
			h.WrittenBy = "v" + strings.TrimPrefix(m[1], "v")
		} else if m := headerLayoutPattern.FindStringSubmatch(line); m != nil {
			h.Layout, _ = strconv.Atoi(m[1])
		} else {
			break
		}
		end++
	}
	return start, end, h, h.Schema > 0
}

// ParseHeader reads the header block at the top of text. ok is false for
// a file with no header (every config written before #1347), or one
// whose header lost its schema line -- the one line the editor cannot
// do without.
func ParseHeader(text string) (Header, bool) {
	_, _, h, ok := headerSpan(splitLines(text))
	return h, ok
}

// HeaderLines renders h as the four header lines, without a trailing
// newline. The parenthesised notes are padded to line up, as in the
// ratified block on #1347.
func HeaderLines(h Header) string {
	return strings.Join([]string{
		headerTitle,
		fmt.Sprintf("%-23s(which settings this file should have; goes up when keys change)", fmt.Sprintf("# schema: %d", h.Schema)),
		"# written-by: mikroview " + "v" + strings.TrimPrefix(h.WrittenBy, "v"),
		fmt.Sprintf("%-23s(the order and banners below)", fmt.Sprintf("# layout: %d", h.Layout)),
	}, "\n")
}

// WriteHeader returns text with h as its header: an existing header
// block is replaced where it stands, and a file without one gets it
// prepended with a blank line after it.
func WriteHeader(text string, h Header) string {
	lines := splitLines(text)
	if start, end, _, ok := headerSpan(lines); ok {
		out := append([]string{}, lines[:start]...)
		out = append(out, HeaderLines(h))
		out = append(out, lines[end:]...)
		return joinLines(out)
	}
	if strings.TrimSpace(text) == "" {
		return HeaderLines(h) + "\n"
	}
	return HeaderLines(h) + "\n\n" + strings.TrimLeft(text, "\n")
}

// GuessSchema dates a file with no header from the keys it uses: the
// newest schema any of its live keys was introduced at, and 1 when none
// says more. Commented-out lines are not evidence -- a file copied from a
// newer example carries every key commented out whatever it was written
// for.
//
// Keys a later release removed are deliberately not used to cap the
// guess. Before #1207 an unknown key was silently ignored, so a stale
// one can sit in a file through several upgrades -- the owner's own
// v0.6.0 install still carried v0.1.0's listen.syslogUdp -- and the
// newest key present is the better witness of when the file was last
// brought up to date. Text that does not parse guesses 1.
func GuessSchema(text string) int {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return 1
	}
	present := map[string]bool{}
	for path := range keyLines(&doc) {
		present[path] = true
	}
	guess := 1
	for schema, keys := range keysIntroducedAt {
		if schema <= guess {
			continue
		}
		for _, k := range keys {
			if present[k] {
				guess = schema
				break
			}
		}
	}
	return guess
}

// keyLines maps every key in doc to the 1-based line it sits on, by its
// full dotted path: "notify.smtp.password", and "devices[0].sourceIp" for
// an entry in a list -- the same spelling config.Validate's Problem.Key
// uses, so a problem can be placed on its line.
func keyLines(doc *yaml.Node) map[string]int {
	out := map[string]int{}
	var walk func(n *yaml.Node, prefix string)
	walk = func(n *yaml.Node, prefix string) {
		switch n.Kind {
		case yaml.DocumentNode:
			for _, c := range n.Content {
				walk(c, prefix)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				path := k.Value
				if prefix != "" {
					path = prefix + "." + k.Value
				}
				out[path] = k.Line
				walk(v, path)
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				path := fmt.Sprintf("%s[%d]", prefix, i)
				out[path] = c.Line
				walk(c, path)
			}
		}
	}
	walk(doc, "")
	return out
}

// splitLines splits text into lines without their newlines. A trailing
// newline does not produce a final empty line, so joinLines(splitLines(s))
// gives back s with exactly one trailing newline.
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// joinLines is splitLines' inverse, ending the text with one newline.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// StampHeader writes the header a downloaded file carries (#1347): the
// running release as written-by, since MikroView is what wrote it, and
// a schema and layout that say what the file actually is rather than
// what the build would like it to be. A text this build accepts (no
// fatal problems) is for CurrentSchema; one it would refuse keeps the
// schema its header claimed, or the guess from its keys. The layout is
// kept from an existing header, or is CurrentLayout for a text carrying
// the layout's own banners, or 0 -- "not laid out by MikroView" -- for
// neither.
func StampHeader(text, writtenBy string) string {
	old, hadHeader := ParseHeader(text)
	h := Header{WrittenBy: writtenBy, Schema: CurrentSchema, Layout: old.Layout}
	fatal := false
	for _, p := range ValidateText(text) {
		if p.Severity == SeverityFatal.String() {
			fatal = true
			break
		}
	}
	if fatal {
		h.Schema = old.Schema
		if !hadHeader {
			h.Schema = GuessSchema(text)
		}
	}
	if !hadHeader && strings.Contains(text, layoutSections[0].Banner) {
		h.Layout = CurrentLayout
	}
	return WriteHeader(text, h)
}
