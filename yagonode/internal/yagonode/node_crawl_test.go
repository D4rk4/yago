package yagonode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
)

func TestCrawlRuntimeDispatchAndConsume(t *testing.T) {
	storageVault := openTestVault(t)
	storage, err := openNodeStorage(storageVault, "")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}

	cfg := crawlConfig{ListenAddr: "127.0.0.1:0"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runtimeProcess, err := buildRuntimeCrawl(
		cfg,
		nodeIdentity(testConfig(t)),
		storage,
		storageVault,
	)
	if err != nil {
		t.Fatalf("build crawl runtime: %v", err)
	}
	runtime, ok := runtimeProcess.(*crawlRuntime)
	if !ok {
		t.Fatalf("runtime type = %T, want *crawlRuntime", runtimeProcess)
	}
	defer runtime.Close()

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
}
