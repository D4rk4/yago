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
)

type guardedOrderClearCrawlSource struct{}

func (guardedOrderClearCrawlSource) Start(
	context.Context,
	adminui.CrawlStart,
) (adminui.CrawlDispatch, error) {
	return adminui.CrawlDispatch{}, nil
}

type guardedOrderClearMonitor struct{}

func (guardedOrderClearMonitor) Monitor(context.Context) adminui.CrawlMonitor {
	return adminui.CrawlMonitor{QueueAvailable: true, QueuePending: 2, QueueLeased: 1}
}

type guardedOrderClearSource struct {
	calls int
}

func (source *guardedOrderClearSource) ClearPendingOrders(context.Context) (int, error) {
	source.calls++

	return 2, nil
}

func TestAdminOrderClearRequiresSessionAndFormCSRF(t *testing.T) {
	clearSource := &guardedOrderClearSource{}
	options := adminui.Options{
		Crawl:                 guardedOrderClearCrawlSource{},
		Monitor:               guardedOrderClearMonitor{},
		CrawlOrderClearSource: clearSource,
	}
	ops := http.NewServeMux()
	ops.Handle(adminui.BasePath, adminui.New(options))
	auth, err := provisionAdminAuth(context.Background(), nodeConfig{
		Admin: adminConfig{Username: "operator", Password: "local-test-password"},
	}, openTestVault(t), nil)
	if err != nil {
		t.Fatalf("provisionAdminAuth: %v", err)
	}
	handler := guardAdminSurface(auth, ops)

	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		adminauth.PathLogin,
		strings.NewReader(`{"username":"operator","password":"local-test-password"}`),
	)
	loginRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", login.Code)
	}
	var loginResponse struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &loginResponse); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if loginResponse.CSRFToken == "" {
		t.Fatal("login response has no CSRF token")
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(cookies))
	}

	post := func(cookie *http.Cookie, csrfToken string) *httptest.ResponseRecorder {
		form := url.Values{"confirmation": {"CLEAR PENDING ORDERS"}}
		if csrfToken != "" {
			form.Set("csrf_token", csrfToken)
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(
			context.Background(),
			http.MethodPost,
			"/admin/crawl/queue/clear",
			strings.NewReader(form.Encode()),
		)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			request.AddCookie(cookie)
		}
		handler.ServeHTTP(recorder, request)

		return recorder
	}

	if got := post(nil, loginResponse.CSRFToken); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated POST status = %d, want 401", got.Code)
	}
	if got := post(cookies[0], ""); got.Code != http.StatusForbidden {
		t.Fatalf("POST without CSRF status = %d, want 403", got.Code)
	}
	if clearSource.calls != 0 {
		t.Fatalf("clear calls before authorized POST = %d, want 0", clearSource.calls)
	}
	if got := post(cookies[0], loginResponse.CSRFToken); got.Code != http.StatusOK {
		t.Fatalf("authorized POST status = %d, want 200: %s", got.Code, got.Body.String())
	}
	if clearSource.calls != 1 {
		t.Fatalf("clear calls after authorized POST = %d, want 1", clearSource.calls)
	}
}
