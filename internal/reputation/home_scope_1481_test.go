// SPDX-License-Identifier: AGPL-3.0-only

package reputation

import (
	"context"
	"errors"
	"testing"

	"github.com/tomlawesome/mikroview/internal/ipscope"
)

// Assumed API: (*Client).SetHomeScope(*ipscope.Scope). A home address is
// refused before any outbound request, so this test makes no network call.
func TestLookupRefusesAConfiguredHomeRangeAsNotPublic(t *testing.T) {
	sc, err := ipscope.New([]string{"198.51.100.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	c := New("")
	c.SetHomeScope(sc)
	_, err = c.Lookup(context.Background(), "198.51.100.66")
	if !errors.Is(err, ErrNotPublic) {
		t.Errorf("Lookup(198.51.100.66) with home 198.51.100.0/24 = %v, want ErrNotPublic", err)
	}
}
