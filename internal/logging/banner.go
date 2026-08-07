// SPDX-License-Identifier: AGPL-3.0-only

package logging

import (
	"fmt"
	"os"
	"strings"
)

// The boot banner, split into backdrop and wordmark so the two can be
// coloured differently -- a dim starfield behind a bright wordmark reads
// as depth, where one flat colour reads as a wall of blocks.
//
// Its job is to make a restart unmissable when you are scrolling back
// through `docker logs` looking for where the container came up. That is
// why it is deliberately large and deliberately unlike every other line
// mikroview prints.
//
// Box-drawing and block characters rather than plain ASCII: the bevelled
// ╗╝ edges are what make the letters look extruded, and there is no
// ASCII equivalent that does. This is not a new dependency on UTF-8 --
// every log line already separates its columns with │ (see formatLine),
// so a terminal that mangles this would already be mangling the rest of
// the output.
const (
	bannerTopRule    = `─────────────────────────────────────────────────────────────────────────────`
	bannerBottomRule = bannerTopRule
)

// bannerSkyAbove/bannerSkyBelow are the backdrop. Placement is
// deliberately irregular -- evenly spaced stars read as a pattern, and a
// pattern doesn't read as sky.
var (
	bannerSkyAbove = []string{
		`      ·            ✦             ·                  ·           ✦      ·`,
		`  ✦        ·               ○                ·             ·          ◯`,
	}
	bannerSkyBelow = []string{
		`    ·          ◯              ·                ✦             ·           ·`,
		`         ✦             ·              ·                ○          ·`,
	}

	// bannerWordmark is MIKROVIEW in an extruded block face.
	bannerWordmark = []string{
		`    ███╗   ███╗██╗██╗  ██╗██████╗  ██████╗ ██╗   ██╗██╗███████╗██╗    ██╗`,
		`    ████╗ ████║██║██║ ██╔╝██╔══██╗██╔═══██╗██║   ██║██║██╔════╝██║    ██║`,
		`    ██╔████╔██║██║█████╔╝ ██████╔╝██║   ██║██║   ██║██║█████╗  ██║ █╗ ██║`,
		`    ██║╚██╔╝██║██║██╔═██╗ ██╔══██╗██║   ██║╚██╗ ██╔╝██║██╔══╝  ██║███╗██║`,
		`    ██║ ╚═╝ ██║██║██║  ██╗██║  ██║╚██████╔╝ ╚████╔╝ ██║███████╗╚███╔███╔╝`,
		`    ╚═╝     ╚═╝╚═╝╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝   ╚═══╝  ╚═╝╚══════╝ ╚══╝╚══╝ `,
	}
)

// PrintBanner writes the boot-time wordmark directly to stdout, once,
// outside the leveled log path -- it isn't a log line (no timestamp/
// level/component), just a startup marker, so it doesn't go through a
// component logger. Not printed for the one-shot CLI modes
// (-healthcheck, -list-users, etc.) -- callers only reach this on the
// real server-start path.
//
// Colour is applied per-band rather than around the whole thing, and is
// dropped entirely off a TTY or under NO_COLOR (see colorEnabled) -- the
// art itself carries no escape sequences, so `docker logs` and a log
// collector get the same shape, just without the colour.
func PrintBanner() {
	var b strings.Builder

	color := colorEnabled()
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	bright := func(s string) string {
		if !color {
			return s
		}
		return ansiCyan + s + ansiReset
	}

	b.WriteString(dim(bannerTopRule) + "\n")
	for _, line := range bannerSkyAbove {
		b.WriteString(dim(line) + "\n")
	}
	for _, line := range bannerWordmark {
		b.WriteString(bright(line) + "\n")
	}
	for _, line := range bannerSkyBelow {
		b.WriteString(dim(line) + "\n")
	}
	b.WriteString(dim(bannerBottomRule))

	fmt.Fprintln(os.Stdout, b.String())
}
