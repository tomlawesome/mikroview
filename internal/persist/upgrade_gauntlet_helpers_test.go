// SPDX-License-Identifier: AGPL-3.0-only

// upgrade_gauntlet_helpers_test.go is the shared half of #1202's
// migration test (build notes section C): the checks every recorded
// release, and the synthetic stand-in below, must pass once gauntlet
// opens the stored accounts and tokens documents in place.
//
// Written from the specification before the code exists. Names the build
// provides (see also seal_in_place_test.go):
//
//	persist.CurrentSchema() == 4  ("accounts and tokens on gauntlet")
//
// and, for these backends to be handed to gauntlet at all, mikroview's
// persist.Snapshot must be gauntlet's persist.Snapshot (an alias), so
// that mikroview's backends satisfy gauntlet's persist.Backend.
//
// #1202 review R26 (ruling 19): there is no sealing in place. Everything
// runGauntletChecks opens is already sealed (a recording made with a key,
// or a stand-in sealed the way the previous release sealed it), and a
// plaintext document goes through runPlaintextRefusalChecks instead.
package persist_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/gauntlet"
	gpersist "github.com/tomlawesome/gauntlet/persist"

	"github.com/tomlawesome/mikroview/internal/retention"
)

// gauntletCase is one recording (or the synthetic stand-in) to check.
type gauntletCase struct {
	// accounts and tokens return a fresh backend over the same stored
	// document each call, as a restart would.
	accounts func() gpersist.Backend
	tokens   func() gpersist.Backend
	// rawAccounts and rawTokens return the bytes as stored, without any
	// decryption: what a disk or a database dump would show.
	rawAccounts func() []byte
	rawTokens   func() []byte

	admin, viewer string
	apiTokenName  string
	// apiTokenKindless: the recorded token predates token kinds (v0.1.0
	// recorded none). #1202 review R12: it is listed, carries no kind, and
	// authenticates as nothing -- as before #1202 and in gauntlet. Spec
	// step C.5 ("kinds default to api") was wrong for v0.1.0.
	apiTokenKindless bool
	// totp: the manifest says an authenticator app was enrolled.
	totp bool
}

const roundTripPassword = "a-fresh-long-passphrase-7Qz!"

// openAccounts opens the accounts store the way the server will: never
// with AllowPlaintextAtRest, with the setup code announcement recorded
// so a test can see that an existing install announced nothing.
func openAccounts(t *testing.T, b gpersist.Backend, announced *bool) *gauntlet.Store {
	t.Helper()
	st, err := gauntlet.OpenStore(b, gauntlet.Options{
		OnSetupCode:  gauntlet.SetupCodeFunc(func(string) { *announced = true }),
		OnUnlockCode: gauntlet.UnlockCodeFunc(func(string, string) {}),
		ProductName:  "MikroView",
	})
	if err != nil {
		t.Fatalf("gauntlet.OpenStore on the stored accounts document: %v", err)
	}
	return st
}

func openTokens(t *testing.T, b gpersist.Backend) *gauntlet.TokenStore {
	t.Helper()
	ts, err := gauntlet.OpenTokenStore(b, gauntlet.TokenOptions{
		Kinds: []gauntlet.TokenKind{gauntlet.TokenKindAPI, gauntlet.TokenKindIngest, gauntlet.TokenKind("droplist-pull")},
	})
	if err != nil {
		t.Fatalf("gauntlet.OpenTokenStore on the stored tokens document: %v", err)
	}
	return ts
}

