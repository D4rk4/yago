package crawlseed_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/crawlseed"
	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
)

func TestHTTPSourceFetchesBoundedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "crawler/1.0" {
			t.Fatalf("user agent = %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer server.Close()

	page, err := crawlseed.NewHTTPSource(server.Client(), "crawler/1.0", 3).Fetch(
		context.Background(),
		mustParse(t, server.URL),
	)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(page.Body) != "abc" || page.ContentType != "text/plain" {
		t.Fatalf("page = %#v", page)
	}
}

func TestHTTPSourceRejectsNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()

	_, err := crawlseed.NewHTTPSource(server.Client(), "", 0).Fetch(
		context.Background(),
		mustParse(t, server.URL),
	)
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("error = %v, want page rejected", err)
	}
}

func TestHTTPSourceDetectsContentTypeAndReturnsRequestErrors(t *testing.T) {
	page, err := crawlseed.NewHTTPSource(
		&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("<xml></xml>")),
				Request:    request,
			}, nil
		})},
		"",
		0,
	).Fetch(context.Background(), mustParse(t, "https://example.org/sitemap.xml"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q", page.ContentType)
	}

	_, err = crawlseed.NewHTTPSource(http.DefaultClient, "", 0).Fetch(
		context.Background(),
		&url.URL{Scheme: "http", Host: "\n"},
	)
	if err == nil {
		t.Fatal("bad target should fail")
	}
}

func TestHTTPSourceUsesDefaultClient(t *testing.T) {
	if crawlseed.NewHTTPSource(nil, "", 0) == nil {
		t.Fatal("source is nil")
	}
}

func TestHTTPSourceReturnsNetworkAndReadErrors(t *testing.T) {
	sentinel := errors.New("network failed")
	_, err := crawlseed.NewHTTPSource(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, sentinel
		})},
		"",
		0,
	).Fetch(context.Background(), mustParse(t, "https://example.org/sitemap.xml"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("network error = %v, want %v", err, sentinel)
	}

	_, err = crawlseed.NewHTTPSource(
		&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: ioReadCloser{
					read:  func([]byte) (int, error) { return 0, io.ErrUnexpectedEOF },
					close: func() error { return nil },
				},
				Request: request,
			}, nil
		})},
		"",
		0,
	).Fetch(context.Background(), mustParse(t, "https://example.org/sitemap.xml"))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read error = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

func TestHTTPSourceReturnsEmptyContentTypeForEmptyBody(t *testing.T) {
	page, err := crawlseed.NewHTTPSource(
		&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    request,
			}, nil
		})},
		"",
		0,
	).Fetch(context.Background(), mustParse(t, "https://example.org/sitemap.xml"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.ContentType != "" {
		t.Fatalf("content type = %q", page.ContentType)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type ioReadCloser struct {
	read  func([]byte) (int, error)
	close func() error
}

func (c ioReadCloser) Read(p []byte) (int, error) {
	if c.read != nil {
		return c.read(p)
	}
	return 0, io.EOF
}

func (c ioReadCloser) Close() error {
	if c.close != nil {
		return c.close()
	}
	return nil
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	return parsed
}
