// SPDX-License-Identifier: AGPL-3.0-only

package api

// The config editor's HTTP surface (#1347). internal/config holds the
// text work -- masking, validating, Carry forward, the header -- and
// internal/configsnap the snapshots; this file is the "who and when":
// admins only, the password again to open, and a fifteen-minute unlock,
// held per session, that Show-secrets, Download and reading a snapshot
// back all need.
//
// MikroView never writes the config file. Everything here reads it or
// hands the admin a download; the operator puts the file on their host.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/configsnap"
)

// editorUnlockWindow is how long the password typed to open the editor
// covers Show-secrets, Download and reading a snapshot back (owner,
// #1347). Fixed from the moment of unlocking, not renewed by use: the
// owner's rule is "downloading needs the password again if the sign-in
// was more than 15 minutes ago".
const editorUnlockWindow = 15 * time.Minute

// editorStateIdle is how long an editor session's remembered secret
// values outlive their last use, so a signed-out session's entry does
// not sit in memory until the process restarts.
const editorStateIdle = 12 * time.Hour

// editorMaxBodyBytes bounds a request carrying a config text: the
// largest text a snapshot may hold, plus room for JSON escaping. Larger
// than maxJSONBodyBytes on purpose -- the example config alone is half
// that -- and only reachable by an admin session.
const editorMaxBodyBytes = 2*configsnap.MaxTextBytes + 64*1024

// ConfigEditor is what the editor needs from main.go.
type ConfigEditor struct {
	// Path is the config file MikroView read at start-up, empty when it
	// started on defaults with no file.
	Path string
	// StartupText is that file's bytes as they were at start-up, to tell
	// the admin when the file on disk has changed since.
	StartupText []byte
	// RunningVersion is this build's release for the header and the
	// download's name: "0.6.1" for a release, "0.6.1-dev.abc1234" for
	// anything else. No leading "v".
	RunningVersion string
	// Snapshots is the snapshot store, nil when there is none --
	// SnapshotsUnavailable then says why, in words for the admin.
	Snapshots            *configsnap.Store
	SnapshotsUnavailable string
}

// editorSession is one browser session's editor state.
type editorSession struct {
	// unlockedAt is when this session last typed the password to open
	// the editor; zero when it never has.
	unlockedAt time.Time
	// values is the secret each placeholder in this session's editor
	// text stands for, exactly as written in the file it came from.
	//
	// One value per key: the text last loaded -- the running config at
	// open, or a snapshot read back -- supplies it, and a secret typed
	// over a placeholder replaces it on the next validate or Carry
	// forward. Kept past the unlock window, because validate and Carry
	// forward need it and do not need the unlock; the window guards
	// reading the values out (Show, Download, a snapshot), not holding
	// them, and the process holds the running config's secrets anyway.
	values   map[string]string
	lastUsed time.Time
}

// editorState is every session's editor state, by session id.
type editorState struct {
	mu       sync.Mutex
	sessions map[string]*editorSession
}

// session returns sessionID's state, created when missing, pruning
// entries idle past editorStateIdle on the way.
func (e *editorState) session(sessionID string, now time.Time) *editorSession {
	if e.sessions == nil {
		e.sessions = map[string]*editorSession{}
	}
	for id, st := range e.sessions {
		if now.Sub(st.lastUsed) > editorStateIdle {
			delete(e.sessions, id)
		}
	}
	st, ok := e.sessions[sessionID]
	if !ok {
		st = &editorSession{values: map[string]string{}}
		e.sessions[sessionID] = st
	}
	st.lastUsed = now
	return st
}

// editorValues returns a copy of the caller's remembered secret values.
func (s *Server) editorValues(r *http.Request) map[string]string {
	id := requestSessionID(r)
	s.editor.mu.Lock()
	defer s.editor.mu.Unlock()
	out := map[string]string{}
	if id == "" {
		return out
	}
	for k, v := range s.editor.session(id, time.Now()).values {
		out[k] = v
	}
	return out
}

