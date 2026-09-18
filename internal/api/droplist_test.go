// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/droplist"
)

// droplistTestServer wires a fresh droplist.Store into an admin test
// server the same way main.go does for a real deployment (SetAuditor,
// SetOwnRanges against the same RouterState) -- newTestServer's own
// Server leaves Droplist nil, since no route touched it before #1224.
func droplistTestServer(t *testing.T) (*Server, *httptest.Server, *http.Client) {
	t.Helper()
	s := newAuthTestServer(t)
	ds, err := droplist.Open("")
	if err != nil {
		t.Fatalf("droplist.Open: %v", err)
	}
	ds.SetAuditor(s.Audit)
	ds.SetOwnRanges(s.RouterState)
	s.Droplist = ds
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	admin := setUpAdmin(t, ts)
	return s, ts, admin
}

// deleteNoBody is deleteJSON (entities_test.go) without a body -- what
// DELETE /api/droplist/{cidr...} and DELETE /api/droplist/key both take,
// since neither reads anything from the request beyond the path/token.
func deleteNoBody(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestDroplistAddListShowsItThenRemoveGone(t *testing.T) {
	_, ts, admin := droplistTestServer(t)

	createResp := postJSON(t, admin, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "203.0.114.0/24", Reason: "scanning our SSH port"})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", createResp.StatusCode)
	}
	var created droplistEntryResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.CIDR != "203.0.114.0/24" || created.Reason != "scanning our SSH port" {
		t.Errorf("unexpected created entry: %+v", created)
	}

	listResp, err := admin.Get(ts.URL + "/api/droplist")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	var list droplistListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.ListName != droplist.ListName {
		t.Errorf("listName = %q, want %q", list.ListName, droplist.ListName)
	}
	if len(list.Entries) != 1 || list.Entries[0].CIDR != "203.0.114.0/24" {
		t.Fatalf("expected the created entry to be listed, got %+v", list.Entries)
	}

	delResp := deleteNoBody(t, admin, ts.URL+"/api/droplist/203.0.114.0/24")
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", delResp.StatusCode)
	}

	afterResp, err := admin.Get(ts.URL + "/api/droplist")
	if err != nil {
		t.Fatal(err)
	}
	defer afterResp.Body.Close()
	var after droplistListResponse
	if err := json.NewDecoder(afterResp.Body).Decode(&after); err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) != 0 {
		t.Errorf("expected no entries after removal, got %+v", after.Entries)
	}

	// A second delete of the same, now-gone range is a 404.
	secondDel := deleteNoBody(t, admin, ts.URL+"/api/droplist/203.0.114.0/24")
	defer secondDel.Body.Close()
	if secondDel.StatusCode != http.StatusNotFound {
		t.Errorf("delete of an already-removed entry: status = %d, want 404", secondDel.StatusCode)
	}
}

// TestDroplistDeleteBodiedFormRemovesEntry is the frontend's actual
// request shape (frontend/src/lib/api.ts's deleteDroplistEntry): a
// bodied DELETE to the bare collection URL, CIDR in a JSON body, not on
// the path -- the shape handleDroplistDelete's path-segment route
// ({cidr...}) cannot answer at all (v0.6.0 pre-release audit: a bodied
// DELETE /api/droplist 301-redirects to "/api/droplist/", losing the
// body's the-only-copy CIDR to an empty PathValue and never removing
// the entry).
func TestDroplistDeleteBodiedFormRemovesEntry(t *testing.T) {
	_, ts, admin := droplistTestServer(t)

	postJSON(t, admin, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "203.0.114.0/24", Reason: "scanning our SSH port"}).Body.Close()

	delResp := deleteJSON(t, admin, ts.URL+"/api/droplist", map[string]string{"cidr": "203.0.114.0/24"})
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(delResp.Body)
		t.Fatalf("delete status = %d, want 204: %s", delResp.StatusCode, body)
	}

	listResp, err := admin.Get(ts.URL + "/api/droplist")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	var list droplistListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) != 0 {
		t.Errorf("expected no entries after the bodied delete, got %+v", list.Entries)
	}
}

func TestDroplistAddPrivateRangeRefused(t *testing.T) {
	_, ts, admin := droplistTestServer(t)
	resp := postJSON(t, admin, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "10.0.0.0/24", Reason: "test"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not a public") {
		t.Errorf("body = %q, want it to name the validation rule", body)
	}
}

