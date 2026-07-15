package contentcluster

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestClusterProjectionReportsVisibilityFailures(t *testing.T) {
	t.Run("resolution context", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		record := fingerprintRecord{URL: "https://resolution-context.example", ClusterID: "cluster"}
		putRawFingerprint(t, engine, record)
		putRawCluster(t, engine, clusterRecord{ID: "cluster", Members: []string{record.URL}})
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, _, err := index.publishedCluster(tx, cancelled, "cluster")

			return err
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("resolution context error = %v", err)
		}
	})
	t.Run("fingerprint resolution", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://resolution-fingerprint.example"
		putRawCluster(t, engine, clusterRecord{ID: "cluster", Members: []string{url}})
		engine.putRaw(fingerprintBucketName, vault.Key(url), []byte("{"))
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, _, err := index.publishedCluster(tx, t.Context(), "cluster")

			return err
		})
		if err == nil {
			t.Fatal("corrupt projected member succeeded")
		}
	})
	t.Run("projected transition", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://projected-transition.example"
		engine.putRaw(fingerprintBucketName, transitionKey(url), []byte("{"))
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, _, err := index.projectedFingerprint(tx, url)

			return err
		})
		if err == nil {
			t.Fatal("corrupt projected transition succeeded")
		}
	})
}

func TestAttachProjectedClusterEnforcesStateAndStorage(t *testing.T) {
	t.Run("cluster read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		record := fingerprintRecord{URL: "https://attach-read.example", ClusterID: "cluster"}
		engine.putRaw(clusterBucketName, vault.Key(record.ClusterID), []byte("{"))
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.attachProjectedCluster(tx, t.Context(), record)
		})
		if err == nil {
			t.Fatal("corrupt projected cluster succeeded")
		}
	})
	t.Run("member limit", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaximumClusterMembers = 1
		index, engine := openFaultIndex(t, limits)
		first := fingerprintRecord{URL: "https://member-a.example", ClusterID: "cluster"}
		second := fingerprintRecord{URL: "https://member-b.example", ClusterID: "cluster"}
		putRawFingerprint(t, engine, first)
		putRawCluster(t, engine, clusterRecord{ID: "cluster", Members: []string{first.URL}})
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.attachProjectedCluster(tx, t.Context(), second)
		})
		if err == nil {
			t.Fatal("projected cluster exceeded its member limit")
		}
	})
	t.Run("missing representative", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.attachProjectedCluster(tx, t.Context(), fingerprintRecord{
				URL:       "https://missing-representative.example",
				ClusterID: "cluster",
			})
		})
		if err == nil {
			t.Fatal("missing projected representative succeeded")
		}
	})
	t.Run("representative transition", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		record := fingerprintRecord{
			URL:       "https://representative-transition.example",
			ClusterID: "cluster",
		}
		engine.putRaw(fingerprintBucketName, transitionKey(record.URL), []byte("{"))
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.attachProjectedCluster(tx, t.Context(), record)
		})
		if err == nil {
			t.Fatal("corrupt projected representative succeeded")
		}
	})
	t.Run("cluster write", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		record := fingerprintRecord{URL: "https://attach-write.example", ClusterID: "cluster"}
		putRawFingerprint(t, engine, record)
		engine.putFailure = clusterBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.attachProjectedCluster(tx, t.Context(), record)
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("projected cluster write error = %v", err)
		}
	})
}

func TestNormalizeProjectedClusterReportsStorageFailures(t *testing.T) {
	t.Run("empty identity", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.normalizeProjectedCluster(tx, t.Context(), "")
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cluster read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putRaw(clusterBucketName, vault.Key("cluster"), []byte("{"))
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.normalizeProjectedCluster(tx, t.Context(), "cluster")
		})
		if err == nil {
			t.Fatal("corrupt normalized cluster succeeded")
		}
	})
	t.Run("empty cluster delete", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		putRawCluster(t, engine, clusterRecord{ID: "cluster", Members: []string{"missing"}})
		engine.deleteFailure = clusterBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.normalizeProjectedCluster(tx, t.Context(), "cluster")
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("empty cluster delete error = %v", err)
		}
	})
	t.Run("cluster write", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		record := fingerprintRecord{URL: "https://normalized-write.example", ClusterID: "cluster"}
		putRawFingerprint(t, engine, record)
		putRawCluster(t, engine, clusterRecord{ID: "cluster", Members: []string{record.URL}})
		engine.putFailure = clusterBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.normalizeProjectedCluster(tx, t.Context(), "cluster")
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("normalized cluster write error = %v", err)
		}
	})
}

func TestProjectedRepresentativeReportsContextAndFingerprintFailures(t *testing.T) {
	t.Run("context", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, err := index.projectedRepresentative(tx, cancelled, []string{"member"})

			return err
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("representative context error = %v", err)
		}
	})
	t.Run("fingerprint read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://representative-read.example"
		engine.putRaw(fingerprintBucketName, vault.Key(url), []byte("{"))
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, err := index.projectedRepresentative(tx, t.Context(), []string{url})

			return err
		})
		if err == nil {
			t.Fatal("corrupt representative fingerprint succeeded")
		}
	})
}