// rememberEditorValues records values for the caller's session. replace
// drops what was held first (a newly loaded text brings its own set);
// otherwise values are merged over it. A value that is itself a
// placeholder is never recorded -- that would make a mask stand for
// itself.
func (s *Server) rememberEditorValues(r *http.Request, values map[string]string, replace bool) {
	id := requestSessionID(r)
	if id == "" {
		return
	}
	s.editor.mu.Lock()
	defer s.editor.mu.Unlock()
	st := s.editor.session(id, time.Now())
	if replace {
		st.values = map[string]string{}
	}
	for k, v := range values {
		if len(config.SecretPlaceholders(v)) > 0 {
			continue
		}
		st.values[k] = v
	}
}

// editorUnlocked reports whether the caller's session opened the editor
// with its password within the last editorUnlockWindow.
func (s *Server) editorUnlocked(r *http.Request, now time.Time) bool {
	id := requestSessionID(r)
	if id == "" {
		return false
	}
	s.editor.mu.Lock()
	defer s.editor.mu.Unlock()
	st := s.editor.session(id, now)
	return !st.unlockedAt.IsZero() && now.Sub(st.unlockedAt) <= editorUnlockWindow
}

// writeReauth is the refusal every unlock-gated route gives once the
// window has lapsed: 401 with {"reauth":true}, the frontend's cue to ask
// for the password again rather than to sign the admin out.
func writeReauth(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]any{
		"reauth": true,
		"error":  "the config editor's unlock has lapsed -- enter your password again",
	})
}

func writeEditorError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// editorGate is the check every editor route starts with: an admin
// caller, and an editor this server was built with. It writes the
// refusal and reports false, so a caller's only job is to stop.
func (s *Server) editorGate(w http.ResponseWriter, r *http.Request) bool {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return false
	}
	if s.ConfigEditor == nil {
		writeEditorError(w, http.StatusServiceUnavailable, "the config editor is not available on this server")
		return false
	}
	return true
}

// decodeEditorBody decodes a request carrying a config text, under
// editorMaxBodyBytes rather than the general JSON cap.
func decodeEditorBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, editorMaxBodyBytes)
	return json.NewDecoder(r.Body).Decode(v)
}

// scrubSecrets replaces any secret value that found its way into a
// message with its placeholder. config.Validate never words a value
// into its messages, but a YAML type error quotes the offending text,
// and the rail must not be where a secret shows up unasked.
func scrubSecrets(problems []config.TextProblem, values map[string]string) []config.TextProblem {
	out := make([]config.TextProblem, 0, len(problems))
	for _, p := range problems {
		for key, raw := range values {
			for _, v := range []string{raw, strings.Trim(raw, `"'`)} {
				if len(v) >= 4 {
					p.Message = strings.ReplaceAll(p.Message, v, config.SecretPlaceholder(key))
				}
			}
		}
		out = append(out, p)
	}
	return out
}

type editorOpenRequest struct {
	Password string `json:"password"`
}

type editorOpenResponse struct {
	Text              string         `json:"text"`
	Path              string         `json:"path"`
	ChangedSinceStart bool           `json:"changedSinceStart"`
	Header            *config.Header `json:"header"`
	SchemaGuess       int            `json:"schemaGuess"`
	RunningVersion    string         `json:"runningVersion"`
	RunningSchema     int            `json:"runningSchema"`
}

