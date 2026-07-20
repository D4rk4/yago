package adminauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
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
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	var body createAPIKeyResponse
	if err := decodeBody(rec, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if id, _, ok := parseAPIKey(body.Key); !ok || id != body.ID {
		t.Fatalf("response credential does not parse to public id %q", body.ID)
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
	if rec.Body.String() != "{\"keys\":[]}\n" {
		t.Fatalf("compatibility body = %q", rec.Body.String())
	}
}

func TestListAPIKeysPaginatesLegacyStoreWithoutDuplicates(t *testing.T) {
	service := testService(t)
	want := storeLegacyAPIKeyRecords(t, service.apiKeys, maximumAPIKeys+44)
	handler := mountAuth(t, service)

	first := requestAPIKeyPage(t, handler, PathAPIKeys)
	if len(first.Keys) != maximumAPIKeys || !first.Truncated || first.NextCursor == "" ||
		first.Total == nil || *first.Total != len(want) {
		t.Fatalf("first legacy page = %+v", first)
	}
	seen := make(map[string]struct{}, len(want))
	for _, key := range first.Keys {
		seen[key.ID] = struct{}{}
	}
	second := requestAPIKeyPage(
		t,
		handler,
		PathAPIKeys+"?cursor="+url.QueryEscape(first.NextCursor),
	)
	if len(second.Keys) != len(want)-maximumAPIKeys || second.Truncated ||
		second.NextCursor != "" || second.Total == nil || *second.Total != len(want) {
		t.Fatalf("second legacy page = %+v", second)
	}
	for _, key := range second.Keys {
		if _, duplicate := seen[key.ID]; duplicate {
			t.Fatalf("duplicate key %q across pages", key.ID)
		}
		seen[key.ID] = struct{}{}
	}
	if len(seen) != len(want) {
		t.Fatalf("listed %d unique keys, want %d", len(seen), len(want))
	}
}

func TestListAPIKeysTwentyRowBoundaryUsesContinuation(t *testing.T) {
	service := testService(t)
	storeLegacyAPIKeyRecords(t, service.apiKeys, 21)
	handler := mountAuth(t, service)

	first := requestAPIKeyPage(t, handler, PathAPIKeys+"?limit=20")
	if len(first.Keys) != 20 || !first.Truncated || first.NextCursor == "" ||
		first.Total == nil || *first.Total != 21 {
		t.Fatalf("first boundary page = %+v", first)
	}
	second := requestAPIKeyPage(
		t,
		handler,
		PathAPIKeys+"?limit=20&cursor="+url.QueryEscape(first.NextCursor),
	)
	if len(second.Keys) != 1 || second.Truncated || second.NextCursor != "" ||
		second.Total == nil || *second.Total != 21 {
		t.Fatalf("second boundary page = %+v", second)
	}
	for _, firstKey := range first.Keys {
		if firstKey.ID == second.Keys[0].ID {
			t.Fatalf("boundary key %q was duplicated", firstKey.ID)
		}
	}
}

func TestListAPIKeysValidatesCursorAndLimit(t *testing.T) {
	handler := mountAuth(t, testService(t))
	validCursor := deterministicAPIKeyID(1)
	paths := []string{
		PathAPIKeys + "?cursor=bad",
		PathAPIKeys + "?cursor=" + validCursor + "&cursor=" + validCursor,
		PathAPIKeys + "?limit=",
		PathAPIKeys + "?limit=zero",
		PathAPIKeys + "?limit=0",
		PathAPIKeys + "?limit=257",
		PathAPIKeys + "?limit=1&limit=2",
	}
	for _, path := range paths {
		rec := doRequest(handler, http.MethodGet, path, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", path, rec.Code)
		}
	}
}

func TestListAPIKeysExplicitPageReportsTotal(t *testing.T) {
	handler := mountAuth(t, testService(t))
	response := requestAPIKeyPage(t, handler, PathAPIKeys+"?limit=20&cursor=")
	if response.Total == nil || *response.Total != 0 || response.Truncated {
		t.Fatalf("explicit empty page = %+v", response)
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

func requestAPIKeyPage(
	t *testing.T,
	handler http.Handler,
	path string,
) listAPIKeysResponse {
	t.Helper()
	rec := doRequest(handler, http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	var body listAPIKeysResponse
	if err := decodeBody(rec, &body); err != nil {
		t.Fatalf("decode list page: %v", err)
	}

	return body
}
