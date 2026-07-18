package yagonode

import (
	"context"
	"strings"
	"testing"
)

func TestBuildCrawlRuntimeReportsRunRegistryOpenFailure(t *testing.T) {
	storageVault := openTestVault(t)
	storage, err := openNodeStorage(storageVault, "")
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	if err := storageVault.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}
	_, err = buildCrawlRuntime(
		context.Background(),
		crawlConfig{ListenAddr: "127.0.0.1:0"},
		nodeIdentity(testConfig(t)),
		storage,
		storageVault,
	)
	if err == nil || !strings.Contains(err.Error(), "open crawl run registry") {
		t.Fatalf("build error = %v", err)
	}
}
