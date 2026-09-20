// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// appFolderRoot is the folder an operator mounts read-only at
// /etc/mikroview: config, GeoIP database, history key, certificate, all
// in one place, so adding a feature is dropping a file in and
// restarting rather than adding another mount line
// (docs/decisions/app-folder.md, #1209).
//
// A package variable and deliberately not a new environment variable
// (#1243). The folder is the deployment contract, not a knob, and a
// MIKROVIEW_APP_FOLDER would be one more thing to set in the layout
// whose whole point is that nothing has to be set. Tests point this at
// a temp directory; nothing else writes to it.
var appFolderRoot = "/etc/mikroview"

// The fixed layout inside the folder. The names are the contract --
// docs/decisions/app-folder.md prints this same list -- so an operator
// can put a file in the right place without reading any code.
const (
	appFolderConfigFile  = "config.yaml"
	appFolderGeoIPFile   = "GeoLite2-Country.mmdb"
	appFolderHistoryKey  = "keys/history.key"
	appFolderTLSCertFile = "certs/tls.crt"
	appFolderTLSKeyFile  = "certs/tls.key"
)

// DefaultConfigPath is the config file MikroView reads when nothing
// names one. Exported so a caller that has to describe the search --
// -validate-config, saying where it looked -- quotes the same path the
// loader used rather than repeating the literal.
func DefaultConfigPath() string { return filepath.Join(appFolderRoot, appFolderConfigFile) }

// AppFolderLookup is one optional file MikroView looked for in the app
// folder, and whether it was there.
//
// Recorded rather than logged from here: the config package holds no
// logger, and one loop at the call site keeps the boot lines in one
// place and in a fixed order.
type AppFolderLookup struct {
	// Key names the setting the file fills -- "geoip.dbPath" -- so the
	// boot line connects to the configuration reference. The config
	// file itself has no key and reads as "config file".
	Key string
	// Path is where MikroView looked. Printed whether or not the file
	// was there: "not found" on its own leaves an operator who
	// misspelled a folder name with nothing to compare against.
	Path  string
	Found bool
}

func (l AppFolderLookup) String() string {
	if l.Found {
		return fmt.Sprintf("%s: found %s", l.Key, l.Path)
	}
	return fmt.Sprintf("%s: not found at %s", l.Key, l.Path)
}

// appFolderFileExists reports whether path is a file MikroView could
// read. A directory of that name is not the file -- an empty bind mount
// created by docker when the host path is missing looks exactly like
// that, and treating it as the operator's certificate would turn a
// typo into a startup failure further downstream.
func appFolderFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// applyAppFolder fills in the paths that config, environment and flags
// all left unset, from the app folder (#1243).
//
// Called after every other source has been merged and before Validate,
// so an explicit value always wins -- that is what keeps every existing
// install working unchanged (#1209's answer 4) -- and so a path the
// folder supplied is checked like any other.
//
// The returned lookups cover every file it actually looked for; a
// setting somebody already named is not looked for at all, and says
// nothing.
func applyAppFolder(cfg *Config) ([]AppFolderLookup, error) {
	var lookups []AppFolderLookup
	look := func(key, name string) (string, bool) {
		path := filepath.Join(appFolderRoot, name)
		found := appFolderFileExists(path)
		lookups = append(lookups, AppFolderLookup{Key: key, Path: path, Found: found})
		return path, found
	}

	if cfg.GeoIP.DBPath == "" {
		if path, found := look("geoip.dbPath", appFolderGeoIPFile); found {
			cfg.GeoIP.DBPath = path
		}
	}

	if cfg.History.KeyFile == "" {
		if path, found := look("history.keyFile", appFolderHistoryKey); found {
			cfg.History.KeyFile = path
		}
	}

	// TLS is a pair or nothing. Only when neither half is set does the
	// folder get a say: mixing a certificate somebody named with a key
	// found here would serve a cert/key pair nobody chose.
	if cfg.TLS.CertFile == "" && cfg.TLS.KeyFile == "" {
		certPath, haveCert := look("tls.certFile", appFolderTLSCertFile)
		keyPath, haveKey := look("tls.keyFile", appFolderTLSKeyFile)
		switch {
		case haveCert && haveKey:
			cfg.TLS.CertFile, cfg.TLS.KeyFile = certPath, keyPath
		case haveCert != haveKey:
			// Refusing to start, rather than quietly falling back to
			// the self-signed certificate: half a pair is somebody
			// mid-way through installing their own certificate, and
			// starting on a certificate they did not choose -- under
			// the name they expected their own to serve -- is the
			// failure they would notice last.
			missing, present := keyPath, certPath
			if haveKey {
				missing, present = certPath, keyPath
			}
			return lookups, fmt.Errorf("the app folder has %s but not %s: a certificate and its private key are used together or not at all. Put the missing file there, or remove both to go back to MikroView's own certificate", present, missing)
		}
	}

	return lookups, nil
}
