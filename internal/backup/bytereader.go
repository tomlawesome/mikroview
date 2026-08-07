// SPDX-License-Identifier: AGPL-3.0-only

package backup

import "bytes"

// newByteReader exists so Read can hand json.Decoder something it can ask
// for a second value from, after the whole (already length-checked)
// payload is in memory.
func newByteReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
