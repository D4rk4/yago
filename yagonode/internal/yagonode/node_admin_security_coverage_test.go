package yagonode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func closedSecuritySource(t *testing.T) *securitySource {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	service, err := adminauth.New(storage, adminauth.Config{})
	if err != nil {
		t.Fatalf("adminauth.New: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}

	return newSecuritySource(service)
}

func TestSecuritySourceSurfacesBackendErrors(t *testing.T) {
	source := closedSecuritySource(t)
	ctx := context.Background()

	if view := source.Security(ctx); view.Error == "" {
		t.Fatal("Security should report an error when the store is unavailable")
	}
	if _, err := source.MintAPIKey(ctx, adminui.APIKeyMint{
		Label:  "bot",
		Scopes: []string{"search:read"},
	}); err == nil {
		t.Fatal("MintAPIKey should surface the backend error")
	}
	if _, err := source.RevokeAPIKey(ctx, adminui.APIKeyRevoke{ID: "some-id"}); err == nil {
		t.Fatal("RevokeAPIKey should surface the backend error")
	}
}

// TestSecuritySourceChangePasswordThroughGuard drives ChangePassword behind the
// admin guard so the request context carries a signed-in principal, exercising
// the mismatch and success branches that a bare context cannot reach.
func TestSecuritySourceChangePasswordThroughGuard(t *testing.T) {
	config := nodeConfig{Admin: adminConfig{Username: "admin", Password: "pw"}}
	service, err := provisionAdminAuth(context.Background(), config, openTestVault(t), nil)
	if err != nil {
		t.Fatalf("provisionAdminAuth: %v", err)
	}
	source := newSecuritySource(service)

	var change adminui.PasswordChange
	var gotResult adminui.PasswordChangeResult
	var gotErr error
	const capturePath = "/api/admin/v1/console/change-password-probe"
	mux := http.NewServeMux()
	mux.HandleFunc(capturePath, func(w http.ResponseWriter, r *http.Request) {
		gotResult, gotErr = source.ChangePassword(r.Context(), change)
		w.WriteHeader(http.StatusOK)
	})
	handler := guardAdminSurface(service, mux)

	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, adminauth.PathLogin,
		strings.NewReader(`{"username":"admin","password":"pw"}`),
	)
	loginReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200", loginRec.Code)
	}
	cookies := loginRec.Result().Cookies()

	call := func(ch adminui.PasswordChange) (adminui.PasswordChangeResult, error) {
		change = ch
		req := httptest.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			capturePath,
			nil,
		)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("probe = %d, want 200", rec.Code)
		}

		return gotResult, gotErr
	}

	if res, err := call(adminui.PasswordChange{
		Current: "wrong", New: "np", Confirm: "np",
	}); err != nil || res.OK {
		t.Fatalf("wrong current: res=%+v err=%v", res, err)
	}
	if res, err := call(adminui.PasswordChange{
		Current: "pw", New: "np", Confirm: "np",
	}); err != nil || !res.OK {
		t.Fatalf("correct current: res=%+v err=%v", res, err)
	}
}
