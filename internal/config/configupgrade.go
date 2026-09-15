// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// MissingSetting is one optional configuration section this build
// understands that the operator's own config file does not set at all
// (#1218).
type MissingSetting struct {
	// Key is the top-level yaml key, e.g. "geoip", "history", "backup".
	Key string
	// Block is the exact text of deploy/config.example.yaml's own
	// section for Key -- its comment lines and all, already "#"-
	// commented -- ready to paste under config.yaml's own devices: line.
	// Never synthesised: a one-line description written here instead
	// would drift from docs/configuration.md's real explanation the
	// moment either one changed and nothing would notice.
	Block string
}

// MissingSettings reports every MissingSetting for runningYAML -- the
// operator's actual config file, undecoded -- paired against exampleYAML
// (deploy/config.example.yaml's own bytes; the caller embeds it, since
// this package cannot go:embed a file outside its own directory).
//
// Granularity is the top-level key config.example.yaml itself organises
// around (geoip, history, backup, ...), not every leaf field: that is
// where the file's own explanatory comments live, and it is also where
// every motivating case for #1218 already sat (geoip.dbPath, history.*,
// backup.enabled are each a whole missing top-level key, not a lone
// field bolted onto a section already present). A field later added
// beside a section the operator already has some of -- a hypothetical
// listen.newOption next to the listen: block every deployment already
// sets -- is not separately surfaced by this; the operator already has
// that section open to compare against docs/configuration.md.
// ruleNames and hostNames share one comment block in the example file
// and so are treated as one unit too: config.example.yaml's own author
// already made that call by documenting them together, and this reuses
// it rather than second-guessing it.
//
// A required section (listen, store, devices -- shown live rather than
// commented out in the example file, because a working deployment
// always has some form of it) is never offered: every MissingSetting is
// something to paste in commented out, and turning a live block into
// one would mean rewriting it -- exactly the "reorders keys, loses
// their own comments" cost #1218's ruling rejected paying against the
// operator's actual file, done here to the example instead.
func MissingSettings(exampleYAML, runningYAML []byte) ([]MissingSetting, error) {
	present, err := presentTopLevelKeys(runningYAML)
	if err != nil {
		return nil, fmt.Errorf("reading the running config: %w", err)
	}

	valid := make(map[string]bool)
	for _, k := range configTopLevelKeys() {
		valid[k] = true
	}

	var out []MissingSetting
	for _, sec := range parseExampleSections(string(exampleYAML), valid) {
		if !sec.commented || present[sec.key] {
			continue
		}
		out = append(out, MissingSetting{Key: sec.key, Block: sec.block})
	}
	return out, nil
}

// configTopLevelKeys is every yaml key Config itself declares -- a
// flat, one-level read of its own field tags, not the recursive walk
// example_config_test.go's yamlFieldNames does for every leaf field.
// MissingSettings only ever proposes a whole top-level section (see its
// own doc comment for why), so it only ever needs to know what counts
// as one.
func configTopLevelKeys() []string {
	t := reflect.TypeOf(Config{})
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		keys = append(keys, name)
	}
	return keys
}

// presentTopLevelKeys is the set of keys runningYAML's root mapping
// actually sets. Read with yaml.Node rather than into Config itself:
// this needs to know which keys the operator *typed*, not which fields
// ended up non-zero -- a field explicitly set to its own zero value
// (history: {enabled: false}) must still count as present. An empty or
// unreadable document (no config file at all, or a path the caller
// could not read) is treated as "nothing set" rather than an error --
// config.LoadWithProblems has already validated this same file moments
// before any caller reaches this, so a failure here would only be a
// race, not a real problem to report a second time.
func presentTopLevelKeys(raw []byte) (map[string]bool, error) {
	present := make(map[string]bool)
	if len(bytes.TrimSpace(raw)) == 0 {
		return present, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return present, nil
	}
	doc := root.Content[0]
	for i := 0; i+1 < len(doc.Content); i += 2 {
		present[doc.Content[i].Value] = true
	}
	return present, nil
}

// exampleSection is one blank-line-delimited section of
// deploy/config.example.yaml, keyed by the top-level Config field it
// documents.
type exampleSection struct {
	key       string
	commented bool
	block     string
}

// topLevelKeyLine matches a key line with no indentation of its own,
// once stripCommentMarker has removed the single leading "# " every
// commented line in the example file carries.
var topLevelKeyLine = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9]*):`)

// stripCommentMarker removes exactly one leading "#" and, if present,
// exactly one space after it -- the fixed marker every commented line in
// deploy/config.example.yaml carries -- leaving any further indentation
// (a nested field, two or more spaces deep) intact for the caller to
// see. A live (uncommented) line passes through unchanged.
func stripCommentMarker(line string) string {
	if !strings.HasPrefix(line, "#") {
		return line
	}
	return strings.TrimPrefix(line[1:], " ")
}

// startsIndented reports whether line, once its comment marker (if any)
// is stripped, begins with further whitespace -- i.e. it is a nested
// field of whatever top-level section is already open, not the start of
// a new one.
func startsIndented(line string) bool {
	return strings.HasPrefix(stripCommentMarker(line), " ")
}

// parseExampleSections splits text into the same sections a reader
// looking at deploy/config.example.yaml would see: blank-line-separated
// blocks, except a blank line is kept inside the current block when
// what follows it is still indented under that block (the file nests a
// second explanatory comment, with its own blank line before it, under
// several top-level keys -- e.g. listen:'s reverse-proxy fields).
//
// Each finished block is matched to whichever key in valid appears on a
// zero-indentation line inside it (its own key line, commented or not);
// a block matching none (the file's leading header, or one whose key
// this build no longer has) is dropped silently.
func parseExampleSections(text string, valid map[string]bool) []exampleSection {
	lines := strings.Split(text, "\n")
	var sections []exampleSection
	var current []string

	flush := func() {
		if len(current) == 0 {
			return
		}
		if key, commented, ok := sectionKey(current, valid); ok {
			block := strings.TrimRight(strings.Join(current, "\n"), "\n")
			sections = append(sections, exampleSection{key: key, commented: commented, block: block})
		}
		current = nil
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) != "" {
			current = append(current, line)
			continue
		}
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}
		if j < len(lines) && startsIndented(lines[j]) {
			// Still inside the open section -- keep the blank line so
			// Block reproduces the file exactly.
			current = append(current, line)
			continue
		}
		flush()
	}
	flush()
	return sections
}

// sectionKey finds the line in lines that introduces one of valid's
// keys at zero indentation, and reports whether that line was itself
// commented out.
func sectionKey(lines []string, valid map[string]bool) (key string, commented bool, ok bool) {
	for _, line := range lines {
		dekeyed := stripCommentMarker(line)
		if strings.HasPrefix(dekeyed, " ") {
			continue
		}
		m := topLevelKeyLine.FindStringSubmatch(dekeyed)
		if m == nil || !valid[m[1]] {
			continue
		}
		return m[1], strings.HasPrefix(line, "#"), true
	}
	return "", false, false
}