// decryptedDocument loads the document through a fresh backend, which
// decrypts it, and returns the plaintext JSON object's top-level keys.
func decryptedDocument(t *testing.T, b gpersist.Backend) map[string]json.RawMessage {
	t.Helper()
	snap, err := b.Load(t.Context())
	if err != nil {
		t.Fatalf("loading the stored document: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(snap.Payload, &doc); err != nil {
		t.Fatalf("the stored document is not a JSON object: %v\n%s", err, snap.Payload)
	}
	return doc
}

func docVersion(t *testing.T, doc map[string]json.RawMessage) int {
	t.Helper()
	raw, ok := doc["version"]
	if !ok {
		t.Fatalf("the document carries no \"version\" key; keys: %v", keysOf(doc))
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("version is not a number: %s", raw)
	}
	return v
}

func keysOf(m map[string]json.RawMessage) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// runGauntletChecks is build notes C steps 2 to 6.
func runGauntletChecks(t *testing.T, c gauntletCase) {
	t.Helper()
	now := time.Now()

	// Step 2. The stored bytes before the first open: already sealed
	// (ruling 19), so opening them changes nothing.
	beforeAccounts, beforeTokens := c.rawAccounts(), c.rawTokens()
	if json.Valid(beforeAccounts) || json.Valid(beforeTokens) {
		t.Fatal("test setup: a document handed to runGauntletChecks is plaintext; those go to runPlaintextRefusalChecks")
	}

	var announced bool
	st := openAccounts(t, c.accounts(), &announced)
	if announced {
		t.Error("opening an existing install announced a setup code")
	}
	if st.Count() != 2 {
		t.Errorf("Count() = %d, want 2 (the recorded admin and viewer)", st.Count())
	}
	if afterAccounts := c.rawAccounts(); !bytes.Equal(beforeAccounts, afterAccounts) {
		t.Error("an already-sealed accounts document changed on open; it must open as it is")
	}

	// Step 3. Passwords.
	if _, err := st.Authenticate(c.admin, fixtureAdminPassword, now); err != nil {
		t.Errorf("admin %q cannot sign in: %v", c.admin, err)
	}
	if _, err := st.Authenticate(c.viewer, fixtureAdminPassword, now); err != nil {
		t.Errorf("viewer %q cannot sign in: %v", c.viewer, err)
	}
	if _, err := st.Authenticate(c.admin, "definitely-not-the-password", now); err == nil {
		t.Error("a wrong password signed the admin in")
	}

	// Step 4. The authenticator app.
	admin, ok := st.ByUsername(c.admin)
	if !ok {
		t.Fatalf("admin %q not found", c.admin)
	}
	if got := admin.HasActiveTOTP(); got != c.totp {
		t.Errorf("admin HasActiveTOTP() = %v, want %v (manifest secondFactorEnrolled)", got, c.totp)
	}
	if got := admin.HasSecondFactor(); got != c.totp {
		t.Errorf("admin HasSecondFactor() = %v, want %v", got, c.totp)
	}

	// Step 5. Tokens.
	ts := openTokens(t, c.tokens())
	if !bytes.Equal(beforeTokens, c.rawTokens()) {
		t.Error("an already-sealed tokens document changed on open; it must open as it is")
	}
	found := false
	for _, tok := range ts.List() {
		if tok.Name != c.apiTokenName {
			continue
		}
		if c.apiTokenKindless {
			// #1202 review R12: listed, with no kind, so no kind's
			// route accepts it.
			if tok.Kind != "" {
				t.Errorf("the recorded token %q carries kind %q; a token recorded with no kind must keep none and authenticate as nothing", tok.Name, tok.Kind)
			}
			found = true
		} else if tok.Kind == gauntlet.TokenKindAPI {
			found = true
		}
	}
	if !found {
		t.Errorf("no api token named %q (kindless: %v) in the opened tokens store: %+v", c.apiTokenName, c.apiTokenKindless, ts.List())
	}
	if _, ok := ts.Authenticate(randomHex(t, 16), gauntlet.TokenKindAPI, now); ok {
		t.Error("a random string authenticated as an api token")
	}

	// Step 6. Round trip: one change, re-open, and the new document
	// version is what is stored.
	if err := st.SetPassword(c.viewer, roundTripPassword, now); err != nil {
		t.Fatalf("SetPassword on the viewer: %v", err)
	}
	var announced2 bool
	st2 := openAccounts(t, c.accounts(), &announced2)
	if _, err := st2.Authenticate(c.viewer, roundTripPassword, now); err != nil {
		t.Errorf("the viewer's new password does not verify after re-open: %v", err)
	}
	if _, err := st2.Authenticate(c.viewer, fixtureAdminPassword, now); err == nil {
		t.Error("the viewer's old password still verifies after the change")
	}
	if _, err := st2.Authenticate(c.admin, fixtureAdminPassword, now); err != nil {
		t.Errorf("the admin cannot sign in after re-open: %v", err)
	}
	admin2, _ := st2.ByUsername(c.admin)
	if admin2 == nil || admin2.HasActiveTOTP() != c.totp {
		t.Errorf("the admin's authenticator app state changed across the round trip")
	}
	if v := docVersion(t, decryptedDocument(t, c.accounts())); v != 10 {
		t.Errorf("accounts document version after the first save = %d, want 10", v)
	}

	creator, _ := st2.ByUsername(c.admin)
	if _, _, err := ts.Create("round-trip", gauntlet.TokenKindAPI, "", creator, now); err != nil {
		t.Fatalf("creating a token: %v", err)
	}
	ts2 := openTokens(t, c.tokens())
	names := map[string]bool{}
	for _, tok := range ts2.List() {
		names[tok.Name] = true
	}
	if !names[c.apiTokenName] || !names["round-trip"] {
		t.Errorf("after re-open the tokens listed are %v; want both %q and \"round-trip\"", names, c.apiTokenName)
	}
	if v := docVersion(t, decryptedDocument(t, c.tokens())); v != 3 {
		t.Errorf("tokens document version after the first save = %d, want 3", v)
	}
}

// plaintextCase is a stored accounts document and tokens document that
// are plaintext JSON, written before v0.7.0 without a key.
type plaintextCase struct {
	accounts, tokens       func() gpersist.Backend
	rawAccounts, rawTokens func() []byte
	// gauntletWords: the refusal comes from gauntlet's own persist.Encrypt
	// wrapper (Postgres rows), whose wording is gauntlet's and not
	// mikroview's to choose, so only the file backend's refusal, which is
	// mikroview's ErrPlaintextDocument, is held to the ruling-19 wording.
	gauntletWords bool
}

// #1202 review R26 (ruling 19). Whatever the schema and whichever way the
// plaintext got there (a keyless v0.6 install, an old recording), opening
// it is refused: gauntlet's open fails for both stores, never an empty
// store and never a setup code, the error says the data was written
// without a key before v0.7.0 and to restore a sealed copy or move it
// aside, and the stored bytes are exactly as they were.
func runPlaintextRefusalChecks(t *testing.T, c plaintextCase) {
	t.Helper()
	beforeAccounts, beforeTokens := c.rawAccounts(), c.rawTokens()
	if !json.Valid(beforeAccounts) || !json.Valid(beforeTokens) {
		t.Fatal("test setup: the stored accounts and tokens documents should both be plaintext JSON")
	}

	var announced bool
	st, err := gauntlet.OpenStore(c.accounts(), gauntlet.Options{
		OnSetupCode:  gauntlet.SetupCodeFunc(func(string) { announced = true }),
		OnUnlockCode: gauntlet.UnlockCodeFunc(func(string, string) {}),
		ProductName:  "MikroView",
	})
	if err == nil {
		t.Errorf("gauntlet.OpenStore opened a plaintext accounts document, with %d accounts (it was sealed in place)", st.Count())
	} else {
		if !c.gauntletWords {
			wantRuling19Text(t, "accounts", err.Error())
		}
	}
	if announced {
		t.Error("a plaintext accounts document announced a setup code, as if the store were empty")
	}
	if got := c.rawAccounts(); !bytes.Equal(got, beforeAccounts) {
		t.Error("the plaintext accounts document was changed (sealed or rewritten)")
	}

	ts, err := gauntlet.OpenTokenStore(c.tokens(), gauntlet.TokenOptions{
		Kinds: []gauntlet.TokenKind{gauntlet.TokenKindAPI, gauntlet.TokenKindIngest, gauntlet.TokenKind("droplist-pull")},
	})
	if err == nil {
		t.Errorf("gauntlet.OpenTokenStore opened a plaintext tokens document, listing %d tokens (it was sealed in place)", len(ts.List()))
	} else {
		if !c.gauntletWords {
			wantRuling19Text(t, "tokens", err.Error())
		}
	}
	if got := c.rawTokens(); !bytes.Equal(got, beforeTokens) {
		t.Error("the plaintext tokens document was changed (sealed or rewritten)")
	}
}

// wantRuling19Text is the wording check of seal_in_place_test.go, for
// this package.
func wantRuling19Text(t *testing.T, what, msg string) {
	t.Helper()
	low := strings.ToLower(msg)
	for _, w := range []string{"without a key", "v0.7.0", "restore a sealed copy", "move it aside"} {
		if !strings.Contains(low, w) {
			t.Errorf("%s: the refusal does not say %q:\n%s", what, w, msg)
		}
	}
	for _, w := range []string{"first start of v0.7.0", "seals it", "will seal", "normal start"} {
		if strings.Contains(low, w) {
			t.Errorf("%s: the refusal still says %q:\n%s", what, w, msg)
		}
	}
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// legacy is the stand-in for a copy of real stored data when no
// recordings have been fetched: the accounts and tokens documents in the
// shape a v0.6.x release wrote them -- an unversioned {"users": [...]}
// object and a bare token list -- built with gauntlet's own password
// hashing (the hash format is the same, build notes "Facts"), then cut
// down to the fields mikroview's own User had.
type legacy struct {
	accountsJSON []byte
	tokensJSON   []byte
	admin        string
	viewer       string
	apiRaw       string
	ingestRaw    string
	dropRaw      string
}

var legacyUserFields = map[string]bool{
	"id": true, "username": true, "passwordHash": true, "role": true,
	"createdAt": true, "lastLogin": true, "hasLocalPassword": true,
	"totpSecret": true, "totpConfirmedAt": true, "totpLastCounter": true,
	"recoveryCodes": true, "passkeys": true,
}

func buildLegacy(t *testing.T, withTOTP bool) legacy {
	t.Helper()
	now := time.Now().Add(-24 * time.Hour)
	mem := gpersist.NewMemory()
	var announced bool
	st, err := gauntlet.OpenStore(mem, gauntlet.Options{
		OnSetupCode:  gauntlet.SetupCodeFunc(func(string) { announced = true }),
		OnUnlockCode: gauntlet.UnlockCodeFunc(func(string, string) {}),
		ProductName:  "MikroView",
	})
	if err != nil {
		t.Fatalf("building the stand-in: %v", err)
	}
	l := legacy{admin: "upgrade-fixture-admin", viewer: "upgrade-fixture-viewer"}
	admin, err := st.Register(l.admin, fixtureAdminPassword, now)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := st.CreateUser(l.viewer, fixtureAdminPassword, gauntlet.RoleViewer, now); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if withTOTP {
		if err := st.SetPendingTOTPSecret(admin.ID, "JBSWY3DPEHPK3PXP"); err != nil {
			t.Fatalf("SetPendingTOTPSecret: %v", err)
		}
		if err := st.ConfirmTOTP(admin.ID, now, 1); err != nil {
			t.Fatalf("ConfirmTOTP: %v", err)
		}
		if _, err := st.GenerateRecoveryCodes(admin.ID, now); err != nil {
			t.Fatalf("GenerateRecoveryCodes: %v", err)
		}
	}
	snap, err := mem.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Users []map[string]json.RawMessage `json:"users"`
	}
	if err := json.Unmarshal(snap.Payload, &doc); err != nil {
		t.Fatalf("stand-in document: %v", err)
	}
	var users []map[string]json.RawMessage
	for _, u := range doc.Users {
		keep := map[string]json.RawMessage{}
		for k, v := range u {
			if legacyUserFields[k] {
				keep[k] = v
			}
		}
		users = append(users, keep)
	}
	l.accountsJSON, err = json.Marshal(map[string]any{"users": users})
	if err != nil {
		t.Fatal(err)
	}

	// The tokens document: a bare list, raw values 32 hex with no prefix,
	// hashed with SHA-256 as the previous release did.
	l.apiRaw, l.ingestRaw, l.dropRaw = randomHex(t, 16), randomHex(t, 16), randomHex(t, 16)
	hash := func(raw string) string {
		sum := sha256.Sum256([]byte(raw))
		return hex.EncodeToString(sum[:])
	}
	made := now.UTC().Format(time.RFC3339)
	l.tokensJSON, err = json.Marshal([]map[string]any{
		{"id": randomHex(t, 16), "name": "upgrade-fixture-token", "kind": "api", "hashedValue": hash(l.apiRaw), "createdAt": made},
		{"id": randomHex(t, 16), "name": "upgrade-fixture-router", "kind": "ingest", "device": "core-router", "hashedValue": hash(l.ingestRaw), "createdAt": made},
		{"id": randomHex(t, 16), "name": "upgrade-fixture-droplist", "kind": "droplist-pull", "hashedValue": hash(l.dropRaw), "createdAt": made},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = announced
	return l
}

func writeFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// freshKey is a throwaway key made at run time; nothing about it is
// committed.
func freshKey(t *testing.T) *retention.Key {
	t.Helper()
	material := make([]byte, retention.MinKeyBytes)
	if _, err := rand.Read(material); err != nil {
		t.Fatal(err)
	}
	k, err := retention.NewKeyFromMaterial(material)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
