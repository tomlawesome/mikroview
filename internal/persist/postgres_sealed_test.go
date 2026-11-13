// SPDX-License-Identifier: AGPL-3.0-only

package persist

// #1202 ruling 2 and 11, Postgres half: each auth store's row is wrapped
// in gauntlet's persist.Encrypt under its own label (`auth`, `tokens`,
// `recovery_keys`, `signins`), a plaintext row is refused (#1202 review
// R26, ruling 19: nothing seals a plaintext row on open, so the wrapper is
// never given MigratePlaintext), and a sealed row copied into another
// store's place fails to open.
//
// These run against a real Postgres (the harness in postgres_test.go) and
// skip without MIKROVIEW_TEST_POSTGRES. They exercise mikroview's
// PostgresBackend under gauntlet's wrapper; the wiring in main (which
// label each store gets) is covered by the start-up tests in
// internal/authe2e.

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/tomlawesome/gauntlet"
	gpersist "github.com/tomlawesome/gauntlet/persist"
)

func pgKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func sealedPG(t *testing.T, p *Pool, name string, key []byte) gpersist.Backend {
	t.Helper()
	b, err := gpersist.Encrypt(NewPostgresBackend(p, name), key, gpersist.EncryptOptions{Label: name})
	if err != nil {
		t.Fatalf("Encrypt(%s): %v", name, err)
	}
	return b
}

func pgRow(t *testing.T, p *Pool, name string) (string, bool) {
	t.Helper()
	var payload string
	err := p.Raw().QueryRow(context.Background(), `SELECT payload FROM store_blob WHERE name = $1`, name).Scan(&payload)
	if err != nil {
		return "", false
	}
	return payload, true
}

func openPGAccounts(t *testing.T, b gpersist.Backend, announced *bool) (*gauntlet.Store, error) {
	t.Helper()
	return gauntlet.OpenStore(b, gauntlet.Options{
		OnSetupCode: gauntlet.SetupCodeFunc(func(string) { *announced = true }),
		ProductName: "MikroView",
	})
}