// handleConfigEditorOpen checks the caller's password again, unlocks the
// editor for this session for editorUnlockWindow, and returns the
// running config's text with its secrets masked.
//
// The text is re-read from the path MikroView started with, not served
// from memory, so what the admin edits is the file as it is now; if that
// differs from what was read at start-up, changedSinceStart says so --
// the running process is still on the old one until a restart.
//
// The password re-check is handleTOTPDelete's, bucket and all: a guess
// at a live credential by a caller who already holds a session, which is
// exactly where a stolen cookie puts an attacker.
func (s *Server) handleConfigEditorOpen(w http.ResponseWriter, r *http.Request) {
	if !s.editorGate(w, r) {
		return
	}
	user := userFromContext(r)
	var req editorOpenRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	now := time.Now()
	userKey := passwordRecheckLimiterKey(user.Username)
	if !s.LoginLimiter.Reserve(userKey, now) {
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}
	if _, err := s.Auth.Authenticate(user.Username, req.Password, now); err != nil {
		writeUnauthorized(w, "incorrect password")
		return
	}
	s.LoginLimiter.Release(userKey, now)

	ed := s.ConfigEditor
	var raw []byte
	if ed.Path != "" {
		// #nosec G304 -- this deployment's own config path, fixed at
		// start-up by main.go; never taken from the request.
		b, err := os.ReadFile(ed.Path)
		if err != nil {
			writeEditorError(w, http.StatusInternalServerError, fmt.Sprintf("reading %s: %v", ed.Path, err))
			return
		}
		raw = b
	}
	text := string(raw)
	masked, values := config.MaskSecrets(text)

	sessionID := requestSessionID(r)
	s.editor.mu.Lock()
	st := s.editor.session(sessionID, now)
	st.unlockedAt = now
	st.values = values
	s.editor.mu.Unlock()

	resp := editorOpenResponse{
		Text:              masked,
		Path:              ed.Path,
		ChangedSinceStart: !bytes.Equal(raw, ed.StartupText),
		SchemaGuess:       config.GuessSchema(text),
		RunningVersion:    ed.RunningVersion,
		RunningSchema:     config.CurrentSchema,
	}
	if h, ok := config.ParseHeader(text); ok {
		resp.Header = &h
	}
	s.Audit.Record(user.Username, "config.editor.open", ed.Path, "opened the config editor; unlocked for 15 minutes")
	writeJSON(w, http.StatusOK, resp)
}

type editorSummaryResponse struct {
	Path              string         `json:"path"`
	Header            *config.Header `json:"header"`
	SchemaGuess       int            `json:"schemaGuess"`
	RunningVersion    string         `json:"runningVersion"`
	RunningSchema     int            `json:"runningSchema"`
	SnapshotCount     int            `json:"snapshotCount"`
	ChangedSinceStart bool           `json:"changedSinceStart"`
}

// handleConfigEditorSummary is what the Engine Room's Config card shows
// before the editor is opened: which file, which schema and release it
// says it is for, how many snapshots are kept. No password and no
// unlock, because nothing in it is secret -- the file's text is not
// sent, only facts about it. It reads the file as it is on disk now; an
// unreadable file reports its header as null and changedSinceStart true.
func (s *Server) handleConfigEditorSummary(w http.ResponseWriter, r *http.Request) {
	if !s.editorGate(w, r) {
		return
	}
	ed := s.ConfigEditor
	var raw []byte
	readable := true
	if ed.Path != "" {
		// #nosec G304 -- this deployment's own config path, fixed at
		// start-up by main.go; never taken from the request.
		b, err := os.ReadFile(ed.Path)
		if err != nil {
			readable = false
		}
		raw = b
	}
	text := string(raw)
	resp := editorSummaryResponse{
		Path:              ed.Path,
		SchemaGuess:       config.GuessSchema(text),
		RunningVersion:    ed.RunningVersion,
		RunningSchema:     config.CurrentSchema,
		ChangedSinceStart: !readable || !bytes.Equal(raw, ed.StartupText),
	}
	if h, ok := config.ParseHeader(text); ok {
		resp.Header = &h
	}
	if ed.Snapshots != nil {
		resp.SnapshotCount = ed.Snapshots.Count()
	}
	writeJSON(w, http.StatusOK, resp)
}

type editorTextRequest struct {
	Text string `json:"text"`
}

