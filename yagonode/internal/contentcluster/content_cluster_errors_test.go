package contentcluster

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOpenReportsEveryRegistrationFailure(t *testing.T) {
	if _, err := Open(nil, Limits{}); err == nil {
		t.Fatal("nil vault opened")
	}
	if _, err := Open(nil, Limits{MaximumTextBytes: -1}); err == nil {
		t.Fatal("invalid limits opened")
	}
	for _, bucket := range []vault.Name{
		fingerprintBucketName,
		clusterBucketName,
		exactBucketName,
		bandBucketName,
	} {
		engine := newClusterFaultEngine()
		v, err := vault.New(engine)
		if err != nil {
			t.Fatalf("new vault for %s: %v", bucket, err)
		}
		engine.provisionFailure = bucket
		if _, err := Open(v, Limits{}); !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("bucket %s error = %v", bucket, err)
		}
	}
}

func TestPrepareEvidencePropagatesWorkCancellation(t *testing.T) {
	limits := DefaultLimits()
	limits.ShingleWords = 1
	normalizationContext := &stagedCancellationContext{
		Context:  context.Background(),
		cancelAt: 2,
	}
	input := Evidence{URL: "https://a.example", ContentHash: "hash", Text: "one"}
	if _, err := prepareEvidence(
		normalizationContext,
		limits,
		input,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("normalization error = %v", err)
	}
	shingleContext := &stagedCancellationContext{
		Context:  context.Background(),
		cancelAt: 5,
	}
	if _, err := prepareEvidence(shingleContext, limits, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("shingle error = %v", err)
	}
}

func TestPublicVaultReadAndWriteFailures(t *testing.T) {
	t.Run("replace update", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		if err := index.vault.Close(); err != nil {
			t.Fatalf("close vault: %v", err)
		}
		_, err := index.Replace(t.Context(), Evidence{
			URL:         "https://a.example",
			ContentHash: "hash",
			Text:        "short",
		})
		if err == nil {
			t.Fatal("replacement on closed vault succeeded")
		}
	})
	t.Run("delete context inside transaction", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		ctx := &stagedCancellationContext{Context: context.Background(), cancelAt: 3}
		if _, err := index.Delete(ctx, "https://a.example"); !errors.Is(err, context.Canceled) {
			t.Fatalf("delete cancellation = %v", err)
		}
	})
	t.Run("delete fingerprint read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putRaw(fingerprintBucketName, vault.Key("https://a.example"), []byte("{"))
		if _, err := index.Delete(t.Context(), "https://a.example"); err == nil {
			t.Fatal("corrupt deleted fingerprint succeeded")
		}
	})
	t.Run("delete detach", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		replaceEvidence(
			t,
			index,
			Evidence{URL: "https://a.example", ContentHash: "hash", Text: "short"},
		)
		engine.putRaw(exactBucketName, vault.Key("hash"), []byte("{"))
		if _, err := index.Delete(t.Context(), "https://a.example"); err == nil {
			t.Fatal("corrupt detach posting succeeded")
		}
	})
	t.Run("delete fingerprint write", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		replaceEvidence(
			t,
			index,
			Evidence{URL: "https://a.example", ContentHash: "hash", Text: "short"},
		)
		engine.deleteFailure = fingerprintBucketName
		if _, err := index.Delete(
			t.Context(),
			"https://a.example",
		); !errors.Is(
			err,
			errInjectedClusterVault,
		) {
			t.Fatalf("fingerprint delete error = %v", err)
		}
	})
}

func TestPublicLookupFailures(t *testing.T) {
	t.Run("lookup fingerprint read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putRaw(fingerprintBucketName, vault.Key("https://a.example"), []byte("{"))
		if _, _, err := index.Lookup(t.Context(), "https://a.example"); err == nil {
			t.Fatal("corrupt lookup fingerprint succeeded")
		}
	})
	t.Run("lookup cluster read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		assignment := replaceEvidence(
			t,
			index,
			Evidence{URL: "https://a.example", ContentHash: "hash", Text: "short"},
		)
		engine.putRaw(clusterBucketName, vault.Key(assignment.ClusterID), []byte("{"))
		if _, _, err := index.Lookup(t.Context(), "https://a.example"); err == nil {
			t.Fatal("corrupt lookup cluster succeeded")
		}
	})
	t.Run("lookup missing cluster", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		assignment := replaceEvidence(
			t,
			index,
			Evidence{URL: "https://a.example", ContentHash: "hash", Text: "short"},
		)
		engine.deleteRaw(clusterBucketName, vault.Key(assignment.ClusterID))
		got, found, err := index.Lookup(t.Context(), "https://a.example")
		if err != nil || found || got != (Assignment{}) {
			t.Fatalf("missing lookup cluster = %#v/%v/%v", got, found, err)
		}
	})
}

