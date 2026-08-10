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
	{`"database/sql"`, "a SQL sink. Parameterise everything and re-run the injection audit before this lands"},
	{`jackc/pgx`, "a SQL sink. Only internal/persist talks to the database; anything else reaching for a driver is building queries somewhere it shouldn't"},
	{`"text/template"`, "text/template does not escape for any context; use html/template if a template is ever needed for HTML"},
	{`unsafe.Pointer`, "bypasses the type system, including the bounds checks that make the parser safe on hostile input"},
	{`exec.Command`, "a shell is a command-injection sink"},
}

// allowed lists paths exempt from a specific sink, with the reason.
//
// An exemption is a decision, not an escape hatch: each one says why the
// sink is safe *there*, so the next person can check whether that
// reasoning still holds rather than assuming someone thought about it.
var allowed = map[string][]string{
	// internal/persist talks to Postgres for every store that fits its
	// blob-table shape (#131). Every statement in it is a compile-time
	// constant with $n bound parameters -- nothing is concatenated,
	// formatted, or built from caller input. Re-audited when it landed;
	// see docs/decisions/postgres-backend.md §7 and
	// docs/decisions/injection-audit.md.
	"internal/persist/postgres.go": {`jackc/pgx`},
	// internal/matchlog is the one deliberate exception (#243 slice 6):
	// its data doesn't fit the blob-table shape (see postgres-backend.md
	// §1a), so it gets its own table and its own direct pgx sink rather
	// than going through internal/persist. Held to the same standard --
	// every statement a compile-time constant with $n placeholders -- and
	// re-audited on the same terms; see injection-audit.md's residual
	// risk section.
	"internal/matchlog/postgres.go": {`jackc/pgx`},
}

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