func TestDroplistAddDuplicateConflict(t *testing.T) {
	_, ts, admin := droplistTestServer(t)
	postJSON(t, admin, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "203.0.114.0/24", Reason: "first"}).Body.Close()

	resp := postJSON(t, admin, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "203.0.114.0/24", Reason: "again"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestDroplistAddUnknownFlagRefused(t *testing.T) {
	_, ts, admin := droplistTestServer(t)
	resp := postJSON(t, admin, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "203.0.114.0/24", Reason: "test", FlagID: "does-not-exist"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "no flag with that id") {
		t.Errorf("body = %q", body)
	}
}

// TestDroplistWriteRoutesAreAdminOnly is a focused version of what
// TestAuthorizationMatrixIsEnforced already proves for every route --
// kept here too since it is the one place a reviewer of this feature
// alone would look for it.
func TestDroplistWriteRoutesAreAdminOnly(t *testing.T) {
	_, ts, admin := droplistTestServer(t)
	postJSON(t, admin, ts.URL+"/api/auth/users", createUserRequest{Username: "operator", Password: "password456", Role: "user"}).Body.Close()
	user := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, user, ts.URL+"/api/auth/login", credentialsRequest{Username: "operator", Password: "password456"}).Body.Close()

	if resp, err := user.Get(ts.URL + "/api/droplist"); err != nil {
		t.Fatal(err)
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("GET /api/droplist as user tier: status = %d, want 403", resp.StatusCode)
		}
	}

	if resp := postJSON(t, user, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "203.0.114.0/24", Reason: "x"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST /api/droplist as user tier: status = %d, want 403", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	if resp := postJSON(t, user, ts.URL+"/api/droplist/key", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST /api/droplist/key as user tier: status = %d, want 403", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	if resp := deleteNoBody(t, user, ts.URL+"/api/droplist/key"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("DELETE /api/droplist/key as user tier: status = %d, want 403", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	if resp := deleteNoBody(t, user, ts.URL+"/api/droplist/203.0.114.0/24"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("DELETE /api/droplist/{cidr} as user tier: status = %d, want 403", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}

// TestDroplistAddWarnsWhenOwnRangesUnknown covers the fail-open case a
// security review flagged: with no router having ever pushed state, an
// otherwise-valid Add still succeeds (refusing would break first-time
// setup), but the 201 must carry a Warning so the operator knows the
// range was not actually checked against the router's own addresses.
func TestDroplistAddWarnsWhenOwnRangesUnknown(t *testing.T) {
	_, ts, admin := droplistTestServer(t)
	resp := postJSON(t, admin, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "203.0.114.0/24", Reason: "test"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var created droplistCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Warning == "" {
		t.Error("expected a warning when no router has ever pushed its own address state")
	}
}

// TestDroplistAddNoWarningOnceOwnRangesAreKnown is the same case once a
// router has actually reported in: the 201 for an unrelated range must
// not carry the warning, since the check genuinely ran.
func TestDroplistAddNoWarningOnceOwnRangesAreKnown(t *testing.T) {
	s, ts, admin := droplistTestServer(t)

	adminUser, ok := s.Auth.ByUsername("admin")
	if !ok {
		t.Fatal("admin account not found")
	}
	ingestRaw, _, err := s.Tokens.Create("router-1", auth.TokenKindIngest, "router-1", adminUser, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}
	pushResp := postIngest(t, ts, ingestRaw,
		`{"kind":"ip-address","page":1,"pages":1,"records":[{"address":"203.0.114.9/24","network":"203.0.114.0","interface":"ether1","comment":""}]}`)
	pushResp.Body.Close()

	resp := postJSON(t, admin, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "203.0.116.0/24", Reason: "unrelated"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var created droplistCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Warning != "" {
		t.Errorf("warning = %q, want empty once a router has reported its own address state", created.Warning)
	}
}

// TestDroplistAddRefusesRoutersOwnRange is the router's-own case end to
// end: a real ingest push carrying an /ip/address entry, then an attempt
// to drop the same range it names.
func TestDroplistAddRefusesRoutersOwnRange(t *testing.T) {
	s, ts, admin := droplistTestServer(t)

	adminUser, ok := s.Auth.ByUsername("admin")
	if !ok {
		t.Fatal("admin account not found")
	}
	ingestRaw, _, err := s.Tokens.Create("router-1", auth.TokenKindIngest, "router-1", adminUser, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}

	pushResp := postIngest(t, ts, ingestRaw,
		`{"kind":"ip-address","page":1,"pages":1,"records":[{"address":"203.0.114.9/24","network":"203.0.114.0","interface":"ether1","comment":""}]}`)
	defer pushResp.Body.Close()
	if pushResp.StatusCode != http.StatusOK {
		t.Fatalf("ingest push status = %d, want 200", pushResp.StatusCode)
	}

	createResp := postJSON(t, admin, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "203.0.114.0/24", Reason: "oops"})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", createResp.StatusCode)
	}
	body, _ := io.ReadAll(createResp.Body)
	if !strings.Contains(string(body), "router's own") {
		t.Errorf("body = %q, want it to mention the router's own range", body)
	}
}

// TestDroplistPullKeyServesTheFeed is the pull key's happy path plus the
// three refusal directions the design calls out: no header, a session
// cookie alone, and the wrong token kind.
func TestDroplistPullKeyServesTheFeed(t *testing.T) {
	_, ts, admin := droplistTestServer(t)
	postJSON(t, admin, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "203.0.114.0/24", Reason: "scanning"}).Body.Close()

	mintResp := postJSON(t, admin, ts.URL+"/api/droplist/key", nil)
	defer mintResp.Body.Close()
	if mintResp.StatusCode != http.StatusCreated {
		t.Fatalf("mint status = %d, want 201", mintResp.StatusCode)
	}
	var minted droplistKeyCreateResponse
	if err := json.NewDecoder(mintResp.Body).Decode(&minted); err != nil {
		t.Fatal(err)
	}
	if minted.Key == "" {
		t.Fatal("expected a raw key in the mint response")
	}
	if cc := mintResp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("mint response Cache-Control = %q, want no-store -- the raw key is shown exactly once", cc)
	}

	pullResp := bearerGet(t, ts.URL+"/api/droplist.rsc", minted.Key)
	defer pullResp.Body.Close()
	if pullResp.StatusCode != http.StatusOK {
		t.Fatalf("pull status = %d, want 200", pullResp.StatusCode)
	}
	if ct := pullResp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := pullResp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	body, err := io.ReadAll(pullResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `address=203.0.114.0/24 comment="mv: scanning"`) {
		t.Errorf("pulled script does not contain the expected entry:\n%s", body)
	}
	if !strings.HasPrefix(string(body), "# mikroview drop list: 1 entries") {
		t.Errorf("pulled script header = %q", strings.SplitN(string(body), "\n", 2)[0])
	}

	// No Authorization header at all.
	noAuthResp, err := http.Get(ts.URL + "/api/droplist.rsc")
	if err != nil {
		t.Fatal(err)
	}
	defer noAuthResp.Body.Close()
	if noAuthResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no-auth status = %d, want 401", noAuthResp.StatusCode)
	}

	// A session cookie alone does not serve it: GET /api/droplist.rsc is
	// not registered on the session-gated mux at all (it lives only on
	// droplistPullRoutes), so a session-authenticated request falls
	// through to the real mux and 404s there -- the same shape
	// TestIngestRouteRejectsASession pins for the ingest push route.
	sessionResp, err := admin.Get(ts.URL + "/api/droplist.rsc")
	if err != nil {
		t.Fatal(err)
	}
	defer sessionResp.Body.Close()
	if sessionResp.StatusCode != http.StatusNotFound {
		t.Errorf("session-only status = %d, want 404 (not registered on the session mux)", sessionResp.StatusCode)
	}
}

// TestDroplistPullKeyBlastRadiusIsTheFeedAndNothingElse pins #1224's
// central safety property: a droplist-pull key reaches exactly one
// route, and an ingest token cannot reach the feed either -- the same
// disjoint-mux guarantee TestBearerMuxesServeOnlyTheirDeclaredRoutes
// checks structurally, exercised here end to end.
func TestDroplistPullKeyBlastRadiusIsTheFeedAndNothingElse(t *testing.T) {
	s, ts, admin := droplistTestServer(t)

	adminUser, ok := s.Auth.ByUsername("admin")
	if !ok {
		t.Fatal("admin account not found")
	}
	ingestRaw, _, err := s.Tokens.Create("router-1", auth.TokenKindIngest, "router-1", adminUser, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}

	ingestOnPull := bearerGet(t, ts.URL+"/api/droplist.rsc", ingestRaw)
	defer ingestOnPull.Body.Close()
	if ingestOnPull.StatusCode == http.StatusOK {
		t.Error("an ingest token reached GET /api/droplist.rsc")
	}

	mintResp := postJSON(t, admin, ts.URL+"/api/droplist/key", nil)
	defer mintResp.Body.Close()
	var minted droplistKeyCreateResponse
	if err := json.NewDecoder(mintResp.Body).Decode(&minted); err != nil {
		t.Fatal(err)
	}

	eventsResp := bearerGet(t, ts.URL+"/api/events", minted.Key)
	defer eventsResp.Body.Close()
	if eventsResp.StatusCode == http.StatusOK {
		t.Error("a droplist-pull key reached GET /api/events -- its blast radius must be the feed and nothing else")
	}

	ingestResp := postIngest(t, ts, minted.Key, validARPPayload)
	defer ingestResp.Body.Close()
	if ingestResp.StatusCode == http.StatusOK {
		t.Error("a droplist-pull key reached POST /api/ingest/routeros -- its blast radius must be the feed and nothing else")
	}
}

// TestDroplistPullKeyRevokeLeavesIngestWorking covers the revoke
// direction: the pull stops working, and an unrelated ingest push --
// using a completely different token -- is unaffected. Mirrors #394's
// "leaves backup pushes working" property for the droplist key's own
// surface.
func TestDroplistPullKeyRevokeLeavesIngestWorking(t *testing.T) {
	s, ts, admin := droplistTestServer(t)

	adminUser, ok := s.Auth.ByUsername("admin")
	if !ok {
		t.Fatal("admin account not found")
	}
	ingestRaw, _, err := s.Tokens.Create("router-1", auth.TokenKindIngest, "router-1", adminUser, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}

	mintResp := postJSON(t, admin, ts.URL+"/api/droplist/key", nil)
	var minted droplistKeyCreateResponse
	if err := json.NewDecoder(mintResp.Body).Decode(&minted); err != nil {
		t.Fatal(err)
	}
	mintResp.Body.Close()

	revokeResp := deleteNoBody(t, admin, ts.URL+"/api/droplist/key")
	defer revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", revokeResp.StatusCode)
	}

	pullResp := bearerGet(t, ts.URL+"/api/droplist.rsc", minted.Key)
	defer pullResp.Body.Close()
	if pullResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("pull after revoke: status = %d, want 401", pullResp.StatusCode)
	}

	ingestResp := postIngest(t, ts, ingestRaw, validARPPayload)
	defer ingestResp.Body.Close()
	if ingestResp.StatusCode != http.StatusOK {
		t.Errorf("ingest push after revoking the droplist key: status = %d, want 200", ingestResp.StatusCode)
	}

	secondRevoke := deleteNoBody(t, admin, ts.URL+"/api/droplist/key")
	defer secondRevoke.Body.Close()
	if secondRevoke.StatusCode != http.StatusNotFound {
		t.Errorf("revoke with no key present: status = %d, want 404", secondRevoke.StatusCode)
	}
}

