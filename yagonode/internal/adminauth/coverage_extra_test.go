package adminauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// injectRawAdmin stores an admin record with an arbitrary (possibly malformed)
// password hash so tests can drive the verify error path.
func injectRawAdmin(t *testing.T, engine *scriptedEngine, username, hash string) {
	t.Helper()
	data, err := json.Marshal(adminRecord{Username: username, PasswordHash: hash})
	if err != nil {
		t.Fatalf("marshal admin record: %v", err)
	}
	engine.buckets[adminCredentialsBucket][string(adminKey)] = data
}

func TestCSRFTokenFromContext(t *testing.T) {
	ctx := contextWithSession(context.Background(), sessionRecord{
		Username:  "op",
		CSRFToken: "tok",
	})
	if tok, ok := CSRFTokenFromContext(ctx); !ok || tok != "tok" {
		t.Fatalf("with token = %q,%v", tok, ok)
	}
	if _, ok := CSRFTokenFromContext(context.Background()); ok {
		t.Fatal("missing session reported a token")
	}
	empty := contextWithSession(context.Background(), sessionRecord{CSRFToken: ""})
	if _, ok := CSRFTokenFromContext(empty); ok {
		t.Fatal("empty token reported present")
	}
}

func TestPrincipalFromContext(t *testing.T) {
	ctx := contextWithSession(context.Background(), sessionRecord{Username: "op"})
	if name, ok := PrincipalFromContext(ctx); !ok || name != "op" {
		t.Fatalf("with principal = %q,%v", name, ok)
	}
	if _, ok := PrincipalFromContext(context.Background()); ok {
		t.Fatal("missing session reported a principal")
	}
	blank := contextWithSession(context.Background(), sessionRecord{Username: ""})
	if _, ok := PrincipalFromContext(blank); ok {
		t.Fatal("empty principal reported present")
	}
}

func TestAuthMessageHelpers(t *testing.T) {
	if loginErrorMessage("throttled") == "" {
		t.Fatal("throttled login message empty")
	}
	if loginErrorMessage("server") == "" {
		t.Fatal("server login message empty")
	}
	if loginErrorMessage("bogus") != "" {
		t.Fatal("unknown login code produced a message")
	}
	if loginNoticeMessage("created") == "" {
		t.Fatal("created notice empty")
	}
	if loginNoticeMessage("out") != "" {
		t.Fatal("logout notice remained visible")
	}
	if loginNoticeMessage("bogus") != "" {
		t.Fatal("unknown notice produced a message")
	}
	if setupErrorMessage("missing") == "" {
		t.Fatal("missing setup message empty")
	}
	if setupErrorMessage("server") == "" {
		t.Fatal("server setup message empty")
	}
	if setupErrorMessage("bogus") != "" {
		t.Fatal("unknown setup code produced a message")
	}
}

func TestRenderAuthPageTemplateError(t *testing.T) {
	service, _ := scriptedService(t)
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, PathLoginPage, nil,
	)
	rec := httptest.NewRecorder()
	service.renderAuthPage(rec, req, "no-such-template", authPageData{})
	if ct := rec.Header().Get("Content-Type"); ct != authHTMLType {
		t.Fatalf("content-type = %q, want %q", ct, authHTMLType)
	}
}

func TestLoginFormServerErrorOnVerifyFailure(t *testing.T) {
	service, engine := scriptedService(t)
	injectRawAdmin(t, engine, "admin", "not-a-valid-hash")
	rec := postForm(htmlSurface(t, service), PathLoginPage, url.Values{
		"username": {"admin"}, "password": {"whatever"},
	})
	if loc := rec.Header().Get("Location"); loc != PathLoginPage+"?error=server" {
		t.Fatalf("location = %q, want server", loc)
	}
}

func TestLoginFormServerErrorOnSessionCreate(t *testing.T) {
	service, engine := scriptedService(t)
	injectAdmin(t, engine, "admin", "correct-horse")
	engine.putErr = errors.New("boom")
	rec := postForm(htmlSurface(t, service), PathLoginPage, url.Values{
		"username": {"admin"}, "password": {"correct-horse"},
	})
	if loc := rec.Header().Get("Location"); loc != PathLoginPage+"?error=server" {
		t.Fatalf("location = %q, want server", loc)
	}
}

func TestSetupFormRedirectsWhenAdminAlreadyExists(t *testing.T) {
	service, engine := scriptedService(t)
	injectAdmin(t, engine, "admin", "pw")
	rec := postForm(htmlSurface(t, service), PathSetupPage, url.Values{
		"username": {"admin2"}, "password": {"correct-horse"},
	})
	if loc := rec.Header().Get("Location"); loc != PathLoginPage {
		t.Fatalf("location = %q, want login", loc)
	}
}

func TestSetupFormServerErrorOnCreate(t *testing.T) {
	service, engine := scriptedService(t)
	engine.putErr = errors.New("boom")
	rec := postForm(htmlSurface(t, service), PathSetupPage, url.Values{
		"username": {"admin"}, "password": {"correct-horse"},
	})
	if loc := rec.Header().Get("Location"); loc != PathSetupPage+"?error=server" {
		t.Fatalf("location = %q, want server", loc)
	}
}

func TestAPIKeyAuthorizerRemainsAvailableOnLastUsedStoreError(t *testing.T) {
	service, engine := scriptedService(t)
	created := createKey(t, service, ScopeSearchRead)
	engine.putErr = errors.New("boom")
	got := service.APIKeyAuthorizer().Authorize(
		context.Background(),
		created.Key,
		ScopeSearchRead,
	)
	if got != APIKeyAuthorized {
		t.Fatalf("outcome = %v, want authorized", got)
	}
}

func TestListAPIKeysErrorOnMalformedRecord(t *testing.T) {
	service, engine := scriptedService(t)
	engine.buckets[adminAPIKeysBucket]["bad"] = []byte("not-json")
	if _, err := service.ListAPIKeys(context.Background()); err == nil {
		t.Fatal("expected a list error for a malformed record")
	}
}

func TestCreateAPIKeyErrorOnStoreFailure(t *testing.T) {
	service, engine := scriptedService(t)
	engine.putErr = errors.New("boom")
	_, err := service.CreateAPIKey(context.Background(), "ci", []string{"search:read"})
	if err == nil {
		t.Fatal("expected a create error when the store fails")
	}
}

func TestRevokeAPIKeyErrorOnStoreFailure(t *testing.T) {
	service, engine := scriptedService(t)
	created := createKey(t, service, ScopeSearchRead)
	engine.deleteErr = errors.New("boom")
	if _, err := service.RevokeAPIKey(context.Background(), created.ID); err == nil {
		t.Fatal("expected a revoke error when the store fails")
	}
}

func TestChangePasswordVerifyError(t *testing.T) {
	service, engine := scriptedService(t)
	injectRawAdmin(t, engine, "admin", "not-a-valid-hash")
	err := service.ChangePassword(context.Background(), "admin", "cur", "new-pw")
	if err == nil {
		t.Fatal("expected a verify error for a malformed stored hash")
	}
}

func TestChangePasswordSetAdminError(t *testing.T) {
	service, engine := scriptedService(t)
	injectAdmin(t, engine, "admin", "correct-horse")
	engine.putErr = errors.New("boom")
	err := service.ChangePassword(context.Background(), "admin", "correct-horse", "new-pw")
	if err == nil {
		t.Fatal("expected a setAdmin error when the store fails")
	}
}
