package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestAssembleNodeSurfacesClosesCrawlRuntimeAfterStoreFailure(t *testing.T) {
	restoreAssemblySeams(t)
	runtime := &recordingCrawl{}
	buildRuntimeCrawl = func(
		context.Context,
		crawlConfig,
		nodeidentity.Identity,
		nodeStorage,
		*vault.Vault,
	) (crawlProcess, error) {
		return runtime, nil
	}
	engine := newCtrlEngine()
	engine.failProvision["rankingprofile"] = true

	_, err := assembleNodeSurfaces(assembleSurfacesInput{
		ctx: context.Background(), config: testConfig(t), vault: ctrlVault(t, engine),
	})
	if err == nil {
		t.Fatal("assemble surfaces accepted ranking store failure")
	}
	if !runtime.closed.Load() {
		t.Fatal("crawl runtime remained open after surface assembly failed")
	}
}