// TestDroplistPullKeyMintTwiceRotates pins the rotate-not-accumulate
// contract: minting again invalidates the first raw value and leaves
// exactly one droplist-pull token in the store.
func TestDroplistPullKeyMintTwiceRotates(t *testing.T) {
	s, ts, admin := droplistTestServer(t)

	first := postJSON(t, admin, ts.URL+"/api/droplist/key", nil)
	var firstKey droplistKeyCreateResponse
	if err := json.NewDecoder(first.Body).Decode(&firstKey); err != nil {
		t.Fatal(err)
	}
	first.Body.Close()

	second := postJSON(t, admin, ts.URL+"/api/droplist/key", nil)
	defer second.Body.Close()
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("second mint status = %d, want 201", second.StatusCode)
	}
	var secondKey droplistKeyCreateResponse
	if err := json.NewDecoder(second.Body).Decode(&secondKey); err != nil {
		t.Fatal(err)
	}
	if firstKey.Key == secondKey.Key {
		t.Fatal("minting again produced the same raw key")
	}

	firstPull := bearerGet(t, ts.URL+"/api/droplist.rsc", firstKey.Key)
	defer firstPull.Body.Close()
	if firstPull.StatusCode != http.StatusUnauthorized {
		t.Errorf("the first raw key still works after rotation: status = %d, want 401", firstPull.StatusCode)
	}

	secondPull := bearerGet(t, ts.URL+"/api/droplist.rsc", secondKey.Key)
	defer secondPull.Body.Close()
	if secondPull.StatusCode != http.StatusOK {
		t.Errorf("the second key does not work: status = %d, want 200", secondPull.StatusCode)
	}

	if got := len(s.Tokens.ByKind(auth.TokenKindDroplistPull)); got != 1 {
		t.Errorf("ByKind(droplist-pull) = %d tokens, want exactly 1 after rotating", got)
	}
}

