// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"regexp"
	"testing"
)

// TestDockerComposeConfigIsOptional covers a v0.6.0 audit finding
// (#1267) against deploy/docker-compose.yml: it used to set
// MIKROVIEW_CONFIG unconditionally, which makes config.yaml mandatory
// rather than optional -- internal/config's load() only treats a
// missing config file as harmless when nothing named one at all
// (configPath == ""); an explicitly named path that does not exist
// "fails, as it always has ... that one is a mistake, not a choice."
// So a first run against an empty app folder (docs/install.md,
// docs/configuration.md: "the folder is optional" / "the file is
// optional") crash-looped instead of starting on defaults, contrary to
// what those docs promise. The compose file has to actually behave
// that way, not merely be documented to.
func TestDockerComposeConfigIsOptional(t *testing.T) {
	data, err := os.ReadFile("deploy/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`(?m)^\s*-\s*MIKROVIEW_CONFIG=`).Match(data) {
		t.Error("deploy/docker-compose.yml sets MIKROVIEW_CONFIG unconditionally, making config.yaml mandatory and crash-looping a first run with an empty app folder -- leave it unset so the app folder's own config.yaml (or its absence) is what decides, the same as internal/config.DefaultConfigPath already does")
	}
}
