// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"time"

	"github.com/tomlawesome/mikroview/internal/blcatalogue"
)

// blocklistActivity fills a ledger row's activity -- how often the
// list's rules fired today, and the flags MikroView raised from it.
// The ledger (BUILD.md part 6) fills it in; until then both stay nil.
func (s *Server) blocklistActivity(device string, l blcatalogue.List, row *blocklistLedgerRow, now time.Time) {
}