// TestDroplistKeyMintConcurrentRequestsLeaveExactlyOneKey pins the
// concurrency fix a security review asked for: handleDroplistKeyCreate's
// create-then-revoke sequence must be serialized, or two requests
// arriving together can each create a token before either reaches its
// own revoke loop, leaving two live droplist-pull keys where at most one
// is ever meant to exist.
func TestDroplistKeyMintConcurrentRequestsLeaveExactlyOneKey(t *testing.T) {
	s, ts, admin := droplistTestServer(t)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			resp := postJSON(t, admin, ts.URL+"/api/droplist/key", nil)
			resp.Body.Close()
		}()
	}
	wg.Wait()

	if got := len(s.Tokens.ByKind(auth.TokenKindDroplistPull)); got != 1 {
		t.Errorf("ByKind(droplist-pull) = %d tokens after %d concurrent mints, want exactly 1", got, n)
	}
}

// TestDroplistLastUsedAtAppearsAfterPull closes the loop on the design's
// "lastUsedAt is the record" decision: GET /api/droplist's key status
// carries no LastUsedAt until the key has actually been used to pull the
// feed.
func TestDroplistLastUsedAtAppearsAfterPull(t *testing.T) {
	_, ts, admin := droplistTestServer(t)

	mintResp := postJSON(t, admin, ts.URL+"/api/droplist/key", nil)
	var minted droplistKeyCreateResponse
	if err := json.NewDecoder(mintResp.Body).Decode(&minted); err != nil {
		t.Fatal(err)
	}
	mintResp.Body.Close()

	before, err := admin.Get(ts.URL + "/api/droplist")
	if err != nil {
		t.Fatal(err)
	}
	var beforeList droplistListResponse
	if err := json.NewDecoder(before.Body).Decode(&beforeList); err != nil {
		t.Fatal(err)
	}
	before.Body.Close()
	if !beforeList.Key.Present {
		t.Fatal("key.present = false right after minting")
	}
	if !beforeList.Key.LastUsedAt.IsZero() {
		t.Fatalf("LastUsedAt = %v before any pull, want zero", beforeList.Key.LastUsedAt)
	}

	pullResp := bearerGet(t, ts.URL+"/api/droplist.rsc", minted.Key)
	pullResp.Body.Close()

	after, err := admin.Get(ts.URL + "/api/droplist")
	if err != nil {
		t.Fatal(err)
	}
	defer after.Body.Close()
	var afterList droplistListResponse
	if err := json.NewDecoder(after.Body).Decode(&afterList); err != nil {
		t.Fatal(err)
	}
	if afterList.Key.LastUsedAt.IsZero() {
		t.Error("LastUsedAt is still zero after a pull")
	}
}

