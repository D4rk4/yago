package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
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

	host, err := yagomodel.ParseHost(ip)
	if err != nil {
		t.Fatalf("parse host: %v", err)
	}
	seed := yagomodel.Seed{
		Hash: yagomodel.Hash(hash),
		IP:   yagomodel.Some(host),
		Port: yagomodel.Some(yagomodel.Port(8090)),
	}

	return yagomodel.EncodeCompactWireForm(seed.String())
}

func TestSeedlistFetcherDecodesLines(t *testing.T) {
	body := strings.Join([]string{
		seedlistLine(t, "AAAAAAAAAAAA", "203.0.113.1"),
		"",
		"q|data",
		"!!! not a seed line",
		yagomodel.EncodeCompactWireForm("not a seed"),
		yagomodel.EncodeCompactWireForm(
			"{Hash=CCCCCCCCCCCC,IP=203.0.113.3,Port=8090,UTC=20260614000329}",
		),
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
	if len(seeds) != 3 {
		t.Fatalf("got %d seeds, want 3 (bad line skipped)", len(seeds))
	}
	if utc, ok := seeds[1].UTC.Get(); !ok || utc.String() != "20260614000329" {
		t.Fatalf("timestamp UTC seed = %q, %v", utc, ok)
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

func TestDecodeSeedlistRejectsCompressedSeedInflation(t *testing.T) {
	bomb := yagomodel.EncodeCompactWireForm(
		"Hash=AAAAAAAAAAAA,Custom=" + strings.Repeat("x", 33<<10),
	)
	valid := seedlistLine(t, "BBBBBBBBBBBB", "203.0.113.2")
	seeds, err := decodeSeedlist(
		t.Context(),
		strings.NewReader(bomb+"\n"+valid),
		"http://example.test/seed.txt",
	)
	if err != nil {
		t.Fatalf("decodeSeedlist: %v", err)
	}
	if len(seeds) != 1 || seeds[0].Hash != "BBBBBBBBBBBB" {
		t.Fatalf("decoded seeds = %#v, want only valid seed", seeds)
	}
}

func TestDecodeSeedlistBoundsDecodedEntries(t *testing.T) {
	line := seedlistLine(t, "AAAAAAAAAAAA", "203.0.113.1") + "\n"
	seeds, err := decodeSeedlist(
		t.Context(),
		strings.NewReader(strings.Repeat(line, seedlistMaxEntries+1)),
		"http://example.test/seed.txt",
	)
	if err != nil {
		t.Fatalf("decodeSeedlist: %v", err)
	}
	if len(seeds) != seedlistMaxEntries {
		t.Fatalf("decoded seeds = %d, want %d", len(seeds), seedlistMaxEntries)
	}
}

func TestDecodeSeedlistBoundsRetainedBytes(t *testing.T) {
	line := yagomodel.EncodeCompactWireForm(
		"Hash=AAAAAAAAAAAA,Custom="+strings.Repeat("x", 8<<10),
	) + "\n"
	seeds, err := decodeSeedlist(
		t.Context(),
		strings.NewReader(strings.Repeat(line, seedlistMaxEntries)),
		"http://example.test/seed.txt",
	)
	if err != nil {
		t.Fatalf("decodeSeedlist: %v", err)
	}
	retainedBytes := 0
	for _, seed := range seeds {
		retainedBytes += seed.RetainedBytes()
	}
	if retainedBytes > seedlistMaxRetainedBytes {
		t.Fatalf("retained seed bytes = %d, maximum %d", retainedBytes, seedlistMaxRetainedBytes)
	}
	if len(seeds) == 0 || len(seeds) >= seedlistMaxEntries {
		t.Fatalf("retained seeds = %d, want bounded non-empty subset", len(seeds))
	}
}

func TestCloseResponseBodyLogsCloseError(t *testing.T) {
	closeResponseBody(context.Background(), failingCloser{}, "test")
}
