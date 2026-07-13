package yagonode

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/clickcapture"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type blockedImpressionEngine struct {
	*ctrlEngine
	startedOnce sync.Once
	started     chan struct{}
	release     chan struct{}
}

func (e *blockedImpressionEngine) Update(
	ctx context.Context,
	operation func(vault.EngineTxn) error,
) error {
	e.startedOnce.Do(func() { close(e.started) })
	<-e.release

	return e.ctrlEngine.Update(ctx, operation)
}

func prepareBlockedImpression(t *testing.T, clicks *clickcapture.Store) {
	t.Helper()
	prepared, err := clicks.PrepareImpression(
		t.Context(),
		"drunklab",
		[]clickcapture.Candidate{{
			URLIdentity:     "https://about.me/drunklab",
			ClusterIdentity: "drunklab",
			Position:        1,
		}},
	)
	if err != nil || prepared.Token == "" {
		t.Errorf("prepared impression = %#v, error = %v", prepared, err)
	}
}

func TestBootJoinsRetainedImpressionsBeforeReturning(t *testing.T) {
	restoreMainSeams(t)
	engine := &blockedImpressionEngine{
		ctrlEngine: newCtrlEngine(),
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	clickVault, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	clicks, err := clickcapture.Open(clickVault)
	if err != nil {
		t.Fatal(err)
	}
	storageVault := openTestVault(t)
	assembleRuntimeNode = func(
		context.Context,
		nodeConfig,
		*vault.Vault,
		*http.Client,
		nodeTelemetry,
	) (node, error) {
		return node{
			announcer: fakeAnnouncer{},
			sweeper:   &scriptedSweeper{},
			vault:     storageVault,
			clicks:    clicks,
		}, nil
	}
	served := make(chan struct{})
	serveRuntimeNode = func(
		context.Context,
		node,
		*metrics.EvictionMetrics,
		...namedServer,
	) error {
		prepareBlockedImpression(t, clicks)
		close(served)

		return nil
	}
	finished := make(chan error, 1)
	config := testConfig(t)
	go func() {
		finished <- bootNode(
			t.Context(),
			config,
			storageVault,
			&http.Client{},
		)
	}()
	select {
	case <-engine.started:
	case <-time.After(time.Second):
		t.Fatal("retained impression did not start")
	}
	select {
	case <-served:
	case <-time.After(time.Second):
		t.Fatal("serve did not return from bounded impression preparation")
	}
	select {
	case err := <-finished:
		t.Fatalf("boot returned before retained impression finished: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(engine.release)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("boot did not return after retained impression finished")
	}
}
