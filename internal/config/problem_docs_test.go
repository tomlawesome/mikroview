// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every problem code MikroView can emit must have a section in
// docs/configuration.md to land on.
//
// docsAnchor builds a deep link from the code itself, so a code with no
// matching heading sends the operator to a page that scrolls nowhere --
// and nothing complained. CFG-0050 and CFG-0051 had been in that state
// since they were added (#267 finding 14 turned this up while adding
// three more).
//
// The obligation is recorded as something that fails, the same way
// THIRD-PARTY-NOTICES.md and internal/api's authzMatrix are.
func TestEveryProblemCodeIsDocumented(t *testing.T) {
	source, err := os.ReadFile("validate.go")
	if err != nil {
		t.Fatalf("reading validate.go: %v", err)
	}
	docs, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatalf("reading the configuration reference: %v", err)
	}

	emitted := map[string]bool{}
	for _, m := range regexp.MustCompile(`"(CFG-\d{4})"`).FindAllStringSubmatch(string(source), -1) {
		emitted[m[1]] = true
	}
	if len(emitted) == 0 {
		t.Fatal("found no problem codes in validate.go -- this test is not looking where it thinks it is")
	}

	documented := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^#+ (CFG-\d{4})`).FindAllStringSubmatch(string(docs), -1) {
		documented[m[1]] = true
	}

	var missing []string
	for code := range emitted {
		if !documented[code] {
			missing = append(missing, code)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d problem code(s) have no section in docs/configuration.md, so their own Docs link goes nowhere: %s",
			len(missing), strings.Join(missing, ", "))
	}
}

// The paste-ready snippet is what an operator acts on, so a code without
// one gives them a message and no next step.
func TestEveryProblemCodeHasAnExample(t *testing.T) {
	source, err := os.ReadFile("validate.go")
	if err != nil {
		t.Fatalf("reading validate.go: %v", err)
	}
	var missing []string
	for _, m := range regexp.MustCompile(`"(CFG-\d{4})"`).FindAllStringSubmatch(string(source), -1) {
		if _, ok := examplesByCode[m[1]]; !ok {
			missing = append(missing, m[1])
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d problem code(s) have no examplesByCode snippet: %s", len(missing), strings.Join(missing, ", "))
	}
}
