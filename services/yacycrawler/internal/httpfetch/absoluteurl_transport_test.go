package httpfetch_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/httpfetch"
)

func TestFetchAbsoluteURLModeSendsNoConnect(t *testing.T) {
	var gotMethod, gotRequestURI string
	proxy, closeFn := proxyURL(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotRequestURI = r.RequestURI
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>hi</html>"))
	})
	defer closeFn()

	outcome, err := httpfetch.New(proxy, httpfetch.ProxyDialAbsoluteURL, testUserAgent, 1<<20, time.Second).
		Fetch(context.Background(), "https://target.example/page")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if outcome.Status != crawlcapability.FetchSucceeded {
		t.Fatalf("kind = %v", outcome.Status)
	}
	if gotMethod == http.MethodConnect {
		t.Fatal("absolute-url mode must not send CONNECT")
	}
	if gotRequestURI != "https://target.example/page" {
		t.Fatalf("request-uri = %q, want absolute URI", gotRequestURI)
	}
}

func TestFetchAbsoluteURLModeTransientOnDialFailure(t *testing.T) {
	proxy, _ := url.Parse("http://127.0.0.1:1")
	outcome, err := httpfetch.New(proxy, httpfetch.ProxyDialAbsoluteURL, testUserAgent, 1<<20, time.Second).
		Fetch(context.Background(), "https://target.example/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != crawlcapability.FetchTransient {
		t.Fatalf("kind = %v, want transient", outcome.Status)
	}
}

func TestFetchAbsoluteURLModeHandlesHTTPTarget(t *testing.T) {
	proxy, closeFn := proxyURL(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("plain"))
	})
	defer closeFn()

	outcome, err := httpfetch.New(proxy, httpfetch.ProxyDialAbsoluteURL, testUserAgent, 1<<20, time.Second).
		Fetch(context.Background(), "http://target.example/page")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(outcome.Body) != "plain" {
		t.Fatalf("body = %q", outcome.Body)
	}
}
