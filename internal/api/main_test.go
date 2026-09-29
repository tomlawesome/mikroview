// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"os"
	"testing"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// TestMain drops Argon2id to the cheapest valid cost (auth.KDFParams.
// Valid's floor: 8 MiB, one pass, one thread) for this package's whole
// test binary, before any test runs. See auth.SetHashParamsForTest's
// doc comment for why the override is safe here -- this call happens
// single-threaded, before m.Run() starts any test goroutine -- and why
// it never reaches production: nothing outside test code calls it, and
// internal/auth's own test suite (password_test.go,
// register_cost_test.go) never does either, so it keeps exercising
// HashPassword at the real, unchanged production cost.
//
// #1392: this package's fixtures (buildSharedAdmin, passkeyTestServer,
// totpTestServer, and every login that goes through the real routes)
// each pay a full production Argon2id hash -- 64 MiB, three passes --
// and confirming a second factor mints ten more hashing recovery
// codes. Measured at 600ms-1s per hash under -race on this host, and
// this package hashes enough of them that it alone pushed past go
// test's ten-minute default timeout.
func TestMain(m *testing.M) {
	restore := auth.SetHashParamsForTest(auth.KDFParams{Memory: 8 * 1024, Time: 1, Threads: 1})
	code := m.Run()
	restore()
	os.Exit(code)
}
