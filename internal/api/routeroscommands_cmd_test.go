// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRouterosCommandsCmdMatchesSetupCommandsAPI guards #894's whole
// premise: scripts/routeroscommands is the CHR exercise's only source
// for "the commands the wizard prints", so the exercise and the wizard
// can never quietly drift apart. This runs the actual command -- not
// just the internal/routeros functions it happens to share with this
// handler -- and checks its output is byte-for-byte what
// POST /api/setup/commands renders for the same address and syslog
// port, dialect "a" (the one dialect the table holds today).
func TestRouterosCommandsCmdMatchesSetupCommandsAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to `go run`; skipped under -short")
	}
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	const address = "mv.example.net:8443"
	const syslogPort = ":6514"

	out := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: address, SyslogPort: syslogPort})

	for _, tc := range []struct {
		step string
		want string
	}{
		{"catrust", out.Steps.CaTrust.Commands},
		{"syslog", out.Steps.Syslog.Commands},
		{"ruletagging", out.Steps.RuleTagging.Commands},
	} {
		t.Run(tc.step, func(t *testing.T) {
			got := runRouterosCommandsCmd(t, tc.step, address, syslogPort)
			if got != tc.want {
				t.Errorf("routeroscommands -step=%s output does not match POST /api/setup/commands:\n got:  %q\nwant: %q", tc.step, got, tc.want)
			}
		})
	}
}

// runRouterosCommandsCmd runs the real scripts/routeroscommands command
// via `go run`, rather than calling internal/routeros directly, so this
// test exercises the same flag parsing and output path the CI exercise
// job and the docs both rely on.
func runRouterosCommandsCmd(t *testing.T, step, address, syslogPort string) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "./scripts/routeroscommands",
		"-dialect", "a", "-step", step, "-address", address, "-syslog-port", syslogPort)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run ./scripts/routeroscommands -step=%s: %v\n%s", step, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}
