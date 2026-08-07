// SPDX-License-Identifier: AGPL-3.0-only

package logging

import (
	"fmt"
	"os"
)

// banner is mikroview's boot-time wordmark. Plain ASCII (no ANSI
// escapes baked in) so it renders correctly whether or not the output
// is a terminal -- unlike log-line coloring, there's nothing here that
// looks like garbage when piped, so PrintBanner doesn't gate this on
// colorEnabled() the way formatLine does; it only gates the color
// wrapped around it.
const banner = ` __  __ ___ _  _____  _____   _____ _____      __
|  \/  |_ _| |/ / _ \/ _ \ \ / /_ _| __\ \    / /
| |\/| || || ' <|   / (_) \ V / | || _| \ \/\/ /
|_|  |_|___|_|\_\_|_\\___/ \_/ |___|___| \_/\_/`

// PrintBanner writes the boot-time wordmark directly to stdout, once,
// outside the leveled log path -- it isn't a log line (no timestamp/
// level/component), just a startup marker, so it doesn't go through a
// component logger. Not printed for the one-shot CLI modes
// (-healthcheck, -list-users, etc.) -- callers only reach this on the
// real server-start path.
func PrintBanner() {
	if colorEnabled() {
		fmt.Fprintln(os.Stdout, ansiCyan+banner+ansiReset)
		return
	}
	fmt.Fprintln(os.Stdout, banner)
}
