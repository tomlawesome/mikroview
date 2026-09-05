// SPDX-License-Identifier: AGPL-3.0-only

package backupsftp

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// hostKeyFileName is kept beside the TLS material (internal/servertls's
// StorePath), not the data directory -- both are generated-on-first-run
// secrets a restore should not carry, per #394's "excluded from -backup"
// requirement. See excludedFromBackup in backup_cli.go.
const hostKeyFileName = "sftp-host-key"

// LoadOrGenerateHostKey returns the drop box's SSH host key, generating
// and persisting one under storeDir on first use.
//
// ed25519 rather than RSA: it is golang.org/x/crypto/ssh's own default
// preference order's top choice, has no key-size decision to get wrong,
// and #394's own measurement (note 10510) found RouterOS negotiated it
// with no special configuration on either side.
//
// Note the security finding this key does *not* fix (#394 note 10510,
// measured on RouterOS 7.23.3): RouterOS's own `/tool fetch mode=sftp`
// never verifies a host key at all, so this key protects a client that
// checks it (a human with an SFTP client, say) but not the router the
// wizard's script targets. Documented in SECURITY.md and the wizard's
// own caveat; generating and persisting it properly is still correct
// for the clients that do check.
func LoadOrGenerateHostKey(storeDir string) (ssh.Signer, error) {
	path := filepath.Join(storeDir, hostKeyFileName)
	if raw, err := os.ReadFile(path); err == nil {
		signer, err := ssh.ParsePrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("backupsftp: parsing %s: %w", path, err)
		}
		return signer, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("backupsftp: reading %s: %w", path, err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("backupsftp: generating host key: %w", err)
	}
	pemBlock, err := ssh.MarshalPrivateKey(priv, "mikroview router-backup SFTP host key")
	if err != nil {
		return nil, fmt.Errorf("backupsftp: encoding host key: %w", err)
	}
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return nil, fmt.Errorf("backupsftp: creating %s: %w", storeDir, err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(pemBlock), 0o600); err != nil {
		return nil, fmt.Errorf("backupsftp: writing %s: %w", path, err)
	}

	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, fmt.Errorf("backupsftp: building signer: %w", err)
	}
	return signer, nil
}
