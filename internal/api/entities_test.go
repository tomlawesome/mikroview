package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tomlawesome/mikroview/internal/entities"
)

// deleteJSON mirrors postJSON (auth_test.go) but for DELETE, which
// handleEntitiesDelete reads a JSON body from -- the real frontend sends
// the same CSRF header on every mutating request regardless of method.
func deleteJSON(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodDelete, url, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// registerAdmin registers the first (always-admin) account against s and
// returns a client whose cookie jar carries that session -- the shared
// setup every admin-gated entities test below needs, mirroring
// TestAdminCanCreateAdditionalUsers' own setup in auth_test.go.
func registerAdmin(t *testing.T, ts *httptest.Server) *http.Client {
	t.Helper()
	client := &http.Client{Jar: mustCookieJar(t)}
	resp := postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("registering the admin account failed: %d", resp.StatusCode)
	}
	return client
}

// TestEntitiesRequireAdminWhileAuthDisabled proves entities management
// follows callerIsAdmin's strict rule, not callerIsAdminOrOpen's --
// unlike detector settings, GET/POST/DELETE /api/entities stay forbidden
// even in the fully-open "auth disabled" state, since there's no admin
// concept once auth itself has been opted out of (same as
// POST /api/auth/users, which this package's admin check is shared with).
func TestEntitiesRequireAdminWhileAuthDisabled(t *testing.T) {
	s, _ := newTestServer(t) // Auth defaults to disabled -- see its own doc comment
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/entities")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected GET /api/entities to require an admin even with auth disabled, got %d", resp.StatusCode)
	}
}

func TestAdminCanListCreateAndDeleteEntities(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := registerAdmin(t, ts)

	// Empty to start.
	listResp, err := client.Get(ts.URL + "/api/entities")
	if err != nil {
		t.Fatal(err)
	}
	var listBody struct {
		Entities []entities.Entity `json:"entities"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listBody); err != nil {
		t.Fatal(err)
	}
	listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 listing entities, got %d", listResp.StatusCode)
	}
	if len(listBody.Entities) != 0 {
		t.Fatalf("expected an empty entities list initially, got %+v", listBody.Entities)
	}

	// Create.
	createResp := postJSON(t, client, ts.URL+"/api/entities", entityRequest{
		Type: entities.TypeHost, Key: "192.168.1.50", Label: "mail relay", Tags: []string{"trusted-mail-sender"},
	})
	var created entities.Entity
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 creating an entity, got %d", createResp.StatusCode)
	}
	if created.Type != entities.TypeHost || created.Key != "192.168.1.50" || created.Label != "mail relay" {
		t.Errorf("unexpected created entity: %+v", created)
	}

	// List reflects the new entity.
	listResp2, err := client.Get(ts.URL + "/api/entities")
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(listResp2.Body).Decode(&listBody)
	listResp2.Body.Close()
	if len(listBody.Entities) != 1 {
		t.Fatalf("expected 1 entity after creation, got %d: %+v", len(listBody.Entities), listBody.Entities)
	}

	// Delete.
	delResp := deleteJSON(t, client, ts.URL+"/api/entities", entityRequest{Type: entities.TypeHost, Key: "192.168.1.50"})
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 deleting an entity, got %d", delResp.StatusCode)
	}

	listResp3, err := client.Get(ts.URL + "/api/entities")
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(listResp3.Body).Decode(&listBody)
	listResp3.Body.Close()
	if len(listBody.Entities) != 0 {
		t.Errorf("expected the entity list to be empty again after deletion, got %+v", listBody.Entities)
	}
}

// TestPostEntitiesUpsertsInPlace proves a second POST for the same
// (type, key) replaces rather than duplicates, mirroring
// entities.Store's own TestUpsertReplacesInPlace at the HTTP layer.
func TestPostEntitiesUpsertsInPlace(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, ts)

	postJSON(t, client, ts.URL+"/api/entities", entityRequest{Type: entities.TypeRule, Key: "r13", Label: "first"}).Body.Close()
	postJSON(t, client, ts.URL+"/api/entities", entityRequest{Type: entities.TypeRule, Key: "r13", Label: "second"}).Body.Close()

	resp, err := client.Get(ts.URL + "/api/entities")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Entities []entities.Entity `json:"entities"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Entities) != 1 {
		t.Fatalf("expected upserting the same (type, key) twice to still be one entity, got %d", len(body.Entities))
	}
	if body.Entities[0].Label != "second" {
		t.Errorf("expected the latest label to win, got %q", body.Entities[0].Label)
	}
}

func TestPostEntitiesRejectsMissingKey(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, ts)

	resp := postJSON(t, client, ts.URL+"/api/entities", entityRequest{Type: entities.TypeHost, Key: ""})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for a missing key, got %d", resp.StatusCode)
	}
}

func TestDeleteEntitiesUnknownReturnsNotFound(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, ts)

	resp := deleteJSON(t, client, ts.URL+"/api/entities", entityRequest{Type: entities.TypeHost, Key: "nonexistent"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 deleting an unknown entity, got %d", resp.StatusCode)
	}
}

func TestNonAdminCannotManageEntities(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := registerAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

	getResp, err := viewerClient.Get(ts.URL + "/api/entities")
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected a non-admin GET /api/entities to be forbidden, got %d", getResp.StatusCode)
	}

	postResp := postJSON(t, viewerClient, ts.URL+"/api/entities", entityRequest{Type: entities.TypeHost, Key: "1.2.3.4"})
	defer postResp.Body.Close()
	if postResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected a non-admin POST /api/entities to be forbidden, got %d", postResp.StatusCode)
	}

	delResp := deleteJSON(t, viewerClient, ts.URL+"/api/entities", entityRequest{Type: entities.TypeHost, Key: "1.2.3.4"})
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected a non-admin DELETE /api/entities to be forbidden, got %d", delResp.StatusCode)
	}
}
