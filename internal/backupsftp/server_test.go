// SPDX-License-Identifier: AGPL-3.0-only

package backupsftp

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/backupvault"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// testHostKey builds an ephemeral host key for a test server -- never
// LoadOrGenerateHostKey's on-disk path, which is covered by its own test.
func testHostKey(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func testTokens(t *testing.T) *auth.TokenStore {
	t.Helper()
	ts, err := auth.OpenTokenStoreWithBackend(persist.NewFileBackend(filepath.Join(t.TempDir(), "tokens.json")))
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func testKey(t *testing.T) *retention.Key {
	t.Helper()
	k, err := retention.NewKeyFromMaterial([]byte(strings.Repeat("k", retention.MinKeyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// harness stands up a real Server on a loopback port and returns an
// ssh.ClientConfig ready to dial it -- RouterOS never verifies the host
// key either (measured, #394 note 10510), so InsecureIgnoreHostKey here
// matches the real client this server actually talks to, not merely
// test convenience.
type harness struct {
	addr   string
	vault  *backupvault.Vault
	tokens *auth.TokenStore
}

func newHarness(t *testing.T, key *retention.Key) *harness {
	t.Helper()
	v, err := backupvault.Open(t.TempDir(), key)
	if err != nil {
		t.Fatal(err)
	}
	tokens := testTokens(t)
	srv := New(v, tokens, testHostKey(t))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	go func() {
		go func() {
			// crude readiness: retry-dial loop below handles the race,
			// this just gives ListenAndServe a moment to bind first.
			close(ready)
		}()
		_ = srv.ListenAndServe(ctx, addr)
	}()
	<-ready
	t.Cleanup(cancel)

	// Retry the dial briefly: ListenAndServe's net.Listen happens
	// asynchronously above.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never started listening on %s", addr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	return &harness{addr: addr, vault: v, tokens: tokens}
}

func (h *harness) mintIngestToken(t *testing.T, device string) string {
	t.Helper()
	raw, _, err := h.tokens.Create("test", auth.TokenKindIngest, device, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (h *harness) dial(t *testing.T, device, password string) (*ssh.Client, error) {
	t.Helper()
	cfg := &ssh.ClientConfig{
		User:            device,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	}
	return ssh.Dial("tcp", h.addr, cfg)
}

func TestLoginRefusedForWrongToken(t *testing.T) {
	h := newHarness(t, testKey(t))
	h.mintIngestToken(t, "rb5009")
	if _, err := h.dial(t, "rb5009", "definitely-not-the-token"); err == nil {
		t.Fatal("dial with a wrong password succeeded")
	}
}

func TestLoginRefusedWhenUsernameDoesNotMatchTokenDevice(t *testing.T) {
	h := newHarness(t, testKey(t))
	tok := h.mintIngestToken(t, "rb5009")
	// The right token, presented under a different username: #394 requires
	// a login can only write inside its own folder, which starts with the
	// username actually having to match the token it was issued for.
	if _, err := h.dial(t, "hap-ax2", tok); err == nil {
		t.Fatal("dial as the wrong device with a valid token for another device succeeded")
	}
}

func TestLoginRefusedWithNoRetentionKey(t *testing.T) {
	h := newHarness(t, nil) // no key: #394's "no key, no backups"
	tok := h.mintIngestToken(t, "rb5009")
	if _, err := h.dial(t, "rb5009", tok); err == nil {
		t.Fatal("dial succeeded with no retention key configured")
	}
}

func TestUploadCommitsOnlyOnClose(t *testing.T) {
	h := newHarness(t, testKey(t))
	tok := h.mintIngestToken(t, "rb5009")
	conn, err := h.dial(t, "rb5009", tok)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	defer client.Close()

	body := append([]byte{0x88, 0xac, 0xa1, 0xb1}, []byte("a real backup")...)
	f, err := client.Create("rb5009.backup")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.Write(body); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	gens := h.vault.Generations("rb5009")
	if len(gens) != 1 {
		t.Fatalf("got %d generations after a completed upload, want 1", len(gens))
	}
}

// TestInterruptedTransferIsDiscarded drops the raw connection mid-upload,
// before the SFTP Close ever reaches the server -- #394's "a file is
// committed only when Close arrives; an interrupted transfer is
// discarded".
func TestInterruptedTransferIsDiscarded(t *testing.T) {
	h := newHarness(t, testKey(t))
	tok := h.mintIngestToken(t, "rb5009")
	conn, err := h.dial(t, "rb5009", tok)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	client, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	f, err := client.Create("rb5009.backup")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.Write([]byte{0x88, 0xac, 0xa1, 0xb1, 'x'}); err != nil {
		t.Fatalf("write: %v", err)
	}
	// No f.Close(): sever the connection outright, as a dropped transfer
	// would.
	conn.Close()

	time.Sleep(50 * time.Millisecond) // let the server notice the close
	if got := h.vault.Generations("rb5009"); len(got) != 0 {
		t.Fatalf("got %d generations after an interrupted transfer, want 0", len(got))
	}
}

func TestWriteOnlyRefusesReadListAndOverwrite(t *testing.T) {
	h := newHarness(t, testKey(t))
	tok := h.mintIngestToken(t, "rb5009")
	conn, err := h.dial(t, "rb5009", tok)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	client, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	defer client.Close()

	// Write one file to have something to (attempt to) read/list/remove.
	f, err := client.Create("rb5009.backup")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte{0x88, 0xac, 0xa1, 0xb1})
	f.Close()

	if _, err := client.ReadDir("/"); err == nil {
		t.Error("ReadDir succeeded -- listing must be refused")
	}
	if _, err := client.Open("rb5009.backup"); err == nil {
		t.Error("Open (read) succeeded -- reading must be refused")
	}
	if err := client.Remove("rb5009.backup"); err == nil {
		t.Error("Remove succeeded -- deleting must be refused")
	}
	if err := client.Rename("rb5009.backup", "rb5009.rsc"); err == nil {
		t.Error("Rename succeeded -- renaming must be refused")
	}
}

func TestOverCapUploadFails(t *testing.T) {
	h := newHarness(t, testKey(t))
	tok := h.mintIngestToken(t, "rb5009")
	conn, err := h.dial(t, "rb5009", tok)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	client, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	defer client.Close()

	f, err := client.Create("rb5009.backup")
	if err != nil {
		t.Fatal(err)
	}
	big := bytes.Repeat([]byte("x"), backupvault.MaxFileBytes+1)
	_, werr := f.Write(big)
	cerr := f.Close()
	if werr == nil && cerr == nil {
		t.Fatal("an over-cap upload succeeded end to end")
	}
	if got := h.vault.Generations("rb5009"); len(got) != 0 {
		t.Fatalf("got %d generations after an over-cap push, want 0", len(got))
	}
}

func TestSubdirectoryDestinationRefused(t *testing.T) {
	h := newHarness(t, testKey(t))
	tok := h.mintIngestToken(t, "rb5009")
	conn, err := h.dial(t, "rb5009", tok)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	client, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	defer client.Close()

	if _, err := client.Create("sub/rb5009.backup"); err == nil {
		t.Error("creating inside a subdirectory succeeded -- a login must be confined to a flat area")
	}
}

func TestUnrecognisedExtensionRefused(t *testing.T) {
	h := newHarness(t, testKey(t))
	tok := h.mintIngestToken(t, "rb5009")
	conn, err := h.dial(t, "rb5009", tok)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	client, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	defer client.Close()

	if _, err := client.Create("rb5009.exe"); err == nil {
		t.Error("creating a file with an unrecognised extension succeeded")
	}
}
