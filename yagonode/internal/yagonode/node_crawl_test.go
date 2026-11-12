package yagonode

import (
	"context"
	"testing"
)

func TestCrawlRuntimeExposesRunRegistry(t *testing.T) {
	storageVault := openTestVault(t)
	storage, err := openNodeStorage(storageVault, "")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	runtimeProcess, err := buildRuntimeCrawl(
		context.Background(),
		crawlConfig{ListenAddr: "127.0.0.1:0"},
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

	if runtime.runRegistry() == nil {
		t.Fatal("runRegistry must be wired on a live crawl runtime")
	}
	if got := runtime.runRegistry().Len(); got != 0 {
		t.Fatalf("fresh registry len = %d, want 0", got)
	}
}
