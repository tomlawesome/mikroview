// SPDX-License-Identifier: AGPL-3.0-only

// Package backupsftp is the write-only SFTP drop box RouterOS pushes its
// configuration backups into (#394).
//
// Login: username is the device name, password is that device's ingest
// token, checked against the same hash internal/auth's token store
// already keeps -- RouterOS's fetch has no key-based auth option
// (measured, #394 note 10510). A login can write only inside its own
// device's area, and can do nothing else: no listing, reading, deleting,
// renaming or overwriting anything, and no shell, exec or port
// forwarding. A file is only ever committed to the vault when its SFTP
// Close arrives (see pendingWrite); an interrupted transfer commits
// nothing.
package backupsftp

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/backupvault"
	"github.com/tomlawesome/mikroview/internal/logging"
)

// Server is the drop box. Vault and Tokens are both required; HostKey is
// generated once and handed in already loaded (see LoadOrGenerateHostKey)
// so this package never decides where key material lives on disk.
type Server struct {
	Vault   *backupvault.Vault
	Tokens  *auth.TokenStore
	HostKey ssh.Signer
	log     *slog.Logger
}

// New builds a Server. It does not listen until ListenAndServe is called.
func New(vault *backupvault.Vault, tokens *auth.TokenStore, hostKey ssh.Signer) *Server {
	return &Server{Vault: vault, Tokens: tokens, HostKey: hostKey, log: logging.New("backupsftp")}
}

// config builds a fresh ssh.ServerConfig. Password auth only -- no
// public key, no keyboard-interactive -- matching the one credential
// RouterOS's fetch can actually present.
func (s *Server) config() *ssh.ServerConfig {
	cfg := &ssh.ServerConfig{
		PasswordCallback: s.authenticate,
		MaxAuthTries:     3,
		ServerVersion:    "SSH-2.0-mikroview-backup-dropbox",
	}
	cfg.AddHostKey(s.HostKey)
	return cfg
}

// authenticate is the login gate: username = device name, password =
// that device's ingest token. Two refusals happen here rather than
// after a session opens, per #394's "no key, no backups" rule: a vault
// with no retention key configured refuses every login outright, and an
// unrecognised or wrongly-scoped token refuses too. Both are logged --
// #394 requires a refusal to be visible, not silent -- but with no
// detail an on-path attacker could use to enumerate valid device names.
func (s *Server) authenticate(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
	device := conn.User()
	if !s.Vault.Enabled() {
		s.log.Warn(fmt.Sprintf("refused an SFTP login from %s: no retention key configured -- the drop box is closed", conn.RemoteAddr()))
		return nil, fmt.Errorf("backups are not accepted on this deployment")
	}
	tok, ok := s.Tokens.Authenticate(string(password), auth.TokenKindIngest, time.Now())
	if !ok || tok.Device == "" || tok.Device != device {
		s.log.Warn(fmt.Sprintf("refused an SFTP login for %q from %s: not a valid ingest token for that device", device, conn.RemoteAddr()))
		return nil, fmt.Errorf("authentication failed")
	}
	return &ssh.Permissions{Extensions: map[string]string{"device": device}}, nil
}

// ListenAndServe accepts connections on addr until ctx is done, same
// shutdown contract as main.go's other listeners (see syslog.ListenTLS):
// it returns nil on a clean context cancellation and a real error
// otherwise.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("backupsftp: listening on %s: %w", addr, err)
	}
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	s.log.Info(fmt.Sprintf("router backup drop box listening on %s", addr))
	cfg := s.config()
	for {
		nc, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("backupsftp: accept: %w", err)
		}
		go s.handleConn(nc, cfg)
	}
}

