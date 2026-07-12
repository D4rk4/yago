package adminauth

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestCreateAPIKeyReturnsUsableKey(t *testing.T) {
	handler := mountAuth(t, testService(t))
	rec := doRequest(
		handler,
		http.MethodPost,
		PathAPIKeys,
		`{"label":"ci","scopes":["admin:read"]}`,
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var body createAPIKeyResponse
	if err := decodeBody(rec, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if id, _, ok := parseAPIKey(body.Key); !ok || id != body.ID {
		t.Fatalf("response key %q does not parse to id %q", body.Key, body.ID)
	}
	if len(body.Scopes) != 1 || body.Scopes[0] != ScopeAdminRead {
		t.Fatalf("scopes = %v", body.Scopes)
	}
}

func TestCreateAPIKeyRejectsInvalidBody(t *testing.T) {
	handler := mountAuth(t, testService(t))
	rec := doRequest(handler, http.MethodPost, PathAPIKeys, `{`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateAPIKeyRejectsEmptyScopes(t *testing.T) {
	handler := mountAuth(t, testService(t))
	rec := doRequest(handler, http.MethodPost, PathAPIKeys, `{"scopes":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateAPIKeyRejectsUnknownScope(t *testing.T) {
	handler := mountAuth(t, testService(t))
	rec := doRequest(handler, http.MethodPost, PathAPIKeys, `{"scopes":["storage:wipe"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateAPIKeySurfacesStoreError(t *testing.T) {
	service, engine := scriptedService(t)
	engine.putErr = errors.New("disk full")
	rec := doRequest(
		mountAuth(t, service),
		http.MethodPost,
		PathAPIKeys,
		`{"scopes":["admin:read"]}`,
	)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestAPIKeysRejectsUnsupportedMethod(t *testing.T) {
	handler := mountAuth(t, testService(t))
	rec := doRequest(handler, http.MethodPut, PathAPIKeys, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestListAPIKeysReturnsEmpty(t *testing.T) {
	handler := mountAuth(t, testService(t))
	rec := doRequest(handler, http.MethodGet, PathAPIKeys, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body listAPIKeysResponse
	if err := decodeBody(rec, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Keys) != 0 {
		t.Fatalf("keys = %v, want empty", body.Keys)
	}
}

func TestListAPIKeysOmitsSecretAndTracksUse(t *testing.T) {
	service := testService(t)
	handler := mountAuth(t, service)
	created := createKey(t, service, ScopeAdminRead)

	before := listKeys(t, handler)
	if len(before) != 1 || before[0].ID != created.ID || before[0].LastUsedAt != nil {
		t.Fatalf("before use = %#v", before)
	}

	if outcome := service.APIKeyAuthorizer().Authorize(
		context.Background(), created.Key, ScopeAdminRead,
	); outcome != APIKeyAuthorized {
		t.Fatalf("authorize = %v", outcome)
	}
	after := listKeys(t, handler)
	if len(after) != 1 || after[0].LastUsedAt == nil {
		t.Fatalf("after use = %#v", after)
	}
}

func TestListAPIKeysSurfacesStoreError(t *testing.T) {
	service, engine := scriptedService(t)
	created := createKey(t, service, ScopeAdminRead)
	engine.buckets[adminAPIKeysBucket][created.ID] = []byte("{corrupt")
	rec := doRequest(mountAuth(t, service), http.MethodGet, PathAPIKeys, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestRevokeAPIKeyRemovesKey(t *testing.T) {
	service := testService(t)
	handler := mountAuth(t, service)
	created := createKey(t, service, ScopeAdminRead)
	rec := doRequest(handler, http.MethodDelete, PathAPIKeys+"/"+created.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if len(listKeys(t, handler)) != 0 {
		t.Fatal("key should be gone after revoke")
	}
}

func TestRevokeAPIKeyReportsMissing(t *testing.T) {
	handler := mountAuth(t, testService(t))
	rec := doRequest(handler, http.MethodDelete, PathAPIKeys+"/missing", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRevokeAPIKeySurfacesStoreError(t *testing.T) {
	service, engine := scriptedService(t)
	created := createKey(t, service, ScopeAdminRead)
	engine.deleteErr = errors.New("locked")
	rec := doRequest(mountAuth(t, service), http.MethodDelete, PathAPIKeys+"/"+created.ID, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func listKeys(t *testing.T, handler http.Handler) []apiKeyView {
	t.Helper()
	rec := doRequest(handler, http.MethodGet, PathAPIKeys, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var body listAPIKeysResponse
	if err := decodeBody(rec, &body); err != nil {
		t.Fatalf("decode list: %v", err)
	}

	return body.Keys
}