// #1202 B1.5 and B8.35 (Postgres), as changed by R26 (ruling 19): a
// plaintext `auth` row left by an earlier release is refused on open, not
// sealed. The accounts store does not open, announces no setup code (it
// is not an empty store), the error says the data was written without a
// key before v0.7.0 and to restore a sealed copy or move it aside, and
// the row is left byte for byte. Paired: a row sealed under the key opens,
// reads back on a second open, and a wrong key is refused.
func TestPostgresPlaintextAuthRowIsRefusedAndASealedOneReadsBack(t *testing.T) {
	p := newTestPool(t)
	ctx := context.Background()
	key := pgKey(t)

	// A document as an earlier release left it: plaintext JSON in the row.
	mem := gpersist.NewMemory()
	seed, err := gauntlet.OpenStore(mem, gauntlet.Options{OnSetupCode: gauntlet.SetupCodeFunc(func(string) {}), ProductName: "MikroView"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Register("pg-admin", "fixture-only-not-real", time.Now()); err != nil {
		t.Fatal(err)
	}
	snap, err := mem.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPostgresBackend(p, "auth").Save(ctx, snap.Payload, 0); err != nil {
		t.Fatalf("seeding the plaintext row: %v", err)
	}
	planted, _ := pgRow(t, p, "auth")
	if bytes.HasPrefix([]byte(planted), []byte(`{"sealed"`)) {
		t.Fatal("test setup: the seeded row is already sealed")
	}

	var announced bool
	st, err := openPGAccounts(t, sealedPG(t, p, "auth", key), &announced)
	if err == nil {
		t.Fatalf("a plaintext auth row opened, with %d accounts (it was sealed in place)", st.Count())
	}
	// (The refusal text is gauntlet's own persist.Encrypt wrapper's; the
	// ruling-19 wording is held to mikroview's errors in the start-up
	// tests of internal/authe2e.)
	if announced {
		t.Error("a plaintext auth row announced a setup code, as if the store were empty")
	}
	if row, _ := pgRow(t, p, "auth"); row != planted {
		t.Errorf("the plaintext row was sealed or rewritten:\n%.80s", row)
	}

	// Paired: the same account, stored sealed under the key (the way
	// v0.7.0 writes it), opens, announces nothing and reads back.
	other := newTestPool(t)
	var announcedSealed bool
	fresh, err := openPGAccounts(t, sealedPG(t, other, "auth", key), &announcedSealed)
	if err != nil {
		t.Fatalf("opening an empty sealed store: %v", err)
	}
	if _, err := fresh.Register("pg-admin", "fixture-only-not-real", time.Now()); err != nil {
		t.Fatal(err)
	}
	row, ok := pgRow(t, other, "auth")
	if !ok || !bytes.HasPrefix(bytes.TrimSpace([]byte(row)), []byte(`{"sealed"`)) {
		t.Fatalf("the auth row is not {\"sealed\": ...} after a write: %.80s", row)
	}
	if bytes.Contains([]byte(row), []byte("pg-admin")) {
		t.Error("the sealed row contains the username in the clear")
	}
	again, err := openPGAccounts(t, sealedPG(t, other, "auth", key), &announcedSealed)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	if again.Count() != 1 {
		t.Errorf("second open sees %d accounts, want 1", again.Count())
	}
	if _, err := again.Authenticate("pg-admin", "fixture-only-not-real", time.Now()); err != nil {
		t.Errorf("sign-in after a re-open: %v", err)
	}

	// A wrong key is refused and never reads as an empty store.
	var announcedWrong bool
	if s, err := openPGAccounts(t, sealedPG(t, other, "auth", pgKey(t)), &announcedWrong); err == nil {
		t.Errorf("a different key opened the row, with %d accounts", s.Count())
	}
	if announcedWrong {
		t.Error("a wrong key announced a setup code, as if the store were empty")
	}
}

// #1202 B9.42: the sealed `tokens` row copied into the `auth` row's place
// fails to open. It is never read as an accounts document or an empty
// store, and the row is left as it was.
func TestPostgresTokensRowCopiedIntoTheAuthRowFailsToOpen(t *testing.T) {
	p := newTestPool(t)
	ctx := context.Background()
	key := pgKey(t)

	// Both stores written sealed, each under its own label.
	var announced bool
	st, err := openPGAccounts(t, sealedPG(t, p, "auth", key), &announced)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := st.Register("pg-admin", "fixture-only-not-real", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ts, err := gauntlet.OpenTokenStore(sealedPG(t, p, "tokens", key), gauntlet.TokenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ts.Create("a-token", gauntlet.TokenKindAPI, "", admin, time.Now()); err != nil {
		t.Fatal(err)
	}

	tokensRow, ok := pgRow(t, p, "tokens")
	if !ok || tokensRow == "" {
		t.Fatal("test setup: no tokens row")
	}
	authBefore, _ := pgRow(t, p, "auth")

	// Paired case: the untouched auth row opens.
	if s, err := openPGAccounts(t, sealedPG(t, p, "auth", key), new(bool)); err != nil || s.Count() != 1 {
		t.Fatalf("the genuine auth row does not open: %v", err)
	}

	// Copy the tokens row over the auth row (a hostile or careless swap).
	if _, err := p.Raw().Exec(ctx, `UPDATE store_blob SET payload = $1 WHERE name = 'auth'`, tokensRow); err != nil {
		t.Fatal(err)
	}
	var swappedAnnounced bool
	s, err := openPGAccounts(t, sealedPG(t, p, "auth", key), &swappedAnnounced)
	if err == nil {
		t.Fatalf("the tokens row opened as the accounts store, with %d accounts", s.Count())
	}
	if swappedAnnounced {
		t.Error("the swapped row announced a setup code, as if the store were empty")
	}
	if after, _ := pgRow(t, p, "auth"); after != tokensRow {
		t.Error("a refused open changed the row")
	}
	_ = authBefore
}
