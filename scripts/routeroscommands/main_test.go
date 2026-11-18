// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/routeros"
)

// #1451: -step=blocklist is what the CHR exercise pastes into a router.

func TestBlocklistStepsRendersTheWholeCatalogueForAVersion(t *testing.T) {
	steps, err := blocklistSteps(routeros.MinimumVersion, "")
	if err != nil {
		t.Fatalf("blocklistSteps: %v", err)
	}
	if len(steps) < 5 {
		t.Fatalf("only %d steps", len(steps))
	}
	joined := strings.Join(steps, "\n")
	for _, want := range []string{"mikroview blocklist: spamhaus", blocklistPlaceholderAddress} {
		if !strings.Contains(joined, want) {
			t.Errorf("rendered block lacks %q", want)
		}
	}
	// The placeholder token is a stand-in, never a real credential.
	if !strings.Contains(joined, blocklistPlaceholderToken) {
		t.Error("the block does not carry the placeholder token the push needs")
	}
}

func TestBlocklistStepsUndo(t *testing.T) {
	all, err := blocklistSteps("", "all")
	if err != nil || strings.Join(all, "\n") != routeros.BlocklistUndoAll() {
		t.Errorf("undo all = %v, %v; want BlocklistUndoAll's lines", all, err)
	}
	one, err := blocklistSteps("", "spamhaus")
	if err != nil || len(one) < 2 || !strings.Contains(strings.Join(one, "\n"), "spamhaus") {
		t.Errorf("undo spamhaus = %v, %v", one, err)
	}
	if _, err := blocklistSteps("", "no-such-list"); err == nil {
		t.Error("an unknown list key was accepted")
	}
}

func TestBlocklistStepsRefusesAVersionBelowTheFloor(t *testing.T) {
	for _, v := range []string{"", "6.48", "garbage"} {
		if _, err := blocklistSteps(v, ""); err == nil || !strings.Contains(err.Error(), "-version") {
			t.Errorf("version %q: err = %v, want a -version complaint", v, err)
		}
	}
}
