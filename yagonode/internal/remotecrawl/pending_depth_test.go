package remotecrawl

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestPendingCountTracksAvailableRemoteCrawlWork(t *testing.T) {
	broker, _ := openMemoryBroker(t, remoteConfig(time.Now), &recordingReceiver{})
	stageURL(t, broker, testURLA)
	stageURL(t, broker, testURLB)
	if count, err := broker.PendingCount(t.Context()); err != nil || count != 2 {
		t.Fatalf("PendingCount before lease = %d, %v", count, err)
	}
	if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err != nil {
		t.Fatal(err)
	}
	if count, err := broker.PendingCount(t.Context()); err != nil || count != 1 {
		t.Fatalf("PendingCount after lease = %d, %v", count, err)
	}
}

func TestPendingCountSurfacesStorageFailure(t *testing.T) {
	broker, storage := openMemoryBroker(t, remoteConfig(time.Now), &recordingReceiver{})
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.PendingCount(context.Background()); err == nil {
		t.Fatal("PendingCount succeeded after storage close")
	}
}

func TestPendingCountSurfacesLengthFailure(t *testing.T) {
	broker, _, engine := openRemoteCrawlFaultBroker(t)
	engine.putRaw(
		vault.Name("__lengths__"),
		vault.Key(remoteCrawlPendingBucket),
		[]byte("bad"),
	)
	if _, err := broker.PendingCount(t.Context()); err == nil {
		t.Fatal("PendingCount accepted a corrupt pending length")
	}
}
