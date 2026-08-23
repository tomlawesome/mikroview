// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"os/exec"
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

// sources lists the files matching pathspec that git considers part of
// the working tree -- tracked files plus untracked ones, minus anything
// gitignored.
//
// Both sweeps below enumerate from git rather than walking the working
// tree, and that is load-bearing (#520). This project puts linked git
// worktrees inside the repo at .claude/worktrees/, which is gitignored
// and so invisible to git but fully visible to a walk. A walk therefore
// finds a second copy of every source file, at a path like
// .claude/worktrees/<name>/internal/persist/postgres.go -- which does
// not match the `internal/persist/postgres.go` key in allowed, so a
// legitimately exempt file failed the gate for no reason beyond another
// session having a branch checked out. ci.yml's gofmt step already
// derives its file list from git for comparable reasons.
//
// The danger that motivated the fix was not the noise itself: it is that
// the quickest way to silence it is to add a worktree path to `allowed`,
// which would loosen the real gate to paper over local directory layout.
//
// --others --exclude-standard rather than tracked files alone, so a
// newly written file is swept before it is ever staged. Ignored paths
// are the only thing dropped, which is exactly the nested-worktree copy
// and never anything that can reach production.
//
// Fails closed. If git cannot be reached, or matches nothing, the gate
// errors rather than sweeping zero files and reporting success -- the
// same reasoning as check-bundle-budget.mjs treating "nothing to
// measure" as a failure.
func sources(t *testing.T, pathspec ...string) []string {
	t.Helper()

	args := append([]string{"ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}, pathspec...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatalf("git ls-files %v: %v\n\n"+
			"This gate derives its file list from git on purpose; see the comment on sources() and #520.",
			pathspec, err)
	}

	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		t.Fatalf("git ls-files %v matched no files -- refusing to pass on an empty sweep", pathspec)
	}
	return files
}

func TestNoForbiddenGoInjectionSinks(t *testing.T) {
	for _, rel := range sources(t, "*.go") {
		// Test files are excluded: this file names every forbidden
		// string, and a test that fails on its own source is useless.
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}

		data, err := os.ReadFile(rel)
		if err != nil {
			t.Fatal(err)
		}
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

// TestNoHTMLInjectionSinksInTheFrontend guards the property the Go
// sweep above deliberately skips: the frontend directory. The
// no-{@html} claim is load-bearing -- it is why device ids, rule
// labels, host names and every other value that arrives from a router
// or a config file can be rendered as text without escaping logic of
// our own -- and until now nothing failed if someone added one.
//
// Svelte auto-escapes {expression}; {@html} opts out entirely, and
// innerHTML/insertAdjacentHTML/outerHTML do the same from plain TS.
func TestNoHTMLInjectionSinksInTheFrontend(t *testing.T) {
	forbidden := []string{"{@html", "innerHTML", "outerHTML", "insertAdjacentHTML"}

	for _, rel := range sources(t, "frontend/src/*.svelte", "frontend/src/*.ts") {
		data, err := os.ReadFile(rel)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range forbidden {
			if strings.Contains(string(data), bad) {
				t.Errorf("%s contains %q -- it bypasses Svelte's escaping. "+
					"Render untrusted values as text; if this is genuinely necessary, "+
					"it needs an entry in docs/decisions/injection-audit.md first.", rel, bad)
			}
		}
	}
}