func TestReplaceInternalFailures(t *testing.T) {
	valid := Evidence{URL: "https://a.example", ContentHash: "hash", Text: "one two three four"}
	t.Run("existing cluster read and missing", func(t *testing.T) {
		for _, corrupt := range []bool{true, false} {
			index, engine := openFaultIndex(t, Limits{})
			assignment := replaceEvidence(t, index, valid)
			if corrupt {
				engine.putRaw(clusterBucketName, vault.Key(assignment.ClusterID), []byte("{"))
			} else {
				engine.deleteRaw(clusterBucketName, vault.Key(assignment.ClusterID))
			}
			got, err := index.Replace(t.Context(), valid)
			if corrupt && err == nil {
				t.Fatal("corrupt existing cluster succeeded")
			}
			if !corrupt && (err != nil || got.ClusterID != assignment.ClusterID) {
				t.Fatalf("missing existing cluster repair = %#v/%v", got, err)
			}
		}
	})
}

func TestReplacePreviousProjectionFailures(t *testing.T) {
	valid := Evidence{URL: "https://a.example", ContentHash: "hash", Text: "one two three four"}
	t.Run("previous delete", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		replaceEvidence(t, index, valid)
		engine.deleteFailure = fingerprintBucketName
		changed := valid
		changed.ContentHash = "changed"
		if _, err := index.Replace(t.Context(), changed); !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("previous delete error = %v", err)
		}
	})
	t.Run("previous detach", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		replaceEvidence(t, index, valid)
		engine.putRaw(exactBucketName, vault.Key(valid.ContentHash), []byte("{"))
		changed := valid
		changed.ContentHash = "changed"
		if _, err := index.Replace(t.Context(), changed); err == nil {
			t.Fatal("previous detach failure succeeded")
		}
	})
	t.Run("match posting", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putRaw(exactBucketName, vault.Key(valid.ContentHash), []byte("{"))
		if _, err := index.Replace(t.Context(), valid); err == nil {
			t.Fatal("corrupt exact match posting succeeded")
		}
	})
}

func TestDetachFailurePaths(t *testing.T) {
	t.Run("band posting", func(t *testing.T) {
		limits := DefaultLimits()
		limits.ShingleWords = 1
		index, engine := openFaultIndex(t, limits)
		evidence := Evidence{
			URL:         "https://a.example",
			ContentHash: "hash",
			Text:        "one two three four",
		}
		replaceEvidence(t, index, evidence)
		prepared, _ := prepareEvidence(t.Context(), limits, evidence)
		engine.putRaw(bandBucketName, bandKey(0, prepared.Bands[0]), []byte("{"))
		if _, err := index.Delete(t.Context(), evidence.URL); err == nil {
			t.Fatal("corrupt detached band posting succeeded")
		}
	})
}

func TestDetachClusterReadFailures(t *testing.T) {
	t.Run("cluster read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		evidence := Evidence{URL: "https://a.example", ContentHash: "hash", Text: "short"}
		assignment := replaceEvidence(t, index, evidence)
		engine.putRaw(clusterBucketName, vault.Key(assignment.ClusterID), []byte("{"))
		if _, err := index.Delete(t.Context(), evidence.URL); err == nil {
			t.Fatal("corrupt detached cluster succeeded")
		}
	})
	t.Run("cluster missing", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		evidence := Evidence{URL: "https://a.example", ContentHash: "hash", Text: "short"}
		assignment := replaceEvidence(t, index, evidence)
		engine.deleteRaw(clusterBucketName, vault.Key(assignment.ClusterID))
		deleted, err := index.Delete(t.Context(), evidence.URL)
		if err != nil || !deleted {
			t.Fatalf("missing detached cluster deletion = %v/%v", deleted, err)
		}
	})
	t.Run("cluster delete", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		evidence := Evidence{URL: "https://a.example", ContentHash: "hash", Text: "short"}
		replaceEvidence(t, index, evidence)
		engine.deleteFailure = clusterBucketName
		if _, err := index.Delete(
			t.Context(),
			evidence.URL,
		); !errors.Is(
			err,
			errInjectedClusterVault,
		) {
			t.Fatalf("detached cluster delete = %v", err)
		}
	})
}

