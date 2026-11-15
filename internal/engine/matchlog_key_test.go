// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/retention"
)

// matchLogTestKey is a usable retention key for tests that open a match
// log, which is sealed and refuses a nil key (#1470).
func matchLogTestKey(t *testing.T) *retention.Key {
	t.Helper()
	k, err := retention.NewKeyFromMaterial([]byte(strings.Repeat("k", retention.MinKeyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}
