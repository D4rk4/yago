package yagonode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

type indexImportAuthFixture struct {
	calls int
}

type rwiBackfillAuthFixture struct {
	starts int
}

func (f *rwiBackfillAuthFixture) Start(
	_ context.Context,
	maximum int64,
	dryRun bool,
) (adminui.RWIBackfillStatus, error) {
	f.starts++
	return adminui.RWIBackfillStatus{
		State:               "running",
		MaximumDocumentsRun: maximum,
		DryRun:              dryRun,
	}, nil
}

func (*rwiBackfillAuthFixture) Pause(context.Context) (adminui.RWIBackfillStatus, error) {
	return adminui.RWIBackfillStatus{State: "paused"}, nil
}

func (*rwiBackfillAuthFixture) Resume(
	context.Context,
	int64,
	bool,
) (adminui.RWIBackfillStatus, error) {
	return adminui.RWIBackfillStatus{State: "running"}, nil
}

func (*rwiBackfillAuthFixture) Status(context.Context) (adminui.RWIBackfillStatus, error) {
	return adminui.RWIBackfillStatus{State: "idle"}, nil
}

func TestRWIBackfillRequiresScopedAdminKeys(t *testing.T) {
	service, err := provisionAdminAuth(context.Background(), nodeConfig{}, openTestVault(t), nil)
	if err != nil {
		t.Fatalf("provision auth: %v", err)
	}
	readKey, err := service.CreateAPIKey(
		context.Background(),
		"backfill read test",
		[]string{string(adminauth.ScopeAdminRead)},
	)
	if err != nil {
		t.Fatal(err)
	}
	writeKey, err := service.CreateAPIKey(
		context.Background(),
		"backfill write test",
		[]string{string(adminauth.ScopeAdminWrite)},
	)
	if err != nil {
		t.Fatal(err)
	}
	controller := new(rwiBackfillAuthFixture)
	ops := http.NewServeMux()
	ops.Handle(adminui.BasePath, adminui.New(adminui.Options{RWIBackfill: controller}))
	handler := guardAdminSurface(service, ops)
	request := func(method, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(
			t.Context(), method, "/admin/index/rwi-backfill", strings.NewReader(body),
		)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if got := request(http.MethodGet, "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", got.Code)
	}
	if got := request(http.MethodGet, writeKey.Secret, ""); got.Code != http.StatusForbidden {
		t.Fatalf("admin:write status read = %d, want 403", got.Code)
	}
	if got := request(http.MethodGet, readKey.Secret, ""); got.Code != http.StatusOK {
		t.Fatalf("admin:read status = %d %s, want 200", got.Code, got.Body.String())
	}
	body := `{"version":1,"action":"start","maximumDocuments":1,"dryRun":true}`
	if got := request(http.MethodPost, readKey.Secret, body); got.Code != http.StatusForbidden {
		t.Fatalf("admin:read start = %d, want 403", got.Code)
	}
	if got := request(http.MethodPost, writeKey.Secret, body); got.Code != http.StatusOK {
		t.Fatalf("admin:write start = %d %s, want 200", got.Code, got.Body.String())
	}
	if controller.starts != 1 {
		t.Fatalf("backfill start calls = %d, want only authorized write", controller.starts)
	}
}

func (f *indexImportAuthFixture) ImportDocuments(
	_ context.Context,
	records []adminui.IndexImportRecord,
) ([]adminui.IndexImportResult, error) {
	f.calls++
	return []adminui.IndexImportResult{{ID: records[0].ID, Status: "imported"}}, nil
}

func TestIndexImportRequiresScopedAdminWriteKey(t *testing.T) {
	service, err := provisionAdminAuth(context.Background(), nodeConfig{}, openTestVault(t), nil)
	if err != nil {
		t.Fatalf("provision auth: %v", err)
	}
	readKey, err := service.CreateAPIKey(
		context.Background(),
		"read-only import test",
		[]string{string(adminauth.ScopeAdminRead)},
	)
	if err != nil {
		t.Fatalf("create read key: %v", err)
	}
	writeKey, err := service.CreateAPIKey(
		context.Background(),
		"write import test",
		[]string{string(adminauth.ScopeAdminWrite)},
	)
	if err != nil {
		t.Fatalf("create write key: %v", err)
	}
	importer := new(indexImportAuthFixture)
	ops := http.NewServeMux()
	ops.Handle(adminui.BasePath, adminui.New(adminui.Options{IndexImport: importer}))
	handler := guardAdminSurface(service, ops)
	body := `{"version":1,"records":[{"id":"fixture-row","document":{"normalizedURL":"https://fixture.example.test/page","contentHash":"fixture-hash","extractedText":"safe fixture"}}]}`
	request := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			"/admin/index/import",
			strings.NewReader(body),
		)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if got := request(""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated import = %d, want 401", got.Code)
	}
	if got := request(readKey.Secret); got.Code != http.StatusForbidden {
		t.Fatalf("admin:read import = %d, want 403", got.Code)
	}
	if got := request(writeKey.Secret); got.Code != http.StatusOK {
		t.Fatalf("admin:write import = %d %s, want 200", got.Code, got.Body.String())
	}
	if importer.calls != 1 {
		t.Fatalf("importer calls = %d, want only the authorized write", importer.calls)
	}
}

type frontierImportAuthFixture struct{ calls int }

func (f *frontierImportAuthFixture) ImportCrawlOrder(
	context.Context,
	adminui.CrawlOrderImportBatch,
) (adminui.CrawlOrderImportResult, error) {
	f.calls++
	return adminui.CrawlOrderImportResult{EnqueuedRequests: 1}, nil
}

func TestCrawlOrderImportRequiresScopedAdminWriteKey(t *testing.T) {
	service, err := provisionAdminAuth(context.Background(), nodeConfig{}, openTestVault(t), nil)
	if err != nil {
		t.Fatalf("provision auth: %v", err)
	}
	readKey, err := service.CreateAPIKey(
		context.Background(),
		"frontier read key",
		[]string{string(adminauth.ScopeAdminRead)},
	)
	if err != nil {
		t.Fatal(err)
	}
	writeKey, err := service.CreateAPIKey(
		context.Background(),
		"frontier write key",
		[]string{string(adminauth.ScopeAdminWrite)},
	)
	if err != nil {
		t.Fatal(err)
	}
	importer := new(frontierImportAuthFixture)
	ops := http.NewServeMux()
	ops.Handle(adminui.BasePath, adminui.New(adminui.Options{CrawlOrderImport: importer}))
	handler := guardAdminSurface(service, ops)
	batch := validFrontierImportBatch(t, "https://example.test/pending")
	body, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	request := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			"/admin/crawl/frontier/import",
			strings.NewReader(string(body)),
		)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if got := request(""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated frontier import = %d, want 401", got.Code)
	}
	if got := request(readKey.Secret); got.Code != http.StatusForbidden {
		t.Fatalf("admin:read frontier import = %d, want 403", got.Code)
	}
	if got := request(writeKey.Secret); got.Code != http.StatusOK {
		t.Fatalf("admin:write frontier import = %d %s, want 200", got.Code, got.Body.String())
	}
	if importer.calls != 1 {
		t.Fatalf("frontier importer calls = %d, want only authorized write", importer.calls)
	}
}
