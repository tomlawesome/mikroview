// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"strings"
)

// Severity separates problems that stop mikroview starting from ones it
// starts despite.
//
// The split exists because "refuse to start" is not automatically the
// safe choice here. mikroview is a security monitor: a deployment that
// won't boot has lost the operator all visibility, which is its own
// security failure, not merely an availability one. So refusing is
// reserved for values that are actively unsafe or that mean mikroview
// isn't doing its job at all, and everything else starts on a safe
// default with the problem made impossible to miss.
type Severity int

const (
	// SeverityFatal: mikroview refuses to start. Reserved for values
	// where continuing would be actively insecure, or where the
	// deployment would silently monitor nothing.
	SeverityFatal Severity = iota
	// SeverityWarn: mikroview starts, applies a safe default, and
	// surfaces the problem in the admin UI as well as the log. A startup
	// log line alone is seen once by whoever ran `docker compose up` and
	// never again, which is not good enough for a value the operator
	// believes is in effect.
	SeverityWarn
)

func (s Severity) String() string {
	if s == SeverityFatal {
		return "fatal"
	}
	return "warning"
}

// Problem is one thing wrong with a configuration.
//
// Deliberately data rather than a formatted string: the CLI, the log,
// and the admin UI banner all render from this same value, so their
// wording cannot drift apart. Code is a stable identifier suitable for a
// metric label or a docs anchor -- unlike Message, it is safe to expose
// where the free text isn't (see Redacted).
type Problem struct {
	Code     string   `json:"code"`
	Severity Severity `json:"-"`
	// Key is the yaml path of the offending setting, e.g.
	// "store.retention". Named so the operator can find it without
	// guessing.
	Key string `json:"key"`
	// Message says what is wrong. It never contains a secret's value --
	// see Validate's redaction rules.
	Message string `json:"message"`
	// Applied describes the safe default substituted in place of the bad
	// value, empty for a fatal problem where nothing was substituted.
	// Load-bearing: silently clamping a value the operator chose is only
	// acceptable because this is reported back to them.
	Applied string `json:"applied,omitempty"`
	// Remediation is what to do about it, in plain language.
	Remediation string `json:"remediation,omitempty"`
	// Example is a config snippet showing the corrected setting, ready
	// to paste. Prose telling someone to "set a positive duration"
	// still leaves them guessing at the YAML shape -- which key, what
	// nesting, quoted or not -- and that guess is the part that goes
	// wrong at 2am. Kept in one table (examplesByCode) rather than at
	// each call site so the snippets and the docs stay in step.
	Example string `json:"example,omitempty"`
	// Docs is a deep link to this code's entry in the configuration
	// reference.
	Docs string `json:"docs,omitempty"`
}

// DocsURL is the configuration reference. Each problem code has an
// anchor there, so a message can point at the exact entry rather than at
// a two-thousand-line page.
const DocsURL = "https://github.com/tomlawesome/mikroview/blob/main/docs/configuration.md"

func (p Problem) String() string {
	s := fmt.Sprintf("%s  %s  %s: %s", p.Severity, p.Code, p.Key, p.Message)
	if p.Applied != "" {
		s += " (using " + p.Applied + " instead)"
	}
	if p.Remediation != "" {
		s += " -- " + p.Remediation
	}
	return s
}

// Report renders problems the way someone who has just been stopped by
// one needs to read them: the code they can search for, the key that is
// wrong, what is wrong with it, what was substituted if anything, and
// -- on its own line -- what to do about it.
//
// Fatals and warnings share this deliberately. A fatal used to reach the
// operator only through Err(), as a single line with the remediation
// tacked on after a "--" at the end of a long sentence. The advice was
// present and was the easiest part to miss, which is the wrong way round
// for the one message that stops the server from starting. Warnings
// already got this treatment from -validate-config; fatals got it
// nowhere.
func Report(problems []Problem) string {
	var b strings.Builder
	for _, p := range problems {
		fmt.Fprintf(&b, "%s  %s  %s: %s\n", p.Severity, p.Code, p.Key, p.Message)
		if p.Applied != "" {
			fmt.Fprintf(&b, "         using %s instead\n", p.Applied)
		}
		if p.Remediation != "" {
			fmt.Fprintf(&b, "         %s\n", p.Remediation)
		}
		if p.Example != "" {
			b.WriteString("\n")
			for _, line := range strings.Split(strings.TrimRight(p.Example, "\n"), "\n") {
				fmt.Fprintf(&b, "           %s\n", line)
			}
			b.WriteString("\n")
		}
		if p.Docs != "" {
			fmt.Fprintf(&b, "         %s\n", p.Docs)
		}
	}
	return b.String()
}

// CheckHint points at the offline checker. Worth saying at the moment of
// failure rather than only in the docs, because a fatal config means the
// container exits -- so `docker exec` into it, the obvious next move,
// answers "container is not running". The checker has to be reached with
// `docker run` instead, and that is not guessable.
const CheckHint = `check a configuration without starting the server:
  mikroview -validate-config
  docker run --rm -e MIKROVIEW_CONFIG=/etc/mikroview/config.yaml \
    -v /path/to/config.yaml:/etc/mikroview/config.yaml:ro <image> -validate-config`

// Result is the outcome of validating a configuration.
type Result struct {
	Fatal    []Problem `json:"fatal"`
	Warnings []Problem `json:"warnings"`
}

// HasProblems reports whether anything at all was found. Used by
// -validate-config, which exits non-zero for either tier: the checker is
// deliberately stricter than the server, because its whole job is to
// catch what the server would tolerate.
func (r Result) HasProblems() bool { return len(r.Fatal) > 0 || len(r.Warnings) > 0 }

// Err returns a single error summarising the fatal problems, or nil.
func (r Result) Err() error {
	if len(r.Fatal) == 0 {
		return nil
	}
	if len(r.Fatal) == 1 {
		return fmt.Errorf("invalid configuration -- %s", r.Fatal[0])
	}
	msg := fmt.Sprintf("invalid configuration -- %d problems:", len(r.Fatal))
	for _, p := range r.Fatal {
		msg += "\n  " + p.String()
	}
	return fmt.Errorf("%s", msg)
}
