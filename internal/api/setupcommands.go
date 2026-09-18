// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/setup"
)

// setupCommandsRequest is what the wizard sends to render RouterOS
// commands (#436): only Address is required, since every other field is
// either optional in the commands it feeds (SyslogPort falls back to the
// running configuration, Token/Kinds together gate whether a push script
// is worth rendering at all) or is itself optional input (Version, the
// operator's pick from the dialect table's rows).
type setupCommandsRequest struct {
	Address    string   `json:"address"`
	SyslogPort string   `json:"syslogPort"`
	Token      string   `json:"token"`
	Kinds      []string `json:"kinds"`
	Version    string   `json:"version"`
	// Device names the router step 6's backup script is being rendered
	// for -- the SFTP username and the destination file stem
	// (routeros.BackupScript). The push script above needs no such
	// field: PushBlock's payload carries no destination filename, only
	// the token.
	Device string `json:"device"`
}

// routerosTable is the dialect table itself, so the wizard can quote its
// bounds ("commands were written for 7.18 and later ... last checked
// against 7.24.1") without hard-coding them a second time.
type routerosTable struct {
	Minimum string         `json:"minimum"`
	Newest  string         `json:"newest"`
	Rows    []routeros.Row `json:"rows"`
}

// pickedVersion is what the operator's Version selection (or its
// absence) resolved to: the standing that version holds, and the dialect
// used to render every step below.
type pickedVersion struct {
	Version  string `json:"version"`
	Standing string `json:"standing"`
	Dialect  string `json:"dialect"`
}

// setupCommandsRouter is one router whose version mikroview actually
// knows -- from a push, or #436 step 3's /ca.crt?ros= hint -- with where
// it stands and any per-row note that concerns it.
type setupCommandsRouter struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	RouterOSVersion string `json:"routerosVersion"`
	Standing        string `json:"standing"`
	Note            string `json:"note"`
}

// commandStep is one rendered block: the commands themselves, and any
// note that belongs beside this specific step (currently only
// ruleTagging ever carries one -- a row's Note, when the selected
// version's row has one).
//
// Blocked carries every reason this block came back blank, as
// machine-readable keys (#1217): the server says which precondition is
// missing, the frontend owns the sentence it says about it. Only
// Backup/BackupSchedule ever set this today -- see the keys listed
// beside backupBlockedKeys below. Empty/omitted means either the block
// is not blank, or it is blank for a reason this field does not cover
// (Push/Schedule's own token-and-kinds gate, unrelated to #1217's
// backup-step complaint).
type commandStep struct {
	Commands string   `json:"commands"`
	Note     string   `json:"note"`
	Blocked  []string `json:"blocked,omitempty"`
}

type setupCommandsSteps struct {
	CaTrust     commandStep `json:"caTrust"`
	Syslog      commandStep `json:"syslog"`
	RuleTagging commandStep `json:"ruleTagging"`
	Push        commandStep `json:"push"`
	// Schedule is step 4's whole hand-over since #1131: the push script
	// saved, scheduled and run once, in one block the operator pastes
	// as it stands. Push stays beside it as the bare script body, which
	// is what the Engine Room's "copy the router lines" re-key affordance
	// wants -- an existing mv-push script's source, not a second
	// `/system script add`.
	Schedule commandStep `json:"schedule"`
	// Backup/BackupSchedule are step 6's two blocks (#394, round 45):
	// the script that saves, exports and pushes both files, and the
	// nightly scheduler entry. Both stay blank when the drop box is not
	// ready -- see backupReady below -- exactly as Push stays blank
	// with no token or kinds.
	Backup         commandStep `json:"backup"`
	BackupSchedule commandStep `json:"backupSchedule"`
}

type setupCommandsResponse struct {
	RouterOS routerosTable         `json:"routeros"`
	Picked   *pickedVersion        `json:"picked"`
	Routers  []setupCommandsRouter `json:"routers"`
	Steps    setupCommandsSteps    `json:"steps"`
}