// handleConfigValidate runs the text through the same checks start-up
// would (config.ValidateText), each problem placed on its line.
func (s *Server) handleConfigValidate(w http.ResponseWriter, r *http.Request) {
	if !s.editorGate(w, r) {
		return
	}
	var req editorTextRequest
	if err := decodeEditorBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	values := s.editorValues(r)
	text := config.UnmaskSecrets(req.Text, values)
	// A secret typed in the clear is remembered, so masking it on the
	// way back out (Carry forward) and the download both still have it.
	_, typed := config.MaskSecrets(text)
	s.rememberEditorValues(r, typed, false)
	for k, v := range typed {
		values[k] = v
	}
	writeJSON(w, http.StatusOK, map[string]any{"problems": scrubSecrets(config.ValidateText(text), values)})
}

type carryForwardResponse struct {
	Text     string               `json:"text"`
	Changes  []config.Change      `json:"changes"`
	Problems []config.TextProblem `json:"problems"`
}

// handleConfigCarryForward rewrites the text for this build
// (config.CarryForward) and, before handing the rewrite back, snapshots
// the text as it was -- labelled with the version it was for -- so the
// file before the rewrite can always be had again (#1347, answer 3).
//
// The snapshot is skipped when the rewrite changes nothing, and when the
// newest before-carry-forward snapshot already holds this exact text:
// pressing Carry forward twice must not push five manual snapshots out.
// A snapshot that cannot be taken (no retention key, or a secret with no
// value) does not stop the rewrite -- nothing is written to the host
// either way -- but is listed among the changes so the admin knows.
func (s *Server) handleConfigCarryForward(w http.ResponseWriter, r *http.Request) {
	if !s.editorGate(w, r) {
		return
	}
	user := userFromContext(r)
	var req editorTextRequest
	if err := decodeEditorBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	values := s.editorValues(r)
	text := config.UnmaskSecrets(req.Text, values)
	out, changes, problems := config.CarryForward(text, s.ConfigEditor.RunningVersion)
	if changes == nil {
		changes = []config.Change{}
	}

	if out != text {
		if note := s.snapshotBeforeCarryForward(user.Username, text); note != "" {
			changes = append(changes, config.Change{Kind: "snapshot", Note: note})
		}
	}

	masked, outValues := config.MaskSecrets(out)
	s.rememberEditorValues(r, outValues, false)
	for k, v := range outValues {
		values[k] = v
	}
	if problems == nil {
		problems = []config.TextProblem{}
	}
	writeJSON(w, http.StatusOK, carryForwardResponse{Text: masked, Changes: changes, Problems: scrubSecrets(problems, values)})
}

// snapshotBeforeCarryForward keeps text as a before-carry-forward
// snapshot and returns a line for the changes list, "" when a snapshot
// was not needed.
func (s *Server) snapshotBeforeCarryForward(by, text string) string {
	store := s.ConfigEditor.Snapshots
	if store == nil {
		return "no snapshot of the text as it was: " + s.ConfigEditor.SnapshotsUnavailable
	}
	if keys := config.SecretPlaceholders(text); len(keys) > 0 {
		return "no snapshot of the text as it was: it still holds masked secrets with no value to put back (" + strings.Join(keys, ", ") + ") -- open the editor again first"
	}
	if newest, ok := store.Newest(); ok && newest.Reason == configsnap.ReasonBeforeCarryForward && newest.Text == text {
		return ""
	}
	meta, err := store.Add(snapshotOf(text, by, configsnap.ReasonBeforeCarryForward, ""))
	if err != nil {
		return "no snapshot of the text as it was: " + err.Error()
	}
	s.Audit.Record(by, "config.snapshot", meta.ID, "before carry forward")
	return fmt.Sprintf("saved the text as it was, before this rewrite, as a snapshot (%s)", meta.TakenAt.Format(time.RFC3339))
}

// snapshotOf labels text with the schema and release it was for.
func snapshotOf(text, by, reason, note string) configsnap.Snapshot {
	snap := configsnap.Snapshot{Text: text}
	snap.By, snap.Reason, snap.Note = by, reason, note
	if h, ok := config.ParseHeader(text); ok {
		snap.Schema, snap.Version = h.Schema, h.WrittenBy
	} else {
		snap.Schema = config.GuessSchema(text)
	}
	return snap
}

