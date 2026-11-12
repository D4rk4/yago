package adminui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type crawlOrderImporterFixture struct {
	calls  int
	batch  CrawlOrderImportBatch
	result CrawlOrderImportResult
	err    error
}

func (f *crawlOrderImporterFixture) ImportCrawlOrder(
	_ context.Context,
	batch CrawlOrderImportBatch,
) (CrawlOrderImportResult, error) {
	f.calls++
	f.batch = batch
	return f.result, f.err
}

func TestCrawlOrderImportValidatesDryRunAndPersistsThroughImporter(t *testing.T) {
	importer := &crawlOrderImporterFixture{result: CrawlOrderImportResult{EnqueuedRequests: 1}}
	console := New(Options{CrawlOrderImport: importer})
	batch := validCrawlOrderImportBatch(t)
	batch.DryRun = true
	response := performCrawlOrderImport(t, console, batch)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"validated":true`) {
		t.Fatalf("dry-run response = %d %s", response.Code, response.Body.String())
	}
	if importer.calls != 0 {
		t.Fatal("dry run reached durable queue importer")
	}

	batch.DryRun = false
	response = performCrawlOrderImport(t, console, batch)
	if response.Code != http.StatusOK || importer.calls != 1 ||
		!strings.Contains(response.Body.String(), `"enqueuedRequests":1`) {
		t.Fatalf(
			"import response = %d %s; calls=%d",
			response.Code,
			response.Body.String(),
			importer.calls,
		)
	}
	if importer.batch.BatchID != batch.BatchID {
		t.Fatal("importer received a different idempotent batch")
	}
}

func TestCrawlOrderImportRejectsMalformedOrTamperedPayload(t *testing.T) {
	importer := new(crawlOrderImporterFixture)
	console := New(Options{CrawlOrderImport: importer})
	valid := validCrawlOrderImportBatch(t)
	body, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		`{"version":2}`,
		`{"version":1,"unknown":true}`,
		strings.Replace(string(body), `https://example.test/page`, `https://example.test/changed`, 1),
	} {
		response := performCrawlOrderImportBody(t, console, input)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid frontier status = %d; body=%s", response.Code, response.Body.String())
		}
	}
	if importer.calls != 0 {
		t.Fatal("invalid frontier reached the importer")
	}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		crawlOrderImportPath,
		strings.NewReader(string(body)),
	)
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	console.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("unsupported content type status = %d", response.Code)
	}
	request = httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		crawlOrderImportPath,
		failedFrontierBody{},
	)
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	console.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("failed body status = %d", response.Code)
	}
}

func TestCrawlOrderImportHandlesUnavailableImporterServiceFailureAndBodyLimits(t *testing.T) {
	batch := validCrawlOrderImportBatch(t)
	body, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}

	response := performCrawlOrderImportBody(t, New(Options{}), string(body))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unconfigured importer status = %d", response.Code)
	}

	importer := &crawlOrderImporterFixture{err: errors.New("fixture secret failure")}
	console := New(Options{CrawlOrderImport: importer})
	response = performCrawlOrderImportBody(t, console, string(body))
	if response.Code != http.StatusServiceUnavailable ||
		strings.Contains(response.Body.String(), "fixture secret failure") {
		t.Fatalf("failed import response = %d %s", response.Code, response.Body.String())
	}
	if importer.calls != 1 {
		t.Fatalf("importer calls = %d, want 1", importer.calls)
	}

	response = performCrawlOrderImportBody(t, console, string(body)+string(body))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("multiple JSON values status = %d", response.Code)
	}
	oversized := strings.Repeat("x", yagocrawlcontract.MaximumCrawlOrderImportBatchBytes+1)
	response = performCrawlOrderImportBody(t, console, oversized)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d", response.Code)
	}
}

type failedFrontierBody struct{}

func (failedFrontierBody) Read([]byte) (int, error) { return 0, errors.New("fixture read failure") }
func (failedFrontierBody) Close() error             { return nil }

func validCrawlOrderImportBatch(t *testing.T) CrawlOrderImportBatch {
	t.Helper()
	batch := CrawlOrderImportBatch{
		Version:  1,
		SourceID: "sanitized-source",
		Profile: yagocrawlcontract.CrawlProfile{
			Handle:          "fixture-profile",
			Name:            "sanitized fixture",
			URLMustMatch:    yagocrawlcontract.MatchAll,
			MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		},
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:           "https://example.test/page",
			ProfileHandle: "fixture-profile",
		}},
	}
	var err error
	batch.BatchID, err = batch.ComputeBatchID()
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func performCrawlOrderImport(
	t *testing.T,
	console http.Handler,
	batch CrawlOrderImportBatch,
) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	return performCrawlOrderImportBody(t, console, string(body))
}

func performCrawlOrderImportBody(
	t *testing.T,
	console http.Handler,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		crawlOrderImportPath,
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	console.ServeHTTP(response, request)
	return response
}
