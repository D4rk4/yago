package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

type failingCloser struct{}

func (failingCloser) Close() error {
	return errors.New("close failed")
}

func seedlistLine(t *testing.T, hash, ip string) string {
	t.Helper()

	host, err := yacymodel.ParseHost(ip)
	if err != nil {
		t.Fatalf("parse host: %v", err)
	}
	seed := yacymodel.Seed{
		Hash: yacymodel.Hash(hash),
		IP:   yacymodel.Some(host),
		Port: yacymodel.Some(yacymodel.Port(8090)),
	}

	return yacymodel.EncodeCompactWireForm(seed.String())
}

func TestSeedlistFetcherDecodesLines(t *testing.T) {
	body := strings.Join([]string{
		seedlistLine(t, "AAAAAAAAAAAA", "203.0.113.1"),
		"",
		"q|data",
		"!!! not a seed line",
		yacymodel.EncodeCompactWireForm("not a seed"),
		seedlistLine(t, "BBBBBBBBBBBB", "203.0.113.2"),
	}, "\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = strings.NewReader(body).WriteTo(w)
	}))
	defer server.Close()

	fetcher := newHTTPSeedlistFetcher(server.Client())
	seeds, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seeds) != 2 {
		t.Fatalf("got %d seeds, want 2 (bad line skipped)", len(seeds))
	}
}

func TestSeedlistFetcherRejectsNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer server.Close()

	fetcher := newHTTPSeedlistFetcher(server.Client())
	if _, err := fetcher.Fetch(context.Background(), server.URL); err == nil {
		t.Fatal("expected error on non-200")
	}
}

func TestSeedlistFetcherRejectsInvalidURL(t *testing.T) {
	fetcher := newHTTPSeedlistFetcher(http.DefaultClient)

	if _, err := fetcher.Fetch(context.Background(), "http://[::1"); err == nil {
		t.Fatal("expected invalid URL error")
	}
}

func TestSeedlistFetcherReturnsClientError(t *testing.T) {
	sentinel := errors.New("transport failed")
	fetcher := newHTTPSeedlistFetcher(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, sentinel
		}),
	})

	if _, err := fetcher.Fetch(
		context.Background(),
		"http://example.test/seed.txt",
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("Fetch error = %v, want %v", err, sentinel)
	}
}

func TestDecodeSeedlistReturnsScannerError(t *testing.T) {
	if _, err := decodeSeedlist(
		context.Background(),
		failingReader{},
		"http://example.test/seed.txt",
	); err == nil {
		t.Fatal("expected scanner error")
	}
}

func TestCloseResponseBodyLogsCloseError(t *testing.T) {
	closeResponseBody(context.Background(), failingCloser{}, "test")
}
