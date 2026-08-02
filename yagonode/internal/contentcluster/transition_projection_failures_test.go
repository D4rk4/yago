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

// TestAttachProjectedClusterFoldsTheAttachedRecordIn pins the equivalence the
// second full pass used to provide: attaching a record must leave the same
// representative the old recompute-from-every-member code chose. The three
// cases are the ones where a fold can diverge from a recompute -- a new cluster
// with no incumbent, an attachment that must lose to the incumbent, and one
// that must beat it.
func TestAttachProjectedClusterFoldsTheAttachedRecordIn(t *testing.T) {
	attach := func(t *testing.T, incumbent *fingerprintRecord, arriving fingerprintRecord) string {
		t.Helper()
		index, engine := openFaultIndex(t, Limits{})
		members := []string(nil)
		if incumbent != nil {
			putRawFingerprint(t, engine, *incumbent)
			members = append(members, incumbent.URL)
			putRawCluster(t, engine, clusterRecord{ID: "cluster", Members: members})
		}
		putRawFingerprint(t, engine, arriving)
		if err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.attachProjectedCluster(tx, t.Context(), arriving)
		}); err != nil {
			t.Fatalf("attach: %v", err)
		}
		var stored clusterRecord
		if err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			record, found, err := index.clusters.Get(tx, vault.Key("cluster"))
			if err != nil || !found {
				t.Fatalf("read stored cluster: found=%v err=%v", found, err)
			}
			stored = record

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		return stored.Representative.URL
	}

	weak := fingerprintRecord{URL: "https://weak.example", ClusterID: "cluster", Quality: 1}
	strong := fingerprintRecord{URL: "https://strong.example", ClusterID: "cluster", Quality: 9}
	// blank carries the scalars betterRepresentative ranks lowest, so it loses
	// to the zero representativeRecord an empty cluster starts with -- every
	// field compares equal until the URL tiebreak, and no URL sorts before "".
	// A first member therefore has to be seeded unconditionally, not merely
	// offered to betterRepresentative, and only a record shaped like this one
	// can tell the two apart.
	blank := fingerprintRecord{URL: "https://blank.example", ClusterID: "cluster"}

	t.Run("first member becomes the representative", func(t *testing.T) {
		if got := attach(t, nil, blank); got != blank.URL {
			t.Fatalf("representative = %q, want %q", got, blank.URL)
		}
	})
	t.Run("a weaker arrival leaves the incumbent", func(t *testing.T) {
		if got := attach(t, &strong, weak); got != strong.URL {
			t.Fatalf("representative = %q, want %q", got, strong.URL)
		}
	})
	t.Run("a stronger arrival takes over", func(t *testing.T) {
		if got := attach(t, &weak, strong); got != strong.URL {
			t.Fatalf("representative = %q, want %q", got, strong.URL)
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
	if postingMatches(fingerprintMatch{}, postingProjection{key: vault.Key{0}}) {
		t.Fatal("empty fingerprint matched a band posting")
	}
	record := fingerprintMatch{HasShingles: true, Fingerprint: 1}
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