func TestDetachClusterUpdateFailures(t *testing.T) {
	t.Run("cluster update", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		first := Evidence{URL: "https://a.example", ContentHash: "hash", Text: "short"}
		replaceEvidence(t, index, first)
		replaceEvidence(
			t,
			index,
			Evidence{URL: "https://b.example", ContentHash: "hash", Text: "short"},
		)
		engine.putFailure = clusterBucketName
		if _, err := index.Delete(
			t.Context(),
			first.URL,
		); !errors.Is(
			err,
			errInjectedClusterVault,
		) {
			t.Fatalf("detached cluster update = %v", err)
		}
	})
}

func TestCandidateFingerprintHelperBranches(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	prepared, _ := prepareEvidence(t.Context(), index.limits, Evidence{
		URL:         "https://query.example",
		ContentHash: "query",
		Text:        "one two three four",
	})
	err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if _, found, err := index.candidate(
			tx,
			t.Context(),
			prepared,
			"https://missing.example",
			true,
		); err != nil ||
			found {
			t.Fatalf("missing candidate = %v, %v", found, err)
		}
		self := recordFrom(prepared, "self")
		if err := index.fingerprints.Put(tx, vault.Key(prepared.URL), self); err != nil {
			return fmt.Errorf("store self fingerprint: %w", err)
		}
		if _, found, err := index.candidate(
			tx,
			t.Context(),
			prepared,
			prepared.URL,
			true,
		); err != nil || found {
			t.Fatalf("self candidate = %v, %v", found, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("candidate branches: %v", err)
	}
	engine.putRaw(fingerprintBucketName, vault.Key("https://broken.example"), []byte("{"))
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, candidateErr := index.candidate(
			tx,
			t.Context(),
			prepared,
			"https://broken.example",
			true,
		)

		return candidateErr
	})
	if err == nil {
		t.Fatal("corrupt candidate fingerprint succeeded")
	}
}

func TestCandidateClusterHelperBranches(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	prepared, _ := prepareEvidence(t.Context(), index.limits, Evidence{
		URL:         "https://query.example",
		ContentHash: "query",
		Text:        "one two three four",
	})
	record := fingerprintRecord{URL: "https://candidate.example", ClusterID: "cluster"}
	raw, _ := (jsonCodec[fingerprintRecord]{}).Encode(record)
	engine.putRaw(fingerprintBucketName, vault.Key(record.URL), raw)
	engine.putRaw(clusterBucketName, vault.Key("cluster"), []byte("{"))
	err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, candidateErr := index.candidate(tx, t.Context(), prepared, record.URL, true)

		return candidateErr
	})
	if err == nil {
		t.Fatal("corrupt candidate cluster succeeded")
	}
	engine.deleteRaw(clusterBucketName, vault.Key("cluster"))
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, found, candidateErr := index.candidate(
			tx,
			t.Context(),
			prepared,
			record.URL,
			true,
		)
		if candidateErr != nil || found {
			t.Fatalf("missing candidate cluster = %v, %v", found, candidateErr)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	putRawCluster(t, engine, clusterRecord{ID: record.ClusterID, Members: []string{record.URL}})
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, candidateErr := index.candidate(tx, cancelled, prepared, record.URL, true)

		return candidateErr
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("candidate cluster cancellation = %v", err)
	}
}

func TestCandidateComparisonBranches(t *testing.T) {
	if !betterCandidate(candidateMatch{similarity: 1}, candidateMatch{similarity: 0.5}) {
		t.Fatal("higher similarity did not win")
	}
	if !betterCandidate(
		candidateMatch{similarity: 1, distance: 1},
		candidateMatch{similarity: 1, distance: 2},
	) {
		t.Fatal("lower distance did not win")
	}
	left := candidateMatch{similarity: 1, record: fingerprintRecord{URL: "same", ClusterID: "a"}}
	right := candidateMatch{similarity: 1, record: fingerprintRecord{URL: "same", ClusterID: "b"}}
	if !betterCandidate(left, right) || betterCandidate(right, left) {
		t.Fatal("cluster identity tie break is unstable")
	}
	if betterCandidate(candidateMatch{similarity: 0.5}, candidateMatch{similarity: 1}) {
		t.Fatal("lower similarity won")
	}
	if betterCandidate(
		candidateMatch{similarity: 1, distance: 2},
		candidateMatch{similarity: 1, distance: 1},
	) {
		t.Fatal("higher distance won")
	}
}

