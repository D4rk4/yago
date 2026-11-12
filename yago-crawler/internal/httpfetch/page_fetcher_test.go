package httpfetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagoegress"
)

func TestFetchReturnsBoundedResponseAndConfiguredIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.UserAgent() != "YagoCrawler/2" {
			t.Errorf("user agent = %q", request.UserAgent())
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><title>sample</title></html>")
	}))
	defer server.Close()
	fetcher := &PageFetcher{
		client:              server.Client(),
		maximumResponseSize: 128,
		userAgent:           "YagoCrawler/2",
	}
	page, err := fetcher.Fetch(context.Background(), server.URL+"/page#fragment")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if page.StatusCode != http.StatusOK || page.ContentType != "text/html; charset=utf-8" ||
		string(page.Body) != "<html><title>sample</title></html>" || page.FinalURL != server.URL+"/page" {
		t.Fatalf("page response = %#v", page)
	}
}

func TestFetchRejectsResponseOverTheByteLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "12345")
	}))
	defer server.Close()
	fetcher := &PageFetcher{
		client:              server.Client(),
		maximumResponseSize: 4,
		userAgent:           "YagoCrawler/2",
	}
	if _, err := fetcher.Fetch(context.Background(), server.URL); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestFetchRejectsURLCredentialsBeforeNetworkAccess(t *testing.T) {
	fetcher := &PageFetcher{
		client:              http.DefaultClient,
		maximumResponseSize: 1024,
		userAgent:           "YagoCrawler/2",
	}
	if _, err := fetcher.Fetch(context.Background(), "https://user:pass@example.org/"); err == nil {
		t.Fatal("URL with user information was accepted")
	}
}

func TestSetMaximumRedirectsChangesTheLiveFetchLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" {
			http.Redirect(w, request, "/target", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "ready")
	}))
	defer server.Close()
	fetcher, err := NewPageFetcher(Limits{
		RequestTimeout:      2 * time.Second,
		ConnectTimeout:      time.Second,
		TLSHandshakeTimeout: time.Second,
		HeaderTimeout:       time.Second,
		MaximumRedirects:    1,
		MaximumResponseSize: 128,
		MaximumIdleHosts:    2,
		MaximumHostActive:   1,
		UserAgent:           "YagoCrawler/2",
	}, yagoegress.NewGuard(true))
	if err != nil {
		t.Fatalf("create page fetcher: %v", err)
	}
	defer fetcher.CloseIdleConnections()
	if !fetcher.SetMaximumRedirects(0) {
		t.Fatal("zero redirect limit was rejected")
	}
	if fetcher.SetMaximumRedirects(-1) ||
		fetcher.SetMaximumRedirects(yagocrawlcontract.MaximumPageRedirects+1) {
		t.Fatal("invalid redirect limit was accepted")
	}
	if _, err := fetcher.Fetch(context.Background(), server.URL); err == nil {
		t.Fatal("redirect was followed after the limit was set to zero")
	}
	if !fetcher.SetMaximumRedirects(1) {
		t.Fatal("valid redirect limit was rejected")
	}
	page, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("fetch after raising redirect limit: %v", err)
	}
	if string(page.Body) != "ready" {
		t.Fatalf("redirected body = %q", page.Body)
	}
}
