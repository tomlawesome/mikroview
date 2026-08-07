// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The injection audit (docs/decisions/injection-audit.md) concluded that
// most classic injection classes don't apply to mikroview because the
// sinks simply aren't present -- no SQL, no shell, no HTML assembly.
//
// That conclusion has a shelf life measured in commits. This test is
// what gives it one that doesn't expire: it fails the moment a sink
// appears, rather than waiting for someone to run the audit again.
//
// Same shape as internal/api's authzMatrix -- a decision recorded as a
// test, so adding the thing forces a conscious argument for it instead
// of a silent regression. When one of these genuinely needs to exist,
// add it to allowed below with the reason, and re-audit that path.

type forbiddenSink struct {
	// substring is matched against source text. Deliberately crude:
	// a real parse would be more precise and far easier to fool by
	// aliasing, and the point here is to force a conversation, not to
	// be a static analyser.
	substring string
	why       string
}

var forbiddenGoSinks = []forbiddenSink{
	{`"os/exec"`, "a shell is a command-injection sink; nothing here should be shelling out"},
	{`"database/sql"`, "the first SQL sink. Parameterise everything and re-run the injection audit before this lands (see issue #131)"},
	{`"text/template"`, "text/template does not escape for any context; use html/template if a template is ever needed for HTML"},
	{`unsafe.Pointer`, "bypasses the type system, including the bounds checks that make the parser safe on hostile input"},
	{`exec.Command`, "a shell is a command-injection sink"},
}

// allowed lists paths exempt from a specific sink, with the reason.
// Empty today -- kept so the exemption has an obvious home and shows up
// in review when it gains its first entry.
var allowed = map[string][]string{}

func TestNoForbiddenGoInjectionSinks(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "frontend", "web", "docs", "deploy":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Test files are excluded: this file names every forbidden
		// string, and a test that fails on its own source is useless.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		src := string(data)

		for _, sink := range forbiddenGoSinks {
			if !strings.Contains(src, sink.substring) {
				continue
			}
			if exempt(rel, sink.substring) {
				continue
			}
			t.Errorf("%s introduces %s.\n%s\n\n"+
				"If this is genuinely needed, add it to `allowed` in injection_sinks_test.go with the reason, "+
				"and re-run the injection audit for that path (docs/decisions/injection-audit.md).",
				rel, sink.substring, sink.why)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func exempt(rel, substring string) bool {
	for _, s := range allowed[rel] {
		if s == substring {
			return true
		}
	}
	return false
}
