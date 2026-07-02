package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	"github.com/D4rk4/yago/yacynode/internal/crawldispatch"
)

func startTestNATS(t *testing.T) string {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}

func TestCrawlRuntimeDispatchAndConsume(t *testing.T) {
	storage, err := openNodeStorage(openTestVault(t))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}

	cfg := crawlConfig{
		NATSURL:       startTestNATS(t),
		OrdersSubject: defaultOrdersSubject,
		IngestSubject: defaultIngestSubject,
		IngestDurable: defaultIngestDurable,
		IngestMaxMsgs: defaultIngestMaxMsgs,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runtimeProcess, err := buildRuntimeCrawl(ctx, cfg, nodeIdentity(testConfig(t)), storage)
	if err != nil {
		t.Fatalf("build crawl runtime: %v", err)
	}
	runtime, ok := runtimeProcess.(*crawlRuntime)
	if !ok {
		t.Fatalf("runtime type = %T, want *crawlRuntime", runtimeProcess)
	}

	done := make(chan struct{})
	go func() { runtime.Run(ctx); close(done) }()

	mux := http.NewServeMux()
	runtime.mountDispatch(mux)

	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		crawldispatch.PathCrawlDispatch,
		strings.NewReader(`{"name":"docs","seeds":["https://example.org"],"maxPagesPerHost":-1}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("dispatch status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not stop after cancel")
	}
	runtime.Close()
}
