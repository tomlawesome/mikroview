// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
)

// A crashing input found by the fuzz gate is written by Go to
// testdata/fuzz/<Target>/ inside the target's own package directory,
// which on a CI runner is discarded with the workspace. ci.yml's
// upload-artifact step rescues it, and reaches it with the glob below
// (#526).
//
// The glob is what these tests exist for. It has a single `*`, so it
// reaches internal/<pkg> and nothing else: a fuzz target added at the
// repository root, or one package deeper at internal/a/b, would be
// fuzzed by CI and its crasher silently thrown away. Nothing else would
// report that -- the gate would still be green, and the loss only shows
// up on the one run that matters, years later, when a real crash is
// found and lost.
//
// Same shape as injection_sinks_test.go: a decision recorded as a test,
// so moving a target or editing the glob forces a conscious argument
// rather than a silent regression.
const fuzzCrasherUploadGlob = "internal/*/testdata/fuzz/**"

// Matched at line start so a mention in a comment or a string is not a
// declaration. Crude on purpose, for the reasons injection_sinks_test.go
// gives: the point is to force a conversation, not to be a parser.
var fuzzTargetDecl = regexp.MustCompile(`(?m)^func (Fuzz[A-Za-z0-9_]*)\(`)

func TestFuzzTargetsSitWhereTheCrasherUploadReaches(t *testing.T) {
	targets := 0

	for _, rel := range sources(t, "*_test.go") {
		data, err := os.ReadFile(rel)
		if err != nil {
			t.Fatal(err)
		}

		for _, m := range fuzzTargetDecl.FindAllStringSubmatch(string(data), -1) {
			targets++
			name := m[1]
			dir := path.Dir(rel)

			segments := strings.Split(dir, "/")
			if len(segments) == 2 && segments[0] == "internal" {
				continue
			}

			t.Errorf("%s declares %s in %q, which %q does not reach.\n"+
				"CI would fuzz it and discard any crashing input with the runner's workspace.\n\n"+
				"Either move the target under internal/<pkg>/, or widen the upload path in "+
				".github/workflows/ci.yml and update fuzzCrasherUploadGlob here to match.",
				rel, name, dir, fuzzCrasherUploadGlob)
		}
	}

	// Fails closed, like sources() itself: a sweep that finds no targets
	// means the regexp or the pathspec has drifted, not that the gate
	// passed.
	if targets == 0 {
		t.Fatal("found no fuzz targets at all -- refusing to pass on an empty sweep. " +
			"If the last target was genuinely removed, delete this file and the upload step in ci.yml.")
	}
}

func TestCIRescuesFuzzCrashers(t *testing.T) {
	const workflow = ".github/workflows/ci.yml"

	data, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	// Each of these is load-bearing separately: the action does the
	// rescuing, the glob decides what it reaches, and the condition is
	// why a green run costs nothing. Losing any one of them loses the
	// crasher just as completely as deleting the step.
	for _, required := range []struct {
		substring string
		why       string
	}{
		{"actions/upload-artifact", "nothing carries the crashing input off the runner"},
		{fuzzCrasherUploadGlob, "the upload no longer reaches where Go writes a crasher"},
		{"if: failure()", "the step would run on green builds too, or not on red ones"},
	} {
		if !strings.Contains(src, required.substring) {
			t.Errorf("%s no longer contains %q -- %s.\n\n"+
				"If the rescue step was deliberately changed, update this test and "+
				"fuzzCrasherUploadGlob to match what it now does. See #526.",
				workflow, required.substring, required.why)
		}
	}
}
