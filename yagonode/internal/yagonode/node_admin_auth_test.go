package yagonode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type failUpdateEngine struct{}

func (failUpdateEngine) Provision(vault.Name) error { return nil }

func (failUpdateEngine) Update(context.Context, func(vault.EngineTxn) error) error {
	return errors.New("update failed")
}

func (failUpdateEngine) View(context.Context, func(vault.EngineTxn) error) error { return nil }

func (failUpdateEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }

func (failUpdateEngine) QuotaBytes() int64 { return 0 }

func (failUpdateEngine) Close() error { return nil }

func adminRequestCode(t *testing.T, handler http.Handler, method, path, body string) int {
	t.Helper()
	req := httptest.NewRequestWithContext(
		context.Background(),
		method,
		path,
		strings.NewReader(body),
	)
	if method == http.MethodPost && (path == adminauth.PathLogin || path == adminauth.PathSetup) {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec.Code
}

func testOpsMux() *http.ServeMux {
	return newOpsMux(metrics.NewHTTPEndpointMetrics().Handler(), nil, nil, nil, nil)
}

func TestGuardAdminSurfaceGatesAndBootstraps(t *testing.T) {
	config := nodeConfig{Admin: adminConfig{Username: "admin", Password: "pw"}}
	service, err := provisionAdminAuth(context.Background(), config, openTestVault(t), nil)
	if err != nil {
		t.Fatalf("provisionAdminAuth: %v", err)
	}
	handler := guardAdminSurface(service, testOpsMux())

	if code := adminRequestCode(t, handler, http.MethodGet, pathHealth, ""); code != http.StatusOK {
		t.Fatalf("%s = %d, want 200 (exempt)", pathHealth, code)
	}
	if code := adminRequestCode(
		t,
		handler,
		http.MethodGet,
		pathMetrics,
		"",
	); code != http.StatusUnauthorized {
		t.Fatalf("%s = %d, want 401 (gated)", pathMetrics, code)
	}
	login := adminRequestCode(
		t,
		handler,
		http.MethodPost,
		adminauth.PathLogin,
		`{"username":"admin","password":"pw"}`,
	)
	if login != http.StatusOK {
		t.Fatalf("login = %d, want 200 (bootstrapped admin)", login)
	}
}

func TestProvisionAdminAuthSurfacesServiceError(t *testing.T) {
	storage := openTestVault(t)
	if _, err := adminauth.New(storage, adminauth.Config{}); err != nil {
		t.Fatalf("pre-register admin auth: %v", err)
	}
	if _, err := provisionAdminAuth(
		context.Background(),
		nodeConfig{},
		storage,
		nil,
	); err == nil {
		t.Fatal("provisionAdminAuth should fail when the service cannot be built")
	}
}

func TestProvisionAdminAuthSurfacesBootstrapError(t *testing.T) {
	storage, err := vault.New(failUpdateEngine{})
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	config := nodeConfig{Admin: adminConfig{Username: "admin", Password: "pw"}}
	if _, err := provisionAdminAuth(
		context.Background(),
		config,
		storage,
		nil,
	); err == nil {
		t.Fatal("provisionAdminAuth should surface a bootstrap error")
	}
}

func TestRunFailsWhenAdminAuthCannotConfigure(t *testing.T) {
	restoreMainSeams(t)
	setValidRunEnv(t)
	openRuntimeVault = func(string, int64) (*vault.Vault, error) {
		storage := openTestVault(t)
		if _, err := adminauth.New(storage, adminauth.Config{}); err != nil {
			t.Fatalf("pre-register admin auth: %v", err)
		}

		return storage, nil
	}
	assembleRuntimeNode = func(
		context.Context,
		nodeConfig,
		*vault.Vault,
		*http.Client,
		nodeTelemetry,
	) (node, error) {
		return node{}, nil
	}
	serveRuntimeNode = func(
		context.Context,
		node,
		*metrics.EvictionMetrics,
		...namedServer,
	) error {
		t.Fatal("serve must not be reached when admin auth configuration fails")

		return nil
	}

	if err := run(); err == nil {
		t.Fatal("run should fail when admin auth cannot be configured")
	}
}
