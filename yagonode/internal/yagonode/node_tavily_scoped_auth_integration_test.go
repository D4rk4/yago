package yagonode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
)

type scopedAuthDocuments map[string]documentstore.Document

func (d scopedAuthDocuments) Document(
	_ context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	document, found := d[normalizedURL]

	return document, found, nil
}

func (d scopedAuthDocuments) Count(context.Context) (int, error) {
	return len(d), nil
}

type listenerMintResponse struct {
	ID  string
	Key string
}

type scopedTavilyCredentials struct {
	ReadRaw  listenerMintResponse
	ReadOnly listenerMintResponse
	Revoked  listenerMintResponse
}

type scopedTavilyProbe struct {
	Path       string
	Body       string
	Credential string
	Status     int
}

const (
	scopedBasicSearchBody = "{\"query\":\"go\"}"
	scopedRawSearchBody   = "{\"query\":\"go\",\"include_raw_content\":true}"
	scopedExtractBody     = "{\"urls\":\"https://example.test/\"}"
)

func TestScopedTavilyKeySpansOperationsPublicAndVaultReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "storage")
	firstVault, err := openRuntimeVault(path, 0)
	if err != nil {
		t.Fatalf("open first runtime vault: %v", err)
	}
	firstService, err := adminauth.New(firstVault, adminauth.Config{})
	if err != nil {
		t.Fatalf("open first auth service: %v", err)
	}
	credentials := mintScopedTavilyCredentials(t, firstService)
	firstPublic := scopedTavilyHandler(firstService)
	assertInitialScopedTavilyContract(t, firstPublic, credentials)

	if err := firstVault.Close(); err != nil {
		t.Fatalf("close first runtime vault: %v", err)
	}
	assertScopedTavilyStatus(t, firstPublic, scopedTavilyProbe{
		Path:       tavilyapi.PathSearch,
		Body:       scopedBasicSearchBody,
		Credential: credentials.ReadRaw.Key,
		Status:     http.StatusServiceUnavailable,
	})

	reopenedVault, err := openRuntimeVault(path, 0)
	if err != nil {
		t.Fatalf("reopen runtime vault: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := reopenedVault.Close(); closeErr != nil {
			t.Errorf("close reopened runtime vault: %v", closeErr)
		}
	})
	reopenedService, err := adminauth.New(reopenedVault, adminauth.Config{})
	if err != nil {
		t.Fatalf("open reopened auth service: %v", err)
	}
	reopenedPublic := scopedTavilyHandler(reopenedService)
	assertReopenedScopedTavilyContract(t, reopenedPublic, credentials)
}

func mintScopedTavilyCredentials(
	t *testing.T,
	service *adminauth.Service,
) scopedTavilyCredentials {
	t.Helper()
	operations := http.NewServeMux()
	adminauth.Mount(operations, service)
	credentials := scopedTavilyCredentials{
		ReadRaw:  mintListenerKey(t, operations, "search:read", "search:raw"),
		ReadOnly: mintListenerKey(t, operations, "search:read"),
		Revoked:  mintListenerKey(t, operations, "search:read"),
	}
	existed, err := service.RevokeAPIKey(t.Context(), credentials.Revoked.ID)
	if err != nil || !existed {
		t.Fatalf("revoke disposable key: existed=%t err=%v", existed, err)
	}

	return credentials
}

func assertInitialScopedTavilyContract(
	t *testing.T,
	handler http.Handler,
	credentials scopedTavilyCredentials,
) {
	t.Helper()
	probes := []scopedTavilyProbe{
		{tavilyapi.PathSearch, scopedBasicSearchBody, credentials.ReadRaw.Key, http.StatusOK},
		{tavilyapi.PathSearch, scopedRawSearchBody, credentials.ReadRaw.Key, http.StatusOK},
		{tavilyapi.PathExtract, scopedExtractBody, credentials.ReadRaw.Key, http.StatusOK},
		{tavilyapi.PathSearch, scopedBasicSearchBody, credentials.ReadOnly.Key, http.StatusOK},
		{tavilyapi.PathSearch, scopedRawSearchBody, credentials.ReadOnly.Key, http.StatusForbidden},
		{tavilyapi.PathExtract, scopedExtractBody, credentials.ReadOnly.Key, http.StatusForbidden},
		{
			tavilyapi.PathSearch,
			scopedBasicSearchBody,
			credentials.Revoked.Key,
			http.StatusUnauthorized,
		},
	}
	for _, probe := range probes {
		assertScopedTavilyStatus(t, handler, probe)
	}
}

