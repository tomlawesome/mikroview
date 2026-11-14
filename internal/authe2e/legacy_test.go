// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// A data directory as an earlier release left it, for the tests that
// start the new binary over existing data (B1.8, B8.35, B9.40, B9.41).
// Built with gauntlet's own password hashing (the hash format is the same
// as mikroview's, design record "Facts") and cut down to the fields
// mikroview's User had; never a recording and never a real credential.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/gauntlet"
	gpersist "github.com/tomlawesome/gauntlet/persist"

	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

type legacyInstall struct {
	root, keyPath string
	admin         *acct
	// tokens maps kind to the raw value (32 hex, no prefix).
	tokens map[string]string
	// accounts and tokenDoc are the plaintext documents.
	accounts, tokenDoc []byte
}

var legacyUserFields = map[string]bool{
	"id": true, "username": true, "passwordHash": true, "role": true,
	"createdAt": true, "lastLogin": true, "hasLocalPassword": true,
	"totpSecret": true, "totpConfirmedAt": true, "totpLastCounter": true,
	"recoveryCodes": true, "passkeys": true,
}

func randHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func buildLegacy(t *testing.T) *legacyInstall {
	t.Helper()
	root := t.TempDir()
	l := &legacyInstall{root: root, keyPath: filepath.Join(root, "keys", "history.key"), tokens: map[string]string{}}
	writeKey(t, l.keyPath)

	now := time.Now().Add(-48 * time.Hour)
	mem := gpersist.NewMemory()
	st, err := gauntlet.OpenStore(mem, gauntlet.Options{
		OnSetupCode: gauntlet.SetupCodeFunc(func(string) {}), OnUnlockCode: gauntlet.UnlockCodeFunc(func(string, string) {}),
		ProductName: "MikroView",
	})
	if err != nil {
		t.Fatalf("building the legacy accounts: %v", err)
	}
	admin, err := st.Register("mv-admin", adminPassword, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser("mv-viewer", "viewer-pw-9X7q!", gauntlet.RoleViewer, now); err != nil {
		t.Fatal(err)
	}
	const secret = "JBSWY3DPEHPK3PXP"
	if err := st.SetPendingTOTPSecret(admin.ID, secret); err != nil {
		t.Fatal(err)
	}
	if err := st.ConfirmTOTP(admin.ID, now, 1); err != nil {
		t.Fatal(err)
	}
	codes, err := st.GenerateRecoveryCodes(admin.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	l.admin = &acct{name: "mv-admin", pass: adminPassword, secret: secret, codes: codes}

	snap, err := mem.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Users []map[string]json.RawMessage `json:"users"`
	}
	must(t, json.Unmarshal(snap.Payload, &doc))
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
	l.accounts, err = json.Marshal(map[string]any{"users": users})
	must(t, err)

	hash := func(raw string) string {
		sum := sha256.Sum256([]byte(raw))
		return hex.EncodeToString(sum[:])
	}
	made := now.UTC().Format(time.RFC3339)
	l.tokens["api"], l.tokens["ingest"], l.tokens["droplist-pull"] = randHex(t, 16), randHex(t, 16), randHex(t, 16)
	l.tokenDoc, err = json.Marshal([]map[string]any{
		{"id": randHex(t, 16), "name": "legacy-api", "kind": "api", "hashedValue": hash(l.tokens["api"]), "createdAt": made},
		{"id": randHex(t, 16), "name": "legacy-router", "kind": "ingest", "device": "core-router", "hashedValue": hash(l.tokens["ingest"]), "createdAt": made},
		{"id": randHex(t, 16), "name": "legacy-droplist", "kind": "droplist-pull", "hashedValue": hash(l.tokens["droplist-pull"]), "createdAt": made},
	})
	must(t, err)
	return l
}

// newLegacyInstall writes plaintext accounts and tokens documents, as a
// release that ran keyless would have, into <root>/data.
func newLegacyInstall(t *testing.T) *legacyInstall {
	t.Helper()
	l := buildLegacy(t)
	data := filepath.Join(l.root, "data")
	must(t, os.MkdirAll(data, 0o700))
	must(t, os.WriteFile(filepath.Join(data, "users.json"), l.accounts, 0o600))
	must(t, os.WriteFile(filepath.Join(data, "tokens.json"), l.tokenDoc, 0o600))
	return l
}

// newLegacyInstallSealed writes the same documents as the previous release
// sealed them under the key: mikroview's encrypted file backend, bound to
// the file's path.
func newLegacyInstallSealed(t *testing.T) *legacyInstall {
	t.Helper()
	l := buildLegacy(t)
	data := filepath.Join(l.root, "data")
	must(t, os.MkdirAll(data, 0o700))
	key, err := retention.LoadKey(l.keyPath)
	if err != nil {
		t.Fatalf("loading the test key: %v", err)
	}
	for name, doc := range map[string][]byte{"users.json": l.accounts, "tokens.json": l.tokenDoc} {
		b := persist.NewEncryptedFileBackend(filepath.Join(data, name), storeNameFor(name), key)
		if _, err := b.Save(context.Background(), doc, 0); err != nil {
			t.Fatalf("sealing %s as the previous release did: %v", name, err)
		}
	}
	return l
}
