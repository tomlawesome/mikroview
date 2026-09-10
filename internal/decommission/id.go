// SPDX-License-Identifier: AGPL-3.0-only

package decommission

import (
	"crypto/rand"
	"encoding/hex"
)

// newID mints a watch id the same way engine.newDefinitionID mints a
// definition id -- 16 random bytes, hex -- rather than deriving one from
// the CIDR. A range can be retired, re-created and retired again, and
// each of those is its own decommission with its own clock and its own
// receipt; an id derived from the range would collide the second one onto
// the first's history.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("decommission: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
