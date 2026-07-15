package yagonode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/siteicon"
)

func TestAdminSiteIconIsAvailableBeforeLogin(t *testing.T) {
	service, err := provisionAdminAuth(
		context.Background(),
		nodeConfig{Admin: adminConfig{Username: "admin", Password: "pw"}},
		openTestVault(t),
		nil,
	)
	if err != nil {
		t.Fatalf("provision admin auth: %v", err)
	}
	handler := guardAdminSurface(service, testOpsMux())

	for _, path := range []string{siteicon.Path, siteicon.LegacyPath} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200 before login", path, recorder.Code)
		}
		if recorder.Header().Get("Content-Type") != "image/svg+xml" {
			t.Fatalf("%s content type = %q", path, recorder.Header().Get("Content-Type"))
		}
		if recorder.Header().Get("Cache-Control") != "public, max-age=86400" {
			t.Fatalf("%s cache policy = %q", path, recorder.Header().Get("Cache-Control"))
		}
	}
}

func TestOpenAdminAssetsUseExactSafeRoutes(t *testing.T) {
	service, err := provisionAdminAuth(
		context.Background(),
		nodeConfig{Admin: adminConfig{Username: "admin", Password: "pw"}},
		openTestVault(t),
		nil,
	)
	if err != nil {
		t.Fatalf("provision admin auth: %v", err)
	}
	handler := guardAdminSurface(service, testOpsMux())
	tests := map[string]struct {
		method string
		path   string
		status int
	}{
		"favicon get":     {http.MethodGet, siteicon.Path, http.StatusOK},
		"favicon post":    {http.MethodPost, siteicon.Path, http.StatusMethodNotAllowed},
		"legacy icon put": {http.MethodPut, siteicon.LegacyPath, http.StatusMethodNotAllowed},
		"favicon suffix":  {http.MethodGet, siteicon.Path + "x", http.StatusUnauthorized},
		"favicon child":   {http.MethodGet, siteicon.Path + "/child", http.StatusUnauthorized},
		"auth css get":    {http.MethodGet, adminauth.PathAuthStylesheet, http.StatusOK},
		"auth css post": {
			http.MethodPost,
			adminauth.PathAuthStylesheet,
			http.StatusMethodNotAllowed,
		},
		"auth css suffix": {
			http.MethodGet,
			adminauth.PathAuthStylesheet + "x",
			http.StatusUnauthorized,
		},
		"auth css child": {
			http.MethodGet,
			adminauth.PathAuthStylesheet + "/child",
			http.StatusUnauthorized,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf(
					"%s %s status = %d, want %d",
					test.method,
					test.path,
					recorder.Code,
					test.status,
				)
			}
		})
	}
}

func TestPublicSearchMountsSiteIcon(t *testing.T) {
	toggles := &runtimeToggles{}
	toggles.SetPortalEnabled(true)
	mux := http.NewServeMux()
	mountNodePublicSearch(mux, publicSearchAssembly{
		storage: nodeStorage{
			postings:     publicSearchPostingIndex{},
			urlDirectory: publicSearchURLDirectory{},
		},
		identity: nodeidentity.Identity{NetworkName: "freeworld"},
		dht:      defaultPublicSearchDHTConfig(),
		client:   http.DefaultClient,
		toggles:  toggles,
	})

	for _, path := range []string{siteicon.Path, siteicon.LegacyPath} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		mux.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, recorder.Code)
		}
	}
}

type guardedAdminSession struct {
	cookie    *http.Cookie
	csrfToken string
}

func guardedSecurityHandler(t *testing.T) http.Handler {
	t.Helper()
	service, err := provisionAdminAuth(
		context.Background(),
		nodeConfig{Admin: adminConfig{Username: "operator", Password: "pw"}},
		openTestVault(t),
		nil,
	)
	if err != nil {
		t.Fatalf("provision admin auth: %v", err)
	}
	mux := testOpsMux()
	mux.Handle(adminui.BasePath, adminui.New(adminui.Options{Security: newSecuritySource(service)}))

	return guardAdminSurface(service, mux)
}

func signInGuardedAdmin(
	t *testing.T,
	handler http.Handler,
	username, password string,
) guardedAdminSession {
	t.Helper()
	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		adminauth.PathLogin,
		strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`),
	)
	loginRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil || session.CSRFToken == "" {
		t.Fatalf("decode login response: token=%q err=%v", session.CSRFToken, err)
	}
	response := login.Result()
	cookies := response.Cookies()
	_ = response.Body.Close()
	if len(cookies) == 0 {
		t.Fatal("login did not return a session cookie")
	}

	return guardedAdminSession{cookie: cookies[0], csrfToken: session.CSRFToken}
}

func guardedAdminLoginStatus(
	t *testing.T,
	handler http.Handler,
	username, password string,
) int {
	t.Helper()
	body := `{"username":"` + username + `","password":"` + password + `"}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		adminauth.PathLogin,
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	return recorder.Code
}

func TestSecurityPasswordUsernameComesFromSignedInSession(t *testing.T) {
	handler := guardedSecurityHandler(t)
	session := signInGuardedAdmin(t, handler, "operator", "pw")
	page := httptest.NewRecorder()
	pageRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/admin/security",
		nil,
	)
	pageRequest.Header.Set("Accept", "text/html")
	pageRequest.AddCookie(session.cookie)
	handler.ServeHTTP(page, pageRequest)
	if page.Code != http.StatusOK {
		t.Fatalf("security page status = %d, body = %s", page.Code, page.Body.String())
	}
	for _, expected := range []string{
		`name="username"`,
		`value="operator"`,
		`autocomplete="username"`,
	} {
		if !strings.Contains(page.Body.String(), expected) {
			t.Fatalf("security page missing %q", expected)
		}
	}
}

func TestSecurityPasswordIgnoresForgedUsername(t *testing.T) {
	handler := guardedSecurityHandler(t)
	session := signInGuardedAdmin(t, handler, "operator", "pw")
	changeForm := url.Values{
		"form":       {"password"},
		"csrf_token": {session.csrfToken},
		"username":   {"attacker"},
		"current":    {"pw"},
		"new":        {"new-password"},
		"confirm":    {"new-password"},
	}
	change := httptest.NewRecorder()
	changeRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/admin/security",
		strings.NewReader(changeForm.Encode()),
	)
	changeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	changeRequest.AddCookie(session.cookie)
	handler.ServeHTTP(change, changeRequest)
	if change.Code != http.StatusOK ||
		!strings.Contains(change.Body.String(), "Password changed.") {
		t.Fatalf("password change = %d, body=%s", change.Code, change.Body.String())
	}

	if status := guardedAdminLoginStatus(
		t,
		handler,
		"operator",
		"new-password",
	); status != http.StatusOK {
		t.Fatalf("session principal login = %d, want 200", status)
	}
	if status := guardedAdminLoginStatus(
		t,
		handler,
		"attacker",
		"new-password",
	); status != http.StatusUnauthorized {
		t.Fatalf("forged principal login = %d, want 401", status)
	}
}
