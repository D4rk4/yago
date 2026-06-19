package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

func TestRunCrawlsSeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write(
			[]byte(`<html lang="en"><title>Hi</title><body>words here</body></html>`),
		); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	cfg := yacycrawler.DefaultConfig()
	cfg.SeedURLs = []string{server.URL}
	cfg.Workers = 1

	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
}