// handleSetupCommands renders the RouterOS commands the setup wizard
// shows an operator (#436): the dialect table itself so the wizard can
// quote its bounds, what an operator-picked version resolves to, every
// router whose version mikroview actually knows and where it stands, and
// the five command blocks rendered for the selected dialect.
//
// Same access gate as GET /api/setup/status beside it: open to any
// signed-in user, not admin-gated (see that handler's doc comment for
// why -- this only re-renders what a viewer can already read off the
// setup-status and devices endpoints as copy-paste commands).
func (s *Server) handleSetupCommands(w http.ResponseWriter, r *http.Request) {
	var req setupCommandsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// Address falls back to the operator's own stored answer (#1213) the
	// same way syslogPort below falls back to the running configuration
	// when the wizard didn't send one -- this endpoint is open to any
	// signed-in user and re-renders text, not a source of truth in its
	// own right, so a caller that omits it gets what the header field
	// actually holds rather than nothing. It is no longer required at
	// the door either way: an operator who has not yet answered that
	// field gets every address-dependent block back blank with the
	// noAddressKey below, the same "blank rather than half-formed"
	// contract every other missing precondition here already gets.
	if req.Address == "" && s.Setup != nil {
		req.Address = s.Setup.Address()
	}
	if err := validateSetupCommandsRequest(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Dialect selection (#436): the picked version's own row when it has
	// one; otherwise, since mikroview holds exactly one dialect today,
	// that one. This is the seam a second dialect would use -- nothing
	// else here needs to change the day one appears.
	dialect := defaultDialect()
	var picked *pickedVersion
	if req.Version != "" {
		d := dialect
		if row, ok := routeros.RowFor(req.Version); ok {
			d = row.Dialect
		}
		picked = &pickedVersion{
			Version:  req.Version,
			Standing: routeros.VersionStanding(req.Version).String(),
			Dialect:  d,
		}
		dialect = d
	}

	routers := make([]setupCommandsRouter, 0)
	if s.Devices != nil {
		for _, info := range s.Devices.List() {
			version, ok := s.effectiveRouterOSVersion(info)
			if !ok {
				continue
			}
			note := ""
			if row, ok := routeros.RowFor(version); ok {
				note = row.Note
			}
			routers = append(routers, setupCommandsRouter{
				ID:              info.ID,
				Name:            info.Name,
				RouterOSVersion: version,
				Standing:        routeros.VersionStanding(version).String(),
				Note:            note,
			})
		}
	}

	// syslogPort falls back to the instance's own running configuration
	// when the wizard didn't send one -- the same value
	// GET /api/setup/status's Instance.SyslogPort already reports.
	syslogPort := req.SyslogPort
	if syslogPort == "" {
		syslogPort = s.SetupInstance.SyslogPort
	}

	// The rule-tagging note is the picked version's own row note, when
	// there is a picked version and its row has one -- e.g. 7.24.0's
	// find-lookup warning. No picked version means nothing to quote here;
	// a per-router note about the same thing is carried on that router's
	// own entry in Routers above.
	ruleTaggingNote := ""
	if picked != nil {
		if row, ok := routeros.RowFor(picked.Version); ok {
			ruleTaggingNote = row.Note
		}
	}

	// noAddressKey is #1213's own precondition, checked ahead of every
	// other one below: CaTrust, Syslog, Push/Schedule and Backup all
	// embed req.Address somewhere in what they render, so with nothing
	// answered yet none of them mean anything -- not even the caTrust
	// step, which used to render unconditionally against
	// window.location.host and now has as much reason to be blank as the
	// rest. Reuses commandStep.Blocked, #1217's mechanism for saying why
	// a block is blank, with one more key rather than a second shape for
	// the same idea.
	const noAddressKey = "no-address"
	noAddress := req.Address == ""
	var addressBlocked []string
	if noAddress {
		addressBlocked = []string{noAddressKey}
	}

	caTrustCommands, syslogCommands := "", ""
	if !noAddress {
		caTrustCommands = routeros.CaTrustCommands(req.Address, dialect)
		syslogCommands = routeros.SyslogCommands(req.Address, syslogPort, dialect)
	}

	// A push script only means something with an address to embed, a
	// token, and at least one kind to push; any missing leaves it blank
	// rather than rendering an empty or half-formed script. Schedule
	// follows it: since #1131 it carries the script body itself, so with
	// nothing to carry it is a `/system script add` around an empty
	// source, which is exactly the half-formed block this blankness rule
	// exists for. Only the address gets a Blocked key, matching the
	// existing "the token/kinds gate is unrelated to #1217's complaint"
	// carve-out just below for that half of the precondition.
	pushCommands, scheduleCommands := "", ""
	if !noAddress && req.Token != "" && len(req.Kinds) > 0 {
		pushCommands = routeros.PushScript(req.Address, req.Token, req.Kinds, dialect)
		scheduleCommands = routeros.ScheduleCommands(pushCommands, dialect)
	}

	// The backup script only means something with a device to name, a
	// token to authenticate it, and somewhere for it to land: a
	// retention key configured (s.Vault.Enabled()) and the drop box
	// actually turned on (SetupInstance.BackupPort set at startup from
	// cfg.Backup.Enabled -- see main.go). Any missing piece leaves both
	// blocks blank, same "blank rather than half-formed" contract Push
	// already has above; the wizard reads a blank Backup block as its
	// wnokey state (round 45).
	//
	// backupBlockedKeys names every missing piece, all that apply rather
	// than just the first (#1217, the owner's "cover all possibilities"
	// ruling): the operator's instance was missing two at once, and a
	// key naming only one would have sent them round the loop a second
	// time to find the other. Wording stays out of Go entirely -- the
	// frontend owns every operator-facing sentence, same split #436
	// already draws for the commands themselves.
	//
	// Which of the two step-6 scripts is rendered is the operator's
	// stored answer, not a per-request field (#955): the transport is a
	// property of the deployment, so it is read from the setup store
	// here the same way the address above falls back to it.
	backupTransport := setup.BackupTransportSFTP
	if s.Setup != nil {
		backupTransport = s.Setup.BackupTransport()
	}
	httpsTransport := backupTransport == setup.BackupTransportHTTPS
	var backupBlockedKeys []string
	if noAddress {
		backupBlockedKeys = append(backupBlockedKeys, noAddressKey)
	}
	// The drop box's own preconditions belong to the SFTP script alone.
	// The HTTPS push needs none of them (#955's ruling): it goes through
	// the ingest channel that is already open, so the only thing it
	// waits on beyond the address is the token -- the same precondition
	// step 4 has.
	if !httpsTransport {
		if s.SetupInstance.BackupPort == "" {
			backupBlockedKeys = append(backupBlockedKeys, "backups-off")
		}
		if !s.Vault.Enabled() {
			backupBlockedKeys = append(backupBlockedKeys, "no-retention-key")
		}
		if req.Device == "" {
			backupBlockedKeys = append(backupBlockedKeys, "no-device")
		}
	}
	if req.Token == "" {
		backupBlockedKeys = append(backupBlockedKeys, "no-token")
	}
	backupCommands, backupScheduleCommands := "", ""
	if len(backupBlockedKeys) == 0 {
		if httpsTransport {
			// The whole address, port and all: this one is posted to
			// mikroview's own HTTPS listener, the same value
			// CaTrustCommands and PushBlock embed -- not the bare host
			// the SFTP form below pairs with the drop box's port.
			backupCommands = routeros.BackupPushScript(req.Address, req.Token, dialect)
			backupScheduleCommands = routeros.BackupPushScheduleCommands(backupCommands, dialect)
		} else {
			// The drop box listens on its own port, not the HTTPS port
			// req.Address carries -- same reasoning SyslogCommands' Hostname
			// call gives for stripping the web port off before pairing it
			// with the syslog port.
			backupCommands = routeros.BackupScript(routeros.Hostname(req.Address), s.SetupInstance.BackupPort, req.Device, req.Token, dialect)
			backupScheduleCommands = routeros.BackupScheduleCommands(dialect)
		}
	}

	writeJSON(w, http.StatusOK, setupCommandsResponse{
		RouterOS: routerosTable{
			Minimum: routeros.MinimumVersion,
			Newest:  routeros.NewestVersion(),
			Rows:    append([]routeros.Row(nil), routeros.Rows...),
		},
		Picked:  picked,
		Routers: routers,
		Steps: setupCommandsSteps{
			CaTrust:     commandStep{Commands: caTrustCommands, Blocked: addressBlocked},
			Syslog:      commandStep{Commands: syslogCommands, Blocked: addressBlocked},
			RuleTagging: commandStep{Commands: routeros.RuleTaggingCommands(dialect), Note: ruleTaggingNote},
			// Push/Schedule only carry the address key, never the
			// token/kinds gate -- see the comment above pushCommands.
			Push:           commandStep{Commands: pushCommands, Blocked: addressBlocked},
			Schedule:       commandStep{Commands: scheduleCommands, Blocked: addressBlocked},
			Backup:         commandStep{Commands: backupCommands, Blocked: backupBlockedKeys},
			BackupSchedule: commandStep{Commands: backupScheduleCommands, Blocked: backupBlockedKeys},
		},
	})
}

// validateSetupCommandsRequest rejects any field whose value could change
// the structure of the RouterOS commands routeros.CaTrustCommands,
// SyslogCommands, PushScript/PushBlock and BackupScript assemble from it
// (#1095): this endpoint is open to any signed-in user, and the rendered
// commands are meant to be pasted into a router terminal unmodified, so a
// '"', '\', ';', space or newline reaching one of those templates is
// never a value worth rendering.
func validateSetupCommandsRequest(req setupCommandsRequest) error {
	// Empty is the "not answered yet" case (#1213) -- handleSetupCommands
	// reads it as noAddress and blanks every block that needs one, rather
	// than refusing the whole request the way an empty value used to.
	// Anything non-empty still has to be a plausible address: it is about
	// to sit bare inside a RouterOS command.
	if req.Address != "" && !validSetupAddress(req.Address) {
		return errors.New("address must be empty, a hostname, or an IP address, optionally with :port")
	}
	if !validSetupSyslogPort(req.SyslogPort) {
		return errors.New("syslogPort must be empty or a number from 1 to 65535")
	}
	if !validSetupToken(req.Token) {
		return errors.New("token must be letters, digits, '_' or '-', up to 256 characters")
	}
	if !validSetupDevice(req.Device) {
		return errors.New("device must be 1 to 64 characters from letters, digits, '.', '_' and '-'")
	}
	return nil
}

// maxSetupDeviceLen mirrors internal/auth/token.go's maxDeviceIDLen --
// that constant is unexported, so it cannot be reused directly, but
// nothing legitimate needs a longer device name than an ingest token's
// own device scope already allows.
const maxSetupDeviceLen = 64

// validSetupDevice restricts Device to the charset internal/auth/token.go's
// validDeviceID already accepts in practice for a real device -- letters,
// digits, dot, underscore, hyphen -- narrower than validDeviceID itself,
// which exists to police the token store rather than a value that is
// about to sit bare inside routeros.BackupScript's user=/dst-path=
// placements (#1095). Empty is fine: Device is optional, and
// handleSetupCommands already treats "" as "render no backup script."
func validSetupDevice(device string) bool {
	if device == "" {
		return true
	}
	if len(device) > maxSetupDeviceLen {
		return false
	}
	// An auto-discovered device's id is its source address
	// (internal/device.Registry.Resolve), so an IPv6 id carries colons
	// the charset below does not; an IP literal is safe bare in every
	// placement the templates use.
	if net.ParseIP(device) != nil {
		return true
	}
	for i := 0; i < len(device); i++ {
		c := device[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

// maxSetupTokenLen caps Token well above anything internal/auth ever
// issues, while still keeping an absurd value out of the rendered
// commands (#1095).
const maxSetupTokenLen = 256

// validSetupToken restricts Token to [A-Za-z0-9_-] -- the alphabet
// internal/auth actually mints tokens in, so nothing legitimate is
// refused. Narrower than "printable ASCII, no quote/backslash/dollar/
// whitespace" on purpose: Token also lands bare inside PushBlock and
// loggingPushBlock's http-header-field=("Content-Type: ...,
// Authorization: Bearer <token>") in internal/routeros/commands.go, and
// RouterOS splits that value on commas into separate headers regardless
// of quoting -- a comma in Token would add a header, which quoting
// cannot prevent, so the alphabet itself is the gate.
func validSetupToken(token string) bool {
	if token == "" {
		return true
	}
	if len(token) > maxSetupTokenLen {
		return false
	}
	for i := 0; i < len(token); i++ {
		c := token[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

// validSetupPortNumber reports whether s is exactly the digits of a port
// number from 1 to 65535 -- no sign, no leading/trailing junk.
func validSetupPortNumber(s string) bool {
	if s == "" || len(s) > 5 {
		return false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	return n >= 1 && n <= 65535
}

// validSetupSyslogPort accepts SyslogPort empty (handleSetupCommands
// falls back to the running configuration), a bare port number, or a
// listen address whose port is one: GET /api/setup/status's own
// Instance.SyslogPort -- the value the wizard round-trips back here
// unless the operator overrides it -- is listen.syslogTls verbatim
// (":6514" shipped, "127.0.0.1:16823" under scripts/live-env.sh). Only
// the port reaches a command (routeros.PortOf), so the host part need
// only be well-formed.
func validSetupSyslogPort(s string) bool {
	if s == "" {
		return true
	}
	if host, port, err := net.SplitHostPort(s); err == nil {
		if host != "" && net.ParseIP(host) == nil && !validSetupHostname(host) {
			return false
		}
		return validSetupPortNumber(port)
	}
	return validSetupPortNumber(s)
}

// validSetupHostLabel reports whether label is a valid hostname label:
// letters, digits and hyphens, never leading or trailing with one.
func validSetupHostLabel(label string) bool {
	if label == "" || len(label) > 63 {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
		default:
			return false
		}
	}
	return true
}

// validSetupHostname reports whether host is a dot-separated run of
// validSetupHostLabel labels.
func validSetupHostname(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !validSetupHostLabel(label) {
			return false
		}
	}
	return true
}

// validSetupAddress reports whether s is a hostname or IP literal,
// optionally with a :port suffix -- the same address forms
// routeros.Hostname's own doc comment handles (bare host, host:port,
// [ipv6]:port, and a bare IPv6 literal with no port and so no brackets).
// net.SplitHostPort does the bracket/port splitting so this does not
// have to reimplement it; what is left over is judged as either an IP
// literal or a hostname.
func validSetupAddress(s string) bool {
	if s == "" {
		return false
	}
	host := s
	if h, port, err := net.SplitHostPort(s); err == nil {
		if !validSetupPortNumber(port) {
			return false
		}
		host = h
	}
	if net.ParseIP(host) != nil {
		return true
	}
	return validSetupHostname(host)
}

// defaultDialect is the dialect used when nothing else picks one -- see
// handleSetupCommands' dialect-selection comment. Reads Rows[0] rather
// than a hard-coded "a" so a second dialect landing in dialects.go
// cannot silently disagree with what this falls back to.
func defaultDialect() string {
	if len(routeros.Rows) == 0 {
		return ""
	}
	return routeros.Rows[0].Dialect
}
