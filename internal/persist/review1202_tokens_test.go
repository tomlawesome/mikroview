// SPDX-License-Identifier: AGPL-3.0-only

package persist_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/gauntlet"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// #1202 review R12: a token recorded with no kind (v0.1.0 wrote none) is
// listed and authenticates as nothing, on any kind, exactly as before
// #1202 and in gauntlet. Paired with a token that records its kind, which
// authenticates on that kind only.
func TestATokenRecordedWithNoKindIsListedAndAuthenticatesAsNothing(t *testing.T) {
	l := buildLegacy(t, false)
	var entries []map[string]any
	if err := json.Unmarshal(l.tokensJSON, &entries); err != nil {
		t.Fatal(err)
	}
	var kindless string
	for _, e := range entries {
		if e["name"] == "upgrade-fixture-token" {
			delete(e, "kind")
			kindless = l.apiRaw
		}
	}
	if kindless == "" {
		t.Fatal("test setup: no api token in the legacy fixture")
	}
	doc, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tokens.json")
	// Sealed as the previous release sealed its tokens document (#1202
	// review R26: nothing seals a plaintext file on open any more).
	key := freshKey(t)
	if _, err := persist.NewEncryptedFileBackend(path, "tokens", key).Save(context.Background(), doc, 0); err != nil {
		t.Fatal(err)
	}
	ts := openTokens(t, persist.NewEncryptedFileBackend(path, "tokens", key))

	listed := false
	for _, tok := range ts.List() {
		if tok.Name == "upgrade-fixture-token" {
			listed = true
			if tok.Kind != "" {
				t.Errorf("the kindless token was given kind %q on read", tok.Kind)
			}
		}
	}
	if !listed {
		t.Fatalf("the kindless token is not listed: %+v", ts.List())
	}
	now := time.Now()
	for _, k := range []gauntlet.TokenKind{gauntlet.TokenKindAPI, gauntlet.TokenKindIngest, gauntlet.TokenKind("droplist-pull")} {
		if _, ok := ts.Authenticate(kindless, k, now); ok {
			t.Errorf("a token recorded with no kind authenticated as %q", k)
		}
	}
	// Paired: the token that records its kind works on that kind.
	if _, ok := ts.Authenticate(l.ingestRaw, gauntlet.TokenKindIngest, now); !ok {
		t.Error("test premise: the ingest token with a recorded kind does not authenticate")
	}
}