// TestDroplistListSetupUsesAddressQueryParamOrHost pins #1225's setup
// card content: the four rendered commands come back on GET
// /api/droplist, using the "address" query parameter when given, and
// falling back to the request's own Host otherwise -- both cases with
// the literal key placeholder, since the real key is never shown here.
func TestDroplistListSetupUsesAddressQueryParamOrHost(t *testing.T) {
	_, ts, admin := droplistTestServer(t)

	withParam, err := admin.Get(ts.URL + "/api/droplist?address=mv.example:8443")
	if err != nil {
		t.Fatal(err)
	}
	defer withParam.Body.Close()
	var withParamList droplistListResponse
	if err := json.NewDecoder(withParam.Body).Decode(&withParamList); err != nil {
		t.Fatal(err)
	}
	wantScheduler := droplist.NewSetup("mv.example:8443", "<DROP-LIST-KEY>").Scheduler
	if withParamList.Setup.Scheduler != wantScheduler {
		t.Errorf("Setup.Scheduler with an address param = %q, want %q", withParamList.Setup.Scheduler, wantScheduler)
	}
	if withParamList.Setup.Rule == "" || withParamList.Setup.DisableRule == "" || withParamList.Setup.EmptyList == "" {
		t.Errorf("expected every Setup field to be populated, got %+v", withParamList.Setup)
	}

	withoutParam, err := admin.Get(ts.URL + "/api/droplist")
	if err != nil {
		t.Fatal(err)
	}
	defer withoutParam.Body.Close()
	var withoutParamList droplistListResponse
	if err := json.NewDecoder(withoutParam.Body).Decode(&withoutParamList); err != nil {
		t.Fatal(err)
	}
	wantHostScheduler := droplist.NewSetup(strings.TrimPrefix(ts.URL, "http://"), "<DROP-LIST-KEY>").Scheduler
	if withoutParamList.Setup.Scheduler != wantHostScheduler {
		t.Errorf("Setup.Scheduler with no address param = %q, want %q (from r.Host)", withoutParamList.Setup.Scheduler, wantHostScheduler)
	}
}