// handleConn negotiates one SSH connection and dispatches its channels.
// Never more than one device per connection: the device is fixed at
// authenticate and carried in sconn.Permissions for the life of the
// connection.
func (s *Server) handleConn(nc net.Conn, cfg *ssh.ServerConfig) {
	defer nc.Close()
	// Covers this goroutine's own code (the handshake and the channel
	// dispatch loop below); it does not reach into pkg/sftp's own
	// request-worker goroutines started inside handleSession -- see
	// recoverAndClose's doc comment for why those need their own
	// recover instead.
	defer logging.Recover(s.log)
	sconn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		s.log.Warn(fmt.Sprintf("SFTP handshake from %s failed: %v", nc.RemoteAddr(), err))
		return
	}
	defer sconn.Close()
	device := sconn.Permissions.Extensions["device"]

	// Global requests (e.g. keepalives) get a blanket "no" -- there is
	// nothing this server does at the connection level.
	go ssh.DiscardRequests(reqs)

	var wg sync.WaitGroup
	for newChan := range chans {
		// Only a plain session channel is accepted. In particular this
		// refuses "direct-tcpip" outright, which is what a client would
		// open to tunnel a forwarded connection through this server --
		// #394 requires no forwarding, and refusing the channel type is
		// what makes that true structurally rather than by convention.
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "only an SFTP session is accepted")
			continue
		}
		ch, chReqs, err := newChan.Accept()
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer logging.Recover(s.log)
			s.handleSession(ch, chReqs, device)
		}()
	}
	wg.Wait()
}

// handleSession answers exactly one kind of channel request: "subsystem
// sftp". Everything else -- "shell", "exec", "pty-req", and anything
// else a client might ask a session channel to do -- is refused, which
// is what makes "no shell or exec" true rather than merely unused.
func (s *Server) handleSession(ch ssh.Channel, reqs <-chan *ssh.Request, device string) {
	defer ch.Close()
	for req := range reqs {
		ok := isSFTPSubsystemRequest(req)
		if req.WantReply {
			req.Reply(ok, nil)
		}
		if !ok {
			continue
		}
		handlers := sftp.Handlers{
			FileGet:  refusingReader{},
			FileCmd:  refusingCmder{},
			FileList: refusingLister{},
			FilePut:  &deviceWriter{vault: s.Vault, device: device, log: s.log, closeConn: ch.Close},
		}
		server := sftp.NewRequestServer(ch, handlers)
		_ = server.Serve()
		server.Close()
		return
	}
}

// isSFTPSubsystemRequest decodes an SSH_MSG_CHANNEL_REQUEST "subsystem"
// payload (a length-prefixed string) and reports whether it names
// "sftp" -- the one subsystem this server offers.
func isSFTPSubsystemRequest(req *ssh.Request) bool {
	if req.Type != "subsystem" || len(req.Payload) < 4 {
		return false
	}
	n := binary.BigEndian.Uint32(req.Payload[:4])
	if uint64(4+n) > uint64(len(req.Payload)) {
		return false
	}
	return string(req.Payload[4:4+n]) == "sftp"
}

// refusingReader/refusingCmder/refusingLister implement pkg/sftp's
// FileReader/FileCmder/FileLister by refusing everything -- the
// write-only half of #394's "no listing, reading, deleting, renaming or
// overwriting" requirement. Filewrite (deviceWriter, below) is the only
// operation this server ever permits.
type refusingReader struct{}

func (refusingReader) Fileread(*sftp.Request) (io.ReaderAt, error) {
	return nil, sftp.ErrSSHFxPermissionDenied
}

type refusingCmder struct{}

func (refusingCmder) Filecmd(*sftp.Request) error {
	return sftp.ErrSSHFxPermissionDenied
}

type refusingLister struct{}

func (refusingLister) Filelist(*sftp.Request) (sftp.ListerAt, error) {
	return nil, sftp.ErrSSHFxPermissionDenied
}

// deviceWriter is the FileWriter for one authenticated connection. It
// only ever hands out a pendingWrite scoped to this connection's own
// device -- there is no path in this type that can address another
// device's folder, which is what makes "a login can only write inside
// its own folder" true regardless of what path the client asks for.
type deviceWriter struct {
	vault  *backupvault.Vault
	device string
	log    *slog.Logger
	// closeConn ends this connection's SFTP channel. It is called only
	// from the panic-recovery wrapping below -- see pendingWrite's
	// closeConn field doc for why closing the connection, rather than
	// trying to fabricate a return value, is the right response to a
	// recovered panic here.
	closeConn func() error
}