func TestPostingLookupBoundsAndSortedSetBranches(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	badKey := vault.Key("bad")
	engine.putRaw(exactBucketName, badKey, []byte("{"))
	err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, postingErr := index.postingURLs(tx, index.exactBuckets, badKey)

		return postingErr
	})
	if err == nil {
		t.Fatal("corrupt posting lookup succeeded")
	}
	oversized := make([]string, index.limits.MaximumBucketMembers+1)
	for position := range oversized {
		oversized[position] = strings.Repeat("x", position+1)
	}
	postingRaw, _ := (jsonCodec[postingRecord]{}).Encode(postingRecord{URLs: oversized})
	engine.putRaw(exactBucketName, badKey, postingRaw)
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, postingErr := index.postingURLs(tx, index.exactBuckets, badKey)

		return postingErr
	})
	if err == nil {
		t.Fatal("oversized posting lookup succeeded")
	}
	if got := insertSorted([]string{"a"}, "a"); len(got) != 1 {
		t.Fatalf("duplicate sorted insert = %v", got)
	}
}

func TestCandidateCollectionLoopBoundsAndErrors(t *testing.T) {
	limits := DefaultLimits()
	limits.MaximumCandidates = 1
	limits.ShingleWords = 1
	index, engine := openFaultIndex(t, limits)
	prepared, _ := prepareEvidence(t.Context(), limits, Evidence{
		URL:         "https://query.example",
		ContentHash: "query",
		Text:        "one two",
	})
	err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, found, candidateErr := index.bestCandidate(
			tx,
			t.Context(),
			prepared,
			[]string{"missing", "ignored"},
			true,
		)
		if candidateErr != nil || found {
			t.Fatalf("bounded candidate verification = %v, %v", found, candidateErr)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, candidateErr := index.bestCandidate(tx, cancelled, prepared, []string{"a"}, true)

		return candidateErr
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("candidate verification cancellation = %v", err)
	}
	engine.putRaw(fingerprintBucketName, vault.Key("broken"), []byte("{"))
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, candidateErr := index.bestCandidate(
			tx,
			t.Context(),
			prepared,
			[]string{"broken"},
			true,
		)

		return candidateErr
	})
	if err == nil {
		t.Fatal("candidate verification read failure succeeded")
	}
}

func TestFindMatchCollectionBoundsAndErrors(t *testing.T) {
	limits := DefaultLimits()
	limits.MaximumCandidates = 1
	limits.ShingleWords = 1
	index, engine := openFaultIndex(t, limits)
	prepared, _ := prepareEvidence(t.Context(), limits, Evidence{
		URL:         "https://query.example",
		ContentHash: "query",
		Text:        "one two",
	})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	engine.deleteRaw(fingerprintBucketName, vault.Key("broken"))
	postingRaw, _ := (jsonCodec[postingRecord]{}).Encode(postingRecord{URLs: []string{"same"}})
	for band, value := range prepared.Bands {
		engine.putRaw(bandBucketName, bandKey(uint8(band), value), postingRaw)
	}
	err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, findErr := index.findMatch(tx, t.Context(), prepared)

		return findErr
	})
	if err != nil {
		t.Fatalf("bounded duplicate candidates: %v", err)
	}
	engine.putRaw(bandBucketName, bandKey(0, prepared.Bands[0]), []byte("{"))
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, findErr := index.findMatch(tx, t.Context(), prepared)

		return findErr
	})
	if err == nil {
		t.Fatal("corrupt band candidate posting succeeded")
	}
	engine.deleteRaw(bandBucketName, bandKey(0, prepared.Bands[0]))
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, findErr := index.findMatch(tx, cancelled, prepared)

		return findErr
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("candidate collection cancellation = %v", err)
	}
	engine.putRaw(exactBucketName, vault.Key(prepared.ContentHash), []byte("{"))
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, findErr := index.findMatch(tx, t.Context(), prepared)

		return findErr
	})
	if err == nil {
		t.Fatal("corrupt exact candidate posting succeeded")
	}
	if math.IsNaN(prepared.Quality) {
		t.Fatal("prepared quality is not finite")
	}
}
