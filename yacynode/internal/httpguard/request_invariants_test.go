package httpguard_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

func testGuard() httpguard.RequestGuard {
	return httpguard.NewRequestGuard(32, time.Second)
}

func TestParseRejectsDisallowedMethod(t *testing.T) {
	guard := testGuard()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathTransferURL,
		nil,
	)

	_, _, _, ok := guard.Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if ok {
		t.Fatal("Parse accepted GET on a POST-only endpoint")
	}
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestParseRejectsUnknownMethod(t *testing.T) {
	guard := testGuard()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPatch,
		yacyproto.PathTransferURL,
		nil,
	)

	_, _, _, ok := guard.Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if ok {
		t.Fatal("Parse accepted PATCH")
	}
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestParseRejectsOversizedBody(t *testing.T) {
	guard := testGuard()
	rec := httptest.NewRecorder()
	body := strings.NewReader(strings.Repeat("x", 1024))
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathTransferURL,
		body,
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, _, _, ok := guard.Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if ok {
		t.Fatal("Parse accepted an oversized body")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestParseAcceptsValidPost(t *testing.T) {
	guard := testGuard()
	rec := httptest.NewRecorder()
	body := strings.NewReader("a=b")
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathTransferURL,
		body,
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	form, ctx, cancel, ok := guard.Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if !ok {
		t.Fatalf("Parse rejected a valid request, status %d", rec.Code)
	}
	defer cancel()
	if ctx == nil {
		t.Fatal("Parse returned a nil context")
	}
	if form.Get("a") != "b" {
		t.Fatalf("form[a] = %q, want b", form.Get("a"))
	}
}

func TestParseAllowsUnsupportedContentEncoding(t *testing.T) {
	guard := testGuard()
	rec := httptest.NewRecorder()
	body := strings.NewReader("a=b")
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathTransferURL,
		body,
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Content-Encoding", "br")

	form, _, cancel, ok := guard.Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if !ok {
		t.Fatalf("Parse rejected unsupported encoding fail-open path, status %d", rec.Code)
	}
	defer cancel()
	if form.Get("a") != "b" {
		t.Fatalf("form[a] = %q, want b", form.Get("a"))
	}
}

func TestParseRejectsInvalidGzipBody(t *testing.T) {
	guard := testGuard()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathTransferURL,
		strings.NewReader("not gzip"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Content-Encoding", "gzip")

	_, _, _, ok := guard.Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if ok {
		t.Fatal("Parse accepted invalid gzip body")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestParseRejectsMalformedForm(t *testing.T) {
	guard := testGuard()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathTransferURL,
		strings.NewReader("a=%zz"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, _, _, ok := guard.Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if ok {
		t.Fatal("Parse accepted malformed form")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestParseAcceptsMultipartForm(t *testing.T) {
	guard := httpguard.NewRequestGuard(256, time.Second)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("a", "b"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathTransferURL,
		&body,
	)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	form, _, cancel, ok := guard.Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if !ok {
		t.Fatalf("Parse rejected multipart form, status %d", rec.Code)
	}
	defer cancel()
	if form.Get("a") != "b" {
		t.Fatalf("form[a] = %q, want b", form.Get("a"))
	}
}
