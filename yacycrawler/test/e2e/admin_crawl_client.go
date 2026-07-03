//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	pathAdminLogin = "/api/admin/v1/auth/login"
	pathCrawl      = "/crawl"
	pathIndexStats = "/api/admin/v1/index/stats"
	sessionCookie  = "yago_admin_session"
	csrfHeader     = "X-CSRF-Token"
)

type adminSession struct {
	cookie string
	csrf   string
}

func adminLogin(t *testing.T, ctx context.Context, opsURL string) adminSession {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, nodeAdminUser, nodeAdminPass)
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		opsURL+pathAdminLogin,
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("build login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("admin login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin login status = %d, body %q", resp.StatusCode, raw)
	}

	session := adminSession{}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessionCookie {
			session.cookie = cookie.Value
		}
	}
	var decoded struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	session.csrf = decoded.CSRFToken
	if session.cookie == "" || session.csrf == "" {
		t.Fatalf("login did not return a session cookie and csrf token")
	}

	return session
}

func dispatchCrawl(
	t *testing.T,
	ctx context.Context,
	opsURL string,
	session adminSession,
	seed string,
) {
	t.Helper()
	body := fmt.Sprintf(
		`{"name":"combined-e2e","seeds":[%q],"scope":"domain","maxDepth":0,"maxPagesPerHost":-1}`,
		seed,
	)
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		opsURL+pathCrawl,
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("build crawl request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeader, session.csrf)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session.cookie})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("dispatch crawl: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("dispatch crawl status = %d, body %q", resp.StatusCode, raw)
	}
}

func indexedDocuments(t *testing.T, ctx context.Context, opsURL string, session adminSession) int {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opsURL+pathIndexStats, nil)
	if err != nil {
		return 0
	}
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session.cookie})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var decoded struct {
		Documents int `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return 0
	}

	return decoded.Documents
}

func rawGet(ctx context.Context, endpoint, cookie string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "request error: " + err.Error()
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "get error: " + err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)

	return fmt.Sprintf("status=%d body=%s", resp.StatusCode, raw)
}
