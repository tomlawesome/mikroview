// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// TextProblem is one entry in the config editor's Problems rail
// (#1347): a problem with a config text, placed on the line it is about.
// Named apart from Problem, which is the loader's own richer record this
// is rendered from.
type TextProblem struct {
	// Line is the 1-based line the problem is on, 0 when it is about a
	// setting the text does not contain (a default that the environment
	// overrode badly, say) or about the file as a whole.
	Line int `json:"line"`
	// Key is the dotted path of the setting, empty when the problem is
	// not about one setting (a syntax error).
	Key string `json:"key"`
	// Severity is "fatal" (MikroView would refuse to start) or
	// "warning" (it would start, using a safe default instead).
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// ValidateText checks a config text the way start-up would check it as
// config.yaml: the same strict decode (unknown keys refuse, with #1207's
// explanation of what replaced a removed one), then the same
// environment, app folder and config.Validate pass load uses -- so the
// editor can never pass a file the server would refuse, or the other way
// round. Flags are not applied: the text is a file, and a flag on the
// running process is not part of it.
//
// Where load stops at the first decode failure, this carries on past
// unknown keys to Validate as well, so the rail shows everything wrong
// at once instead of one problem per restart.
func ValidateText(text string) []TextProblem {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return []TextProblem{{Line: yamlErrorLine(err), Severity: SeverityFatal.String(), Message: err.Error()}}
	}
	lines := keyLines(&doc)

	var problems []TextProblem
	cfg := defaults()
	dec := yaml.NewDecoder(strings.NewReader(text))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		var typeErr *yaml.TypeError
		if !errors.As(err, &typeErr) {
			return []TextProblem{{Line: yamlErrorLine(err), Severity: SeverityFatal.String(), Message: err.Error()}}
		}
		for _, issue := range keyIssues(typeErr) {
			problems = append(problems, TextProblem{Line: issue.Line, Key: issue.Key, Severity: SeverityFatal.String(), Message: issue.Msg})
		}
	}

	_, result, err := finishLoad(cfg, "", nil, nil)
	if err != nil && len(result.Fatal) == 0 {
		// Not a Validate finding: the app folder refusing half a TLS
		// pair, or a flag error. Still a refusal to start.
		problems = append(problems, TextProblem{Severity: SeverityFatal.String(), Message: err.Error()})
	}
	for _, p := range append(result.Fatal, result.Warnings...) {
		problems = append(problems, TextProblem{
			Line:     lineForKey(lines, p.Key),
			Key:      p.Key,
			Severity: p.Severity.String(),
			Message:  problemMessage(p),
		})
	}
	return problems
}

// problemMessage words a Problem for the rail: what is wrong, what was
// used instead, and what to do -- Problem.String without the severity,
// code and key the rail already shows as fields.
func problemMessage(p Problem) string {
	s := p.Message
	if p.Applied != "" {
		s += " (using " + p.Applied + " instead)"
	}
	if p.Remediation != "" {
		s += " -- " + p.Remediation
	}
	return s
}

// lineForKey places a Problem.Key on its line: the key itself if the
// text has it, else the nearest enclosing section it does have, else 0.
func lineForKey(lines map[string]int, key string) int {
	for key != "" {
		if l, ok := lines[key]; ok {
			return l
		}
		i := strings.LastIndexAny(key, ".[")
		if i < 0 {
			return 0
		}
		key = key[:i]
	}
	return 0
}

// yamlErrorLine pulls the line number out of a yaml.v3 error, 0 when it
// names none.
func yamlErrorLine(err error) int {
	if m := yamlLinePattern.FindStringSubmatch(err.Error()); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}