// TestDroplistListOwnRangesKnownMirrorsStore pins ownRangesKnown against
// the same Store.OwnRangesKnown() TestDroplistAddWarnsWhenOwnRangesUnknown
// and TestDroplistAddNoWarningOnceOwnRangesAreKnown already exercise
// through the warning text -- this is the same fact, surfaced directly
// on the list response for #1225's Settings group.
func TestDroplistListOwnRangesKnownMirrorsStore(t *testing.T) {
	s, ts, admin := droplistTestServer(t)

	before, err := admin.Get(ts.URL + "/api/droplist")
	if err != nil {
		t.Fatal(err)
	}
	var beforeList droplistListResponse
	if err := json.NewDecoder(before.Body).Decode(&beforeList); err != nil {
		t.Fatal(err)
	}
	before.Body.Close()
	if beforeList.OwnRangesKnown {
		t.Error("ownRangesKnown = true before any router has pushed its own addresses")
	}

	adminUser, ok := s.Auth.ByUsername("admin")
	if !ok {
		t.Fatal("admin account not found")
	}
	ingestRaw, _, err := s.Tokens.Create("router-1", auth.TokenKindIngest, "router-1", adminUser, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}
	postIngest(t, ts, ingestRaw,
		`{"kind":"ip-address","page":1,"pages":1,"records":[{"address":"203.0.114.9/24","network":"203.0.114.0","interface":"ether1","comment":""}]}`).Body.Close()

	after, err := admin.Get(ts.URL + "/api/droplist")
	if err != nil {
		t.Fatal(err)
	}
	defer after.Body.Close()
	var afterList droplistListResponse
	if err := json.NewDecoder(after.Body).Decode(&afterList); err != nil {
		t.Fatal(err)
	}
	if !afterList.OwnRangesKnown {
		t.Error("ownRangesKnown = false after a router has pushed its own addresses")
	}
}

