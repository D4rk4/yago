//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	pathAdminLogin    = "/api/admin/v1/auth/login"
	pathCrawl         = "/crawl"
	pathIndexStats    = "/api/admin/v1/index/stats"
	pathExplain       = "/api/admin/v1/search/explain"
	pathModel         = "/api/admin/v1/search/ranking/model"
	pathTrain         = "/api/admin/v1/search/ranking/model/train"
	pathSearchJSON    = "/yacysearch.json"
	sessionCookie     = "yago_admin_session"
	csrfHeader        = "X-CSRF-Token"
	combinedCrawlName = "combined-e2e"
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
) string {
	return dispatchCrawlWithDepth(t, ctx, opsURL, session, seed, 0)
}

func dispatchCrawlWithDepth(
	t *testing.T,
	ctx context.Context,
	opsURL string,
	session adminSession,
	seed string,
	depth int,
) string {
	t.Helper()
	body := fmt.Sprintf(
		`{"name":%q,"seeds":[%q],"scope":"domain","maxDepth":%d,"maxPagesPerHost":-1}`,
		combinedCrawlName,
		seed,
		depth,
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
	req.Header.Set("Cookie", sessionCookie+"="+session.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("dispatch crawl: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("dispatch crawl status = %d, body %q", resp.StatusCode, raw)
	}
	var accepted struct {
		ProfileHandle string `json:"profileHandle"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		t.Fatalf("decode crawl dispatch response: %v", err)
	}
	if accepted.ProfileHandle == "" {
		t.Fatal("crawl dispatch response did not contain a profile handle")
	}

	return accepted.ProfileHandle
}

func crawlMonitorBody(
	ctx context.Context,
	opsURL string,
	session adminSession,
) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opsURL+"/admin/crawl/monitor", nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Cookie", sessionCookie+"="+session.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	raw, err := io.ReadAll(resp.Body)

	return string(raw), err == nil
}

func indexedDocuments(t *testing.T, ctx context.Context, opsURL string, session adminSession) int {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opsURL+pathIndexStats, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Cookie", sessionCookie+"="+session.cookie)
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

func searchFindsTerm(ctx context.Context, peerURL, term string) bool {
	endpoint := peerURL + pathSearchJSON + "?query=" + url.QueryEscape(term)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var decoded struct {
		Channels []struct {
			TotalResults string            `json:"totalResults"`
			Items        []json.RawMessage `json:"items"`
		} `json:"channels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return false
	}
	if len(decoded.Channels) == 0 {
		return false
	}
	channel := decoded.Channels[0]

	return len(channel.Items) > 0 || (channel.TotalResults != "" && channel.TotalResults != "0")
}

func rankingExplainFindsTerm(
	ctx context.Context,
	opsURL string,
	session adminSession,
	term string,
) bool {
	body := fmt.Sprintf(`{"query":%q}`, term)
	resp, ok := authenticatedPost(ctx, opsURL+pathExplain, session, body)
	if !ok {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	var decoded struct {
		Query   string `json:"query"`
		Results []struct {
			URL         string             `json:"url"`
			FieldScores map[string]float64 `json:"fieldScores"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return false
	}

	return decoded.Query == term && len(decoded.Results) > 0 &&
		decoded.Results[0].URL != "" && len(decoded.Results[0].FieldScores) > 0
}

func rankingModelIsInactive(
	ctx context.Context,
	opsURL string,
	session adminSession,
) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opsURL+pathModel, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Cookie", sessionCookie+"="+session.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var decoded struct {
		Status struct {
			Current struct {
				Active bool `json:"active"`
			} `json:"current"`
			Rollback []json.RawMessage `json:"rollback"`
		} `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return false
	}

	return !decoded.Status.Current.Active && len(decoded.Status.Rollback) == 0
}

func rankingTrainingRejectsColdStart(
	ctx context.Context,
	opsURL string,
	session adminSession,
) bool {
	resp, ok := authenticatedPost(
		ctx,
		opsURL+pathTrain,
		session,
		`{"model_kind":"linear_lambdarank"}`,
	)
	if !ok {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)

	return resp.StatusCode == http.StatusBadRequest &&
		strings.Contains(string(raw), "train ranking model")
}

func authenticatedPost(
	ctx context.Context,
	endpoint string,
	session adminSession,
	body string,
) (*http.Response, bool) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		strings.NewReader(body),
	)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeader, session.csrf)
	req.Header.Set("Cookie", sessionCookie+"="+session.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}

	return resp, true
}

func rawGet(ctx context.Context, endpoint, cookie string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "request error: " + err.Error()
	}
	if cookie != "" {
		req.Header.Set("Cookie", sessionCookie+"="+cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "get error: " + err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)

	return fmt.Sprintf("status=%d body=%s", resp.StatusCode, raw)
}