// kindForFilename maps an upload's destination name to the vault kind
// it should be stored as, matching the wizard's script (round 45):
// dst-path=<device>.backup and dst-path=<device>.rsc. Case-insensitive
// since RouterOS file names are not.
func kindForFilename(name string) (string, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".backup"):
		return backupvault.KindBackup, true
	case strings.HasSuffix(lower, ".rsc"):
		return backupvault.KindRsc, true
	default:
		return "", false
	}
}

// Filewrite is called for every SSH_FXP_OPEN with write/create/truncate
// flags (Put, and the write half of Open) -- see pkg/sftp's
// request.go:(*Request).open. req.Filepath has already been cleaned and
// rooted at "/" by the library, so a ".." segment cannot escape it; this
// still refuses anything but a single flat file name, so there is no
// subdirectory for a login to address even inside its own folder.
func (w *deviceWriter) Filewrite(req *sftp.Request) (wa io.WriterAt, err error) {
	// pkg/sftp calls Filewrite (and the WriterAt it returns) from its
	// own per-request worker goroutines, not from any goroutine this
	// package starts -- see the package doc comment on recoverAndClose
	// below for why that means this recover has to sit here rather than
	// on handleConn/handleSession.
	defer recoverAndClose(w.log, w.device, "Filewrite", w.closeConn, &err)
	name := strings.TrimPrefix(req.Filepath, "/")
	if name == "" || strings.Contains(name, "/") {
		w.log.Warn(fmt.Sprintf("%s tried to open %q -- refused: not a plain file name", w.device, req.Filepath))
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	kind, ok := kindForFilename(name)
	if !ok {
		w.log.Warn(fmt.Sprintf("%s tried to upload %q -- refused: destination must end .backup or .rsc", w.device, name))
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	return &pendingWrite{vault: w.vault, device: w.device, kind: kind, log: w.log, closeConn: w.closeConn}, nil
}

// pendingWrite buffers one upload in memory until its SFTP Close
// arrives, then -- and only then -- asks the vault to commit it. RouterOS's
// own transfer is a strictly sequential sequence of WriteAt calls
// (measured, #394 note 10510), so a simple growable buffer is enough;
// nothing here needs to support out-of-order writes to behave correctly
// for the one client this server serves, and refusing anything past the
// cap keeps a hostile out-of-order write from being a memory amplifier
// regardless.
//
// This is also where "an interrupted transfer is discarded" lives, and
// it needs the TransferError hook below to work: pkg/sftp's
// RequestServer.Serve calls Close() on every still-open handle when the
// connection drops (its own doc comment: "possible on dropped
// connections, client crashes, etc."), so Close alone cannot tell a real
// client-issued SSH_FXP_CLOSE apart from that cleanup sweep. It calls
// TransferError first, with the error that ended the loop, only on the
// cleanup path -- a graceful close never calls it (see
// request-server.go's closeRequest, which calls only req.close()). aborted
// records that, and Close checks it before ever asking the vault to
// commit.
type pendingWrite struct {
	vault  *backupvault.Vault
	device string
	kind   string
	log    *slog.Logger
	// closeConn ends this connection's SFTP channel; see
	// recoverAndClose's doc comment for why a recovered panic closes
	// the connection instead of inventing a return value.
	closeConn func() error

	mu      sync.Mutex
	buf     []byte
	overCap bool
	closed  bool
	aborted bool
}

// recoverAndClose is deferred directly -- as logging.Recover's own doc
// comment explains, one layer of closure in between would stop
// recover() from seeing the panic at all -- at the top of every method
// pkg/sftp calls straight from its own per-request worker goroutines
// (request-server.go's packetWorker, spawned from Serve). Those
// goroutines belong to pkg/sftp, not this package, so the
// defer logging.Recover(...) already covering handleConn/handleSession's
// own goroutines (see ListenAndServe/handleConn) never runs in the same
// goroutine as a call into Filewrite, WriteAt or Close and could not
// catch a panic there.
//
// A recovered panic here has no good return value to invent -- the
// call stopped mid-way through, not at a point that produced one -- so
// this closes the SFTP channel instead. That lands the transfer on the
// same "interrupted, nothing kept" path an ordinary dropped connection
// already takes (TransferError/aborted, above), rather than risking
// pkg/sftp treating a zero-valued response as a successful write or
// commit.
func recoverAndClose(log *slog.Logger, device, op string, closeConn func() error, errp *error) {
	if r := recover(); r != nil {
		log.Error(fmt.Sprintf("recovered from a panic in %s for %s -- closing the connection: %v\n%s", op, device, r, debug.Stack()))
		*errp = fmt.Errorf("backupsftp: internal error")
		if closeConn != nil {
			closeConn()
		}
	}
}

// TransferError implements sftp.TransferError -- see the type doc
// comment above. It only ever assigns a bool under a lock already held
// by every other method here, so it has no recover of its own: there is
// nothing in it that can panic.
func (w *pendingWrite) TransferError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.aborted = true
}

func (w *pendingWrite) WriteAt(p []byte, off int64) (n int, err error) {
	defer recoverAndClose(w.log, w.device, "WriteAt", w.closeConn, &err)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.overCap {
		return 0, fmt.Errorf("backupsftp: file exceeds the %d-byte cap", backupvault.MaxFileBytes)
	}
	// off is the client's requested write offset, passed straight
	// through by pkg/sftp from the wire (a uint64 cast to int64) with
	// no validation of its own. A negative value, or one that would
	// overflow when added to len(p), must be rejected here before
	// either ever reaches the slice expression below. The check is
	// written as a subtraction from the cap rather than off+len(p) so
	// that the check itself cannot overflow: off is already known
	// non-negative and at most MaxFileBytes at this point, so
	// MaxFileBytes-off is always in [0, MaxFileBytes].
	if off < 0 || off > backupvault.MaxFileBytes || int64(len(p)) > backupvault.MaxFileBytes-off {
		w.overCap = true
		w.buf = nil
		return 0, fmt.Errorf("backupsftp: file exceeds the %d-byte cap", backupvault.MaxFileBytes)
	}
	end := off + int64(len(p))
	if int64(len(w.buf)) < end {
		grown := make([]byte, end)
		copy(grown, w.buf)
		w.buf = grown
	}
	copy(w.buf[off:end], p)
	return len(p), nil
}

// Close commits the buffered file to the vault. Its error return
// reaches the SFTP client as the response to its own Close request (see
// pkg/sftp's request.go:(*Request).close), so a refusal here is not
// silent to the router either: `/tool fetch` reports the transfer
// failed, which is honest -- the alternative is the router believing the
// push succeeded and deleting its only local copy (the wizard's script
// removes the source file immediately after each fetch).
func (w *pendingWrite) Close() (err error) {
	defer recoverAndClose(w.log, w.device, "Close", w.closeConn, &err)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.aborted {
		w.log.Warn(fmt.Sprintf("discarded an interrupted push from %s: the connection ended before %s completed",
			w.device, w.kind))
		return nil
	}
	if w.overCap {
		// Already logged by WriteAt's refusal path via the vault having
		// never been asked; log once more here with the device, since
		// WriteAt has no device field to log with.
		w.log.Warn(fmt.Sprintf("refused a push from %s: %s exceeded the %d-byte cap -- nothing kept",
			w.device, w.kind, backupvault.MaxFileBytes))
		return fmt.Errorf("backupsftp: file exceeds the %d-byte cap", backupvault.MaxFileBytes)
	}
	if err := w.vault.Store(w.device, w.kind, w.buf, time.Now()); err != nil {
		return err
	}
	return nil
}
