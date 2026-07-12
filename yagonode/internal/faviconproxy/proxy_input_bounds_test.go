package faviconproxy

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type countedRejectingTransport struct {
	requests atomic.Int64
}

func (t *countedRejectingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.requests.Add(1)

	return nil, http.ErrHandlerTimeout
}

func exactDNSHost() string {
	return strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." +
		strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
}

func TestDNSHostExactAndPlusOneBounds(t *testing.T) {
	exact := exactDNSHost()
	if len(exact) != maximumDNSHostBytes || normalizedHost(exact) != exact {
		t.Fatalf("exact host length = %d", len(exact))
	}
	plusOne := exact + "e"
	if len(plusOne) != maximumDNSHostBytes+1 || normalizedHost(plusOne) != "" {
		t.Fatalf("plus-one host length = %d", len(plusOne))
	}
	label := strings.Repeat("a", maximumDNSLabelBytes)
	if normalizedHost(label+".example") == "" ||
		normalizedHost(label+"a.example") != "" {
		t.Fatal("DNS label boundary was not enforced")
	}
	for _, invalid := range []string{"-a.example", "a-.example", "a..example", "münich.example"} {
		if normalizedHost(invalid) != "" {
			t.Fatalf("invalid DNS host accepted: %q", invalid)
		}
	}
}

func TestFaviconRejectsOversizedHostBeforeFetch(t *testing.T) {
	transport := &countedRejectingTransport{}
	proxy := New(&http.Client{Transport: transport}, 1)
	if result := get(t, proxy, URLFor(exactDNSHost())); result.Code != http.StatusOK {
		t.Fatalf("exact host status = %d", result.Code)
	}
	requests := transport.requests.Load()
	for _, host := range []string{
		exactDNSHost() + "a",
		strings.Repeat("a", maximumDNSLabelBytes+1) + ".example",
	} {
		if result := get(t, proxy, URLFor(host)); result.Code != http.StatusBadRequest {
			t.Fatalf("invalid host status = %d", result.Code)
		}
	}
	if got := transport.requests.Load(); got != requests {
		t.Fatalf("invalid hosts fetched: before=%d after=%d", requests, got)
	}
}

func exactImageURL() string {
	prefix := "https://example.org/image?payload="

	return prefix + strings.Repeat("a", maximumImageURLBytes-len(prefix))
}

func TestImageURLExactAndPlusOneBounds(t *testing.T) {
	exact := exactImageURL()
	if len(exact) != maximumImageURLBytes || normalizedImageURL(exact) != exact {
		t.Fatalf("exact image URL length = %d", len(exact))
	}
	if normalizedImageURL(exact+"a") != "" {
		t.Fatal("plus-one image URL was accepted")
	}
	expanding := "https://example.org/" + strings.Repeat("é", 4000)
	if len(expanding) > maximumImageURLBytes || normalizedImageURL(expanding) != "" {
		t.Fatal("expanded normalized URL exceeded the retained bound")
	}
	if got := normalizedImageURL(
		"HTTPS://Example.org/image#fragment",
	); got != "https://example.org/image" {
		t.Fatalf("normalized image URL = %q", got)
	}
	if got := normalizedImageURL(
		"https://Example.org:8443/image",
	); got != "https://example.org:8443/image" {
		t.Fatalf("normalized image port URL = %q", got)
	}
	if normalizedImageURL("https://"+strings.Repeat("a", 64)+".example/image") != "" {
		t.Fatal("oversized image host label was accepted")
	}
	if normalizedImageURL("https://[2001:db8::1]/image") == "" {
		t.Fatal("absolute IPv6 image URL was rejected")
	}
}

func TestImageProxyRejectsOversizedURLBeforeFetch(t *testing.T) {
	transport := &countedRejectingTransport{}
	proxy := NewImageProxy(&http.Client{Transport: transport})
	if result := imageGet(t, proxy, ImageURLFor(exactImageURL())); result.Code != http.StatusOK {
		t.Fatalf("exact URL status = %d", result.Code)
	}
	requests := transport.requests.Load()
	if result := imageGet(
		t,
		proxy,
		ImageURLFor(exactImageURL()+"a"),
	); result.Code != http.StatusBadRequest {
		t.Fatalf("oversized URL status = %d", result.Code)
	}
	oversizedHost := "https://" + strings.Repeat("a", maximumDNSLabelBytes+1) +
		".example/image"
	if result := imageGet(
		t,
		proxy,
		ImageURLFor(oversizedHost),
	); result.Code != http.StatusBadRequest {
		t.Fatalf("oversized image host status = %d", result.Code)
	}
	if got := transport.requests.Load(); got != requests {
		t.Fatalf("oversized URL fetched: before=%d after=%d", requests, got)
	}
}

func TestProxyCacheBudgetsAreThirtyTwoMiB(t *testing.T) {
	if maxCacheBytes != 32<<20 || imageCacheBytes != 32<<20 {
		t.Fatalf("cache budgets = %d %d", maxCacheBytes, imageCacheBytes)
	}
}
