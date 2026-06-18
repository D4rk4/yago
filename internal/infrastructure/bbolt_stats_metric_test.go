package infrastructure

import (
	"expvar"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestNilBboltStorageStats(t *testing.T) {
	var store *BboltStorage
	if store.Stats() != (bolt.Stats{}) {
		t.Errorf("nil storage stats = %+v, want zero value", store.Stats())
	}
}

func TestPublishBboltStats(t *testing.T) {
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)

	PublishBboltStats(store)
	PublishBboltStats(store)

	if expvar.Get(MetricBboltStats) == nil {
		t.Fatalf("metric %q not published", MetricBboltStats)
	}
}