func TestPostingTransitionReportsContextAndStorageFailures(t *testing.T) {
	record := fingerprintRecord{URL: "https://posting.example", ContentHash: "hash"}
	assertPostingOperationReadAndContextFailures(t, record)
	t.Run("prepare write", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putFailure = exactBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.prepareRecordPostings(tx, t.Context(), record)
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("prepared posting write error = %v", err)
		}
	})
	t.Run("finalize write", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putFailure = exactBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.finalizeRecordPostings(tx, t.Context(), record)
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("finalized posting write error = %v", err)
		}
	})
	t.Run("remove empty delete", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		putRawPosting(t, engine, vault.Key(record.ContentHash), postingRecord{
			URLs: []string{record.URL},
		})
		engine.deleteFailure = exactBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.removeRecordPostings(tx, t.Context(), record)
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("empty posting delete error = %v", err)
		}
	})
	t.Run("remove survivor write", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		survivor := fingerprintRecord{
			URL:         "https://posting-survivor.example",
			ContentHash: record.ContentHash,
		}
		putRawFingerprint(t, engine, survivor)
		putRawPosting(t, engine, vault.Key(record.ContentHash), postingRecord{
			URLs: []string{survivor.URL},
		})
		engine.putFailure = exactBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.removeRecordPostings(tx, t.Context(), record)
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("cleaned posting write error = %v", err)
		}
	})
}

func assertPostingOperationReadAndContextFailures(t *testing.T, record fingerprintRecord) {
	t.Helper()
	for _, operation := range []struct {
		name string
		run  func(*Index, *vault.Txn, context.Context, fingerprintRecord) error
	}{
		{name: "prepare", run: (*Index).prepareRecordPostings},
		{name: "finalize", run: (*Index).finalizeRecordPostings},
		{name: "remove", run: (*Index).removeRecordPostings},
	} {
		t.Run(operation.name+" context", func(t *testing.T) {
			index, _ := openFaultIndex(t, Limits{})
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
				return operation.run(index, tx, cancelled, record)
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s context error = %v", operation.name, err)
			}
		})
		t.Run(operation.name+" posting read", func(t *testing.T) {
			index, engine := openFaultIndex(t, Limits{})
			engine.putRaw(exactBucketName, vault.Key(record.ContentHash), []byte("{"))
			err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
				return operation.run(index, tx, t.Context(), record)
			})
			if err == nil {
				t.Fatalf("%s corrupt posting succeeded", operation.name)
			}
		})
	}
}

func TestVisiblePostingFiltersInvalidAndBoundedMembers(t *testing.T) {
	t.Run("projected fingerprint", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://visible-transition.example"
		putRawPosting(t, engine, vault.Key("hash"), postingRecord{
			URLs: []string{url},
		})
		engine.putRaw(fingerprintBucketName, transitionKey(url), []byte("{"))
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, err := index.visiblePosting(tx, postingProjection{
				collection: index.exactBuckets,
				key:        vault.Key("hash"),
				exact:      true,
			})

			return err
		})
		if err == nil {
			t.Fatal("corrupt visible posting member succeeded")
		}
	})
	t.Run("member bound", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaximumBucketMembers = 1
		index, engine := openFaultIndex(t, limits)
		urls := []string{"https://visible-a.example", "https://visible-b.example"}
		for _, url := range urls {
			putRawFingerprint(t, engine, fingerprintRecord{URL: url, ContentHash: "hash"})
		}
		putRawPosting(t, engine, vault.Key("hash"), postingRecord{URLs: urls})
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			posting, err := index.visiblePosting(tx, postingProjection{
				collection: index.exactBuckets,
				key:        vault.Key("hash"),
				exact:      true,
			})
			if len(posting.URLs) != 1 {
				t.Fatalf("visible posting members = %v", posting.URLs)
			}

			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if postingMatches(fingerprintRecord{}, postingProjection{key: vault.Key{0}}) {
		t.Fatal("empty fingerprint matched a band posting")
	}
	record := fingerprintRecord{Shingles: []uint64{1}, Fingerprint: 1}
	if postingMatches(record, postingProjection{key: vault.Key{0}}) {
		t.Fatal("malformed band posting matched")
	}
	if postingMatches(record, postingProjection{key: vault.Key{255, 1}}) {
		t.Fatal("out-of-range band posting matched")
	}
}

func putRawFingerprint(t *testing.T, engine *clusterFaultEngine, record fingerprintRecord) {
	t.Helper()
	raw, err := (jsonCodec[fingerprintRecord]{}).Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(fingerprintBucketName, vault.Key(record.URL), raw)
}

func putRawCluster(t *testing.T, engine *clusterFaultEngine, record clusterRecord) {
	t.Helper()
	raw, err := (jsonCodec[clusterRecord]{}).Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(clusterBucketName, vault.Key(record.ID), raw)
}

func putRawPosting(
	t *testing.T,
	engine *clusterFaultEngine,
	key vault.Key,
	record postingRecord,
) {
	t.Helper()
	raw, err := (jsonCodec[postingRecord]{}).Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(exactBucketName, key, raw)
}