// handleConfigDownload hands back the text as the file to put on the
// host: secrets put back, the header written for this build, named
// config.v<running>.yaml so it sits beside the previous file rather
// than over it.
func (s *Server) handleConfigDownload(w http.ResponseWriter, r *http.Request) {
	if !s.editorGate(w, r) {
		return
	}
	user := userFromContext(r)
	var req editorTextRequest
	if err := decodeEditorBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !s.editorUnlocked(r, time.Now()) {
		writeReauth(w)
		return
	}
	text := config.UnmaskSecrets(req.Text, s.editorValues(r))
	if keys := config.SecretPlaceholders(text); len(keys) > 0 {
		writeEditorError(w, http.StatusBadRequest, "the text still holds masked secrets with no value to put back ("+strings.Join(keys, ", ")+") -- type them in, or open the editor again")
		return
	}
	file := config.StampHeader(text, s.ConfigEditor.RunningVersion)
	name := "config.v" + s.ConfigEditor.RunningVersion + ".yaml"

	s.Audit.Record(user.Username, "config.download", name, "downloaded a config file from the editor")
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(file))
}

// handleConfigEditorReveal returns the real value behind every
// placeholder in the caller's editor text, within the unlock window.
// Each value is exactly as written in the file, quotes included, so
// putting it in place of its placeholder gives back the original text.
func (s *Server) handleConfigEditorReveal(w http.ResponseWriter, r *http.Request) {
	if !s.editorGate(w, r) {
		return
	}
	if !s.editorUnlocked(r, time.Now()) {
		writeReauth(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": s.editorValues(r)})
}

// snapshotView is one snapshot's metadata as the API gives it -- the
// field names the web side was built against, which differ from the
// store's own: when rather than takenAt, why rather than reason, and a
// note that is null rather than "" when there is none.
type snapshotView struct {
	ID      string    `json:"id"`
	When    time.Time `json:"when"`
	By      string    `json:"by"`
	Schema  int       `json:"schema"`
	Version string    `json:"version"`
	Why     string    `json:"why"`
	Note    *string   `json:"note"`
}

func viewOf(m configsnap.Meta) snapshotView {
	v := snapshotView{ID: m.ID, When: m.TakenAt, By: m.By, Schema: m.Schema, Version: m.Version, Why: m.Reason}
	if m.Note != "" {
		note := m.Note
		v.Note = &note
	}
	return v
}

type snapshotsListResponse struct {
	// Available is false when this server keeps no snapshots, with
	// Reason saying why in words for the admin (null when available).
	Available bool           `json:"available"`
	Reason    *string        `json:"reason"`
	Keep      int            `json:"keep"`
	Snapshots []snapshotView `json:"snapshots"`
}

// handleConfigSnapshotsList lists the snapshots, newest first, without
// their text.
func (s *Server) handleConfigSnapshotsList(w http.ResponseWriter, r *http.Request) {
	if !s.editorGate(w, r) {
		return
	}
	resp := snapshotsListResponse{Keep: configsnap.Keep, Snapshots: []snapshotView{}}
	if store := s.ConfigEditor.Snapshots; store != nil {
		resp.Available = true
		for _, m := range store.List() {
			resp.Snapshots = append(resp.Snapshots, viewOf(m))
		}
	} else {
		reason := s.ConfigEditor.SnapshotsUnavailable
		resp.Reason = &reason
	}
	writeJSON(w, http.StatusOK, resp)
}

type snapshotCreateRequest struct {
	Text string `json:"text"`
	Note string `json:"note"`
}

// snapshotStore is the store, or writes the refusal and returns nil.
func (s *Server) snapshotStore(w http.ResponseWriter) *configsnap.Store {
	if s.ConfigEditor.Snapshots == nil {
		writeEditorError(w, http.StatusServiceUnavailable, "config snapshots are unavailable: "+s.ConfigEditor.SnapshotsUnavailable)
		return nil
	}
	return s.ConfigEditor.Snapshots
}

// handleConfigSnapshotCreate keeps the editor's text as a manual
// snapshot, secrets put back so the snapshot is a whole, usable file.
func (s *Server) handleConfigSnapshotCreate(w http.ResponseWriter, r *http.Request) {
	if !s.editorGate(w, r) {
		return
	}
	user := userFromContext(r)
	var req snapshotCreateRequest
	if err := decodeEditorBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	store := s.snapshotStore(w)
	if store == nil {
		return
	}
	text := config.UnmaskSecrets(req.Text, s.editorValues(r))
	if keys := config.SecretPlaceholders(text); len(keys) > 0 {
		writeEditorError(w, http.StatusBadRequest, "the text still holds masked secrets with no value to put back ("+strings.Join(keys, ", ")+") -- open the editor again first")
		return
	}
	meta, err := store.Add(snapshotOf(text, user.Username, configsnap.ReasonManual, strings.TrimSpace(req.Note)))
	if errors.Is(err, configsnap.ErrTooLarge) {
		writeEditorError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	if err != nil {
		writeEditorError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Record(user.Username, "config.snapshot", meta.ID, "manual")
	writeJSON(w, http.StatusCreated, viewOf(meta))
}

type snapshotGetResponse struct {
	snapshotView
	Text string `json:"text"`
}

// handleConfigSnapshotGet returns one snapshot, its secrets masked the
// way the editor's own text is. Within the unlock window only: a
// snapshot is a whole config file, secrets and all, and this is how it
// is downloaded or loaded into the editor. Its secret values become the
// ones this session's placeholders stand for (see editorSession.values).
func (s *Server) handleConfigSnapshotGet(w http.ResponseWriter, r *http.Request) {
	if !s.editorGate(w, r) {
		return
	}
	store := s.snapshotStore(w)
	if store == nil {
		return
	}
	snap, err := store.Get(r.PathValue("id"))
	if err != nil {
		writeEditorError(w, http.StatusNotFound, "no such snapshot")
		return
	}
	if !s.editorUnlocked(r, time.Now()) {
		writeReauth(w)
		return
	}
	masked, values := config.MaskSecrets(snap.Text)
	s.rememberEditorValues(r, values, false)
	writeJSON(w, http.StatusOK, snapshotGetResponse{snapshotView: viewOf(snap.Meta), Text: masked})
}

// handleConfigSnapshotDelete removes one snapshot.
func (s *Server) handleConfigSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	if !s.editorGate(w, r) {
		return
	}
	user := userFromContext(r)
	store := s.snapshotStore(w)
	if store == nil {
		return
	}
	id := r.PathValue("id")
	if err := store.Delete(id); errors.Is(err, configsnap.ErrNotFound) {
		writeEditorError(w, http.StatusNotFound, "no such snapshot")
		return
	} else if err != nil {
		writeEditorError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Audit.Record(user.Username, "config.snapshot.delete", id, "")
	w.WriteHeader(http.StatusNoContent)
}

// configEditorRoutes is the editor's route table, shared by the normal
// server and setup-only mode (setuponly.go) so the two can never serve
// different editors.
func (s *Server) configEditorRoutes() []route {
	return []route{
		{http.MethodPost, "/api/config/editor/open", s.handleConfigEditorOpen},
		{http.MethodGet, "/api/config/editor/summary", s.handleConfigEditorSummary},
		{http.MethodGet, "/api/config/editor/reveal", s.handleConfigEditorReveal},
		{http.MethodPost, "/api/config/validate", s.handleConfigValidate},
		{http.MethodPost, "/api/config/carry-forward", s.handleConfigCarryForward},
		{http.MethodPost, "/api/config/download", s.handleConfigDownload},
		{http.MethodGet, "/api/config/snapshots", s.handleConfigSnapshotsList},
		{http.MethodPost, "/api/config/snapshots", s.handleConfigSnapshotCreate},
		{http.MethodGet, "/api/config/snapshots/{id}", s.handleConfigSnapshotGet},
		{http.MethodDelete, "/api/config/snapshots/{id}", s.handleConfigSnapshotDelete},
	}
}