// TestDroplistListRoutersReportsHeldAgainstPushedSnapshot is the drift
// number end to end: a drop-list entry, a router that has pushed its
// own address-list snapshot containing that same range (as the bare /32
// form RouterOS actually renders), and one that has pushed nothing --
// the first must show up with held=1, the second must not appear at
// all.
func TestDroplistListRoutersReportsHeldAgainstPushedSnapshot(t *testing.T) {
	s, ts, admin := droplistTestServer(t)

	createResp := postJSON(t, admin, ts.URL+"/api/droplist", droplistCreateRequest{CIDR: "203.0.114.5/32", Reason: "scanning"})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("create status = %d, want 201, body = %s", createResp.StatusCode, body)
	}

	adminUser, ok := s.Auth.ByUsername("admin")
	if !ok {
		t.Fatal("admin account not found")
	}
	ingestRaw, _, err := s.Tokens.Create("router-1", auth.TokenKindIngest, "router-1", adminUser, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}
	pushResp := postIngest(t, ts, ingestRaw,
		`{"kind":"address-list","page":1,"pages":1,"records":[{"list":"`+droplist.ListName+`","address":"203.0.114.5","comment":"mv: scanning","dynamic":false}]}`)
	defer pushResp.Body.Close()
	if pushResp.StatusCode != http.StatusOK {
		t.Fatalf("address-list ingest push status = %d, want 200", pushResp.StatusCode)
	}

	listResp, err := admin.Get(ts.URL + "/api/droplist")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	var list droplistListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Routers) != 1 {
		t.Fatalf("routers = %+v, want exactly one device (only router-1 ever pushed an address-list snapshot)", list.Routers)
	}
	got := list.Routers[0]
	if got.Device != "router-1" || got.Held != 1 || got.Total != 1 {
		t.Errorf("routers[0] = %+v, want device=router-1 held=1 total=1", got)
	}
	if got.ConfirmedAt.IsZero() {
		t.Error("confirmedAt is zero despite a pushed snapshot")
	}
}

// TestDroplistKeyCreateAcceptsOptionalAddressAndFillsScheduler covers
// #1225's addition to the mint response: an explicit address in the
// request body lands in the returned Scheduler line with the real key
// filled in, and a bodyless mint (every call before #1225 made) still
// works, falling back to the request's own Host.
func TestDroplistKeyCreateAcceptsOptionalAddressAndFillsScheduler(t *testing.T) {
	_, ts, admin := droplistTestServer(t)

	withAddress := postJSON(t, admin, ts.URL+"/api/droplist/key", map[string]string{"address": "mv.example:8443"})
	defer withAddress.Body.Close()
	if withAddress.StatusCode != http.StatusCreated {
		t.Fatalf("mint with address status = %d, want 201", withAddress.StatusCode)
	}
	var minted droplistKeyCreateResponse
	if err := json.NewDecoder(withAddress.Body).Decode(&minted); err != nil {
		t.Fatal(err)
	}
	wantScheduler := droplist.NewSetup("mv.example:8443", minted.Key).Scheduler
	if minted.Scheduler != wantScheduler {
		t.Errorf("Scheduler = %q, want %q", minted.Scheduler, wantScheduler)
	}
	if minted.Scheduler == "" || !strings.Contains(minted.Scheduler, minted.Key) {
		t.Errorf("Scheduler does not carry the real minted key: %q", minted.Scheduler)
	}

	// A genuinely empty body (no request payload at all, as opposed to
	// postJSON's JSON "null") must still work -- decodeJSONBody's
	// io.EOF is not a request error.
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/droplist/key", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	emptyBodyResp, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer emptyBodyResp.Body.Close()
	if emptyBodyResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(emptyBodyResp.Body)
		t.Fatalf("mint with no body at all: status = %d, want 201, body = %s", emptyBodyResp.StatusCode, body)
	}
	var mintedNoBody droplistKeyCreateResponse
	if err := json.NewDecoder(emptyBodyResp.Body).Decode(&mintedNoBody); err != nil {
		t.Fatal(err)
	}
	if mintedNoBody.Scheduler == "" {
		t.Error("expected a scheduler line even with no request body, falling back to r.Host")
	}
}

// A supplied setup address is used only when it looks like a host[:port]
// (stage-3 security review): the rendered commands are pasted into a
// RouterOS terminal, and a line break in the address would smuggle a
// second command into the paste. Anything else falls back to the Host
// header.
func TestDroplistAddressOrRefusesAnythingButAHostPort(t *testing.T) {
	cases := map[string]string{
		"":                             "fallback:1",
		"mv.example":                   "mv.example",
		"mv.example:8443":              "mv.example:8443",
		"192.0.2.10:19803":             "192.0.2.10:19803",
		"[2001:db8::1]:8443":           "[2001:db8::1]:8443",
		"mv.example\n/user add name=p": "fallback:1",
		"mv.example/api":               "fallback:1",
		"mv example":                   "fallback:1",
		"mv.example\"":                 "fallback:1",
		"mv.example;":                  "fallback:1",
	}
	for in, want := range cases {
		if got := droplistAddressOr(in, "fallback:1"); got != want {
			t.Errorf("droplistAddressOr(%q) = %q, want %q", in, got, want)
		}
	}
}
