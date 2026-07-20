package yagonode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestBootSharesScopedKeyAcrossOperationsAndPublicServers(t *testing.T) {
	restoreMainSeams(t)
	config := testConfig(t)
	config.Admin = adminConfig{Username: "operator", Password: "test-password"}
	storage := openTestVault(t)
	assembleRuntimeNode = func(
		_ context.Context,
		_ nodeConfig,
		_ *vault.Vault,
		_ *http.Client,
		telemetry nodeTelemetry,
	) (node, error) {
		return node{
			announcer:     fakeAnnouncer{},
			sweeper:       &scriptedSweeper{},
			searchExplain: newSearchExplainEndpoint(nil, nil, nil, nil, nil),
			publicMux: scopedTavilyHandlerWithAccess(tavilyapi.SearchAccessPolicy{
				Authorizer: telemetry.searchAuthorizer,
			}),
		}, nil
	}
	serveRuntimeNode = func(
		_ context.Context,
		_ node,
		_ *metrics.EvictionMetrics,
		servers ...namedServer,
	) error {
		operations := runtimeServerHandler(t, servers, "ops")
		public := runtimeServerHandler(t, servers, "public search")
		session := signInGuardedAdmin(t, operations, "operator", "test-password")
		credential := mintGuardedSearchKey(t, operations, session)
		assertScopedTavilyStatus(t, public, scopedTavilyProbe{
			Path:       tavilyapi.PathSearch,
			Body:       scopedBasicSearchBody,
			Credential: credential.Key,
			Status:     http.StatusOK,
		})

		return nil
	}
	if err := bootNode(t.Context(), config, storage); err != nil {
		t.Fatalf("boot node: %v", err)
	}
}

func runtimeServerHandler(t *testing.T, servers []namedServer, name string) http.Handler {
	t.Helper()
	for _, server := range servers {
		if server.name == name {
			return server.server.Handler
		}
	}
	t.Fatalf("runtime server %q is missing", name)

	return nil
}

func mintGuardedSearchKey(
	t *testing.T,
	handler http.Handler,
	session guardedAdminSession,
) listenerMintResponse {
	t.Helper()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		adminauth.PathAPIKeys,
		strings.NewReader(`{"label":"runtime wiring","scopes":["search:read"]}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", session.csrfToken)
	request.AddCookie(session.cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
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