func assertReopenedScopedTavilyContract(
	t *testing.T,
	handler http.Handler,
	credentials scopedTavilyCredentials,
) {
	t.Helper()
	for range 10 {
		assertScopedTavilyStatus(t, handler, scopedTavilyProbe{
			Path:       tavilyapi.PathSearch,
			Body:       scopedBasicSearchBody,
			Credential: credentials.ReadRaw.Key,
			Status:     http.StatusOK,
		})
	}
	probes := []scopedTavilyProbe{
		{tavilyapi.PathSearch, scopedRawSearchBody, credentials.ReadRaw.Key, http.StatusOK},
		{tavilyapi.PathExtract, scopedExtractBody, credentials.ReadRaw.Key, http.StatusOK},
		{tavilyapi.PathSearch, scopedRawSearchBody, credentials.ReadOnly.Key, http.StatusForbidden},
		{
			tavilyapi.PathSearch,
			scopedBasicSearchBody,
			credentials.Revoked.Key,
			http.StatusUnauthorized,
		},
	}
	for _, probe := range probes {
		assertScopedTavilyStatus(t, handler, probe)
	}
}

func TestScopedTavilyKeyThrottleMapsToTooManyRequests(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open memory vault: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	service, err := adminauth.New(storage, adminauth.Config{
		APIKeyMaxPerWindow: 1,
		APIKeyWindow:       time.Hour,
	})
	if err != nil {
		t.Fatalf("open auth service: %v", err)
	}
	created, err := service.CreateAPIKey(t.Context(), "throttle", []string{"search:read"})
	if err != nil {
		t.Fatalf("mint throttle key: %v", err)
	}
	handler := scopedTavilyHandler(service)
	assertScopedTavilyStatus(t, handler, scopedTavilyProbe{
		Path:       tavilyapi.PathSearch,
		Body:       scopedBasicSearchBody,
		Credential: created.Secret,
		Status:     http.StatusOK,
	})
	response := scopedTavilyRequest(
		t, handler, tavilyapi.PathSearch, scopedBasicSearchBody, created.Secret,
	)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("throttled status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
	if retryAfter := response.Header().Get("Retry-After"); retryAfter != "1" {
		t.Fatalf("Retry-After = %q, want 1", retryAfter)
	}
}

func mintListenerKey(t *testing.T, operations http.Handler, scopes ...string) listenerMintResponse {
	t.Helper()
	requestBody, err := json.Marshal(map[string]any{
		"label":  "listener integration",
		"scopes": scopes,
	})
	if err != nil {
		t.Fatalf("encode mint request: %v", err)
	}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		adminauth.PathAPIKeys,
		strings.NewReader(string(requestBody)),
	)
	response := httptest.NewRecorder()
	operations.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("mint status = %d, want %d", response.Code, http.StatusCreated)
	}
	var minted listenerMintResponse
	if err := json.Unmarshal(response.Body.Bytes(), &minted); err != nil {
		t.Fatalf("decode mint response: %v", err)
	}
	if minted.ID == "" || minted.Key == "" {
		t.Fatal("mint response omitted credential fields")
	}

	return minted
}

func scopedTavilyHandler(service *adminauth.Service) http.Handler {
	return scopedTavilyHandlerWithAccess(tavilyapi.SearchAccessPolicy{
		Authorizer: buildSearchScopeAuthorizer(service),
	})
}

func scopedTavilyHandlerWithAccess(access tavilyapi.SearchAccessPolicy) http.Handler {
	documents := scopedAuthDocuments{
		"https://example.test/": {
			NormalizedURL: "https://example.test/",
			Title:         "Go",
			ExtractedText: "Go search result",
		},
	}
	search := &fakeSearcher{resp: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title:   "Go",
			URL:     "https://example.test/",
			Snippet: "Go search result",
		}},
	}}
	mux := http.NewServeMux()
	tavilyapi.Mount(mux, search, documents, access, nil)
	tavilyapi.MountExtract(mux, documents, access, nil)

	return mux
}

func assertScopedTavilyStatus(
	t *testing.T,
	handler http.Handler,
	probe scopedTavilyProbe,
) {
	t.Helper()
	response := scopedTavilyRequest(
		t,
		handler,
		probe.Path,
		probe.Body,
		probe.Credential,
	)
	if response.Code != probe.Status {
		t.Fatalf("%s status = %d, want %d", probe.Path, response.Code, probe.Status)
	}
	if probe.Status == http.StatusUnauthorized &&
		response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("%s missing bearer challenge", probe.Path)
	}
	if (probe.Status == http.StatusTooManyRequests ||
		probe.Status == http.StatusServiceUnavailable) &&
		response.Header().Get("Retry-After") != "1" {
		t.Fatalf("%s missing retry guidance", probe.Path)
	}
}

func scopedTavilyRequest(
	t *testing.T,
	handler http.Handler,
	path string,
	body string,
	credential string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		path,
		strings.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+credential)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}
