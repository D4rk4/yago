package infrastructure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

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
		"!!! not a seed line",
		seedlistLine(t, "BBBBBBBBBBBB", "203.0.113.2"),
	}, "\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	fetcher := NewHTTPSeedlistFetcher(server.Client())
	seeds, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seeds) != 2 {
		t.Fatalf("got %d seeds, want 2 (bad line skipped)", len(seeds))
	}
	if ip, ok := seeds[0].IP.Get(); !ok || ip != "203.0.113.1" {
		t.Errorf("first ip = %q, %v", ip, ok)
	}
}

func TestSeedlistFetcherRejectsNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer server.Close()

	fetcher := NewHTTPSeedlistFetcher(server.Client())
	if _, err := fetcher.Fetch(context.Background(), server.URL); err == nil {
		t.Fatal("expected error on non-200")
	}
}
