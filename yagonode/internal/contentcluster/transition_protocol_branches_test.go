package contentcluster

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestReplaceBatchHandlesEmptyRepeatedAndInvalidEvidence(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	empty, err := index.ReplaceBatch(t.Context(), nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty replacement batch = %#v/%v", empty, err)
	}
	if _, err := index.ReplaceBatch(t.Context(), []Evidence{
		{URL: "https://valid.example", ContentHash: "hash"},
		{URL: "", ContentHash: "hash"},
	}); err == nil {
		t.Fatal("invalid replacement batch succeeded")
	}
	repeated := []Evidence{
		{URL: "https://repeated.example", ContentHash: "first", Text: "one two three four"},
		{URL: "https://repeated.example", ContentHash: "second", Text: "five six seven eight"},
	}
	replacements, err := index.ReplaceBatch(t.Context(), repeated)
	if err != nil || len(replacements) != 2 {
		t.Fatalf("repeated replacement batch = %#v/%v", replacements, err)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		[]EvidenceFinalization{replacements[1].Finalization},
	); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceBatchReportsRepeatedTransitionFailure(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	engine.putFailure = fingerprintBucketName
	_, err := index.ReplaceBatch(t.Context(), []Evidence{
		{URL: "https://repeated-failure.example", ContentHash: "first"},
		{URL: "https://repeated-failure.example", ContentHash: "second"},
	})
	if !errors.Is(err, errInjectedClusterVault) {
		t.Fatalf("repeated transition error = %v", err)
	}
}

func TestDeleteReportsPendingFinalizationFailure(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	evidence := Evidence{URL: "https://delete-finalization.example", ContentHash: "hash"}
	replaceEvidence(t, index, evidence)
	deletion, err := index.DeleteTransition(t.Context(), evidence.URL)
	if err != nil {
		t.Fatal(err)
	}
	index.ReleaseEvidenceTransitions([]EvidenceFinalization{deletion.Finalization})
	engine.deleteFailure = fingerprintBucketName
	if _, err := index.Delete(t.Context(), evidence.URL); !errors.Is(
		err,
		errInjectedClusterVault,
	) {
		t.Fatalf("pending deletion finalization error = %v", err)
	}
	engine.deleteFailure = ""
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		[]EvidenceFinalization{deletion.Finalization},
	); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceBoundaryIndicesCompactDuplicateIdentities(t *testing.T) {
	indices := evidenceBoundaryIndices([]string{"same", "same"})
	if len(indices) != 1 {
		t.Fatalf("boundary indices = %v", indices)
	}
}

func TestFinalizeEvidenceTransitionsIgnoresNoopWithCollidingLease(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	url := "https://collision-834.example/"
	if evidenceBoundaryIndices([]string{url})[0] != evidenceBoundaryIndices([]string{""})[0] {
		t.Fatal("test identities do not collide")
	}
	lease, err := index.boundaries.acquireLease(t.Context(), []string{url})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err = index.FinalizeEvidenceTransitions(ctx, []EvidenceFinalization{
		{},
		{url: url, token: "active", urlLease: lease},
	})
	if err != nil {
		lease.close()
		t.Fatalf("mixed finalization: %v", err)
	}
}

func TestCandidateRejectsStaleExactPosting(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	prepared, err := prepareEvidence(t.Context(), index.limits, Evidence{
		URL:         "https://candidate-query.example",
		ContentHash: "expected",
	})
	if err != nil {
		t.Fatal(err)
	}
	record := fingerprintRecord{
		URL:         "https://stale-candidate.example",
		ContentHash: "stale",
		ClusterID:   "cluster",
	}
	putRawFingerprint(t, engine, record)
	putRawCluster(t, engine, clusterRecord{ID: record.ClusterID, Members: []string{record.URL}})
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, found, err := index.candidate(tx, t.Context(), prepared, record.URL, true)
		if found {
			t.Fatal("stale exact candidate matched")
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTransitionReadAndProjectionIdentityFailures(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	url := "https://transition-read.example"
	engine.putRaw(fingerprintBucketName, transitionKey(url), []byte("{"))
	if _, err := index.readTransitions(t.Context(), []string{url}); err == nil {
		t.Fatal("corrupt transition scan succeeded")
	}
	if _, err := index.evidenceProjectionIdentities(t.Context(), []string{url}); err == nil {
		t.Fatal("corrupt transition projection identity succeeded")
	}
}

func TestPersistTransitionsReportsContextPredecessorAndWriteFailures(t *testing.T) {
	t.Run("context", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		ctx := &stagedCancellationContext{Context: context.Background(), cancelAt: 3}
		err := index.persistTransitions(ctx, []fingerprintTransition{
			{Token: "token", URL: "https://persist-context.example"},
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("persist context error = %v", err)
		}
	})
	t.Run("predecessor read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://persist-predecessor.example"
		engine.putRaw(fingerprintBucketName, vault.Key(url), []byte("{"))
		err := index.persistTransitions(t.Context(), []fingerprintTransition{
			{Token: "token", URL: url, PreviousFound: true},
		})
		if err == nil {
			t.Fatal("corrupt transition predecessor succeeded")
		}
	})
	t.Run("conflict", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://persist-conflict.example"
		putRawFingerprint(t, engine, fingerprintRecord{URL: url, ContentHash: "current"})
		err := index.persistTransitions(t.Context(), []fingerprintTransition{
			{Token: "token", URL: url, PreviousFound: false},
		})
		if !errors.Is(err, errEvidenceTransitionConflict) {
			t.Fatalf("persist conflict error = %v", err)
		}
	})
	t.Run("transition write", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putFailure = fingerprintBucketName
		err := index.persistTransitions(t.Context(), []fingerprintTransition{
			{Token: "token", URL: "https://persist-write.example"},
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("persist transition write error = %v", err)
		}
	})
}

func TestPublishReplacementMarkersReportEveryStateFailure(t *testing.T) {
	t.Run("marker context", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		ctx := &stagedCancellationContext{Context: context.Background(), cancelAt: 3}
		err := index.publishTransitionMarkers(ctx, []fingerprintTransition{
			{Token: "token", URL: "https://publish-context.example"},
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("publish context error = %v", err)
		}
	})
	t.Run("replacement read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://publish-replacement-read.example"
		engine.putRaw(fingerprintBucketName, vault.Key(url), []byte("{"))
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.publishReplacementMarker(tx, fingerprintTransition{
				URL:          url,
				CurrentFound: true,
			})
		})
		if err == nil {
			t.Fatal("corrupt replacement marker succeeded")
		}
	})
	t.Run("replacement already published", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		record := fingerprintRecord{URL: "https://already-published.example", ContentHash: "hash"}
		putRawFingerprint(t, engine, record)
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.publishReplacementMarker(tx, fingerprintTransition{
				URL:          record.URL,
				Current:      record,
				CurrentFound: true,
			})
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("replacement conflict", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://replacement-conflict.example"
		putRawFingerprint(t, engine, fingerprintRecord{URL: url, ContentHash: "unexpected"})
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.publishReplacementMarker(tx, fingerprintTransition{
				URL:           url,
				PreviousFound: false,
				Current:       fingerprintRecord{URL: url, ContentHash: "current"},
				CurrentFound:  true,
			})
		})
		if !errors.Is(err, errEvidenceTransitionConflict) {
			t.Fatalf("replacement conflict error = %v", err)
		}
	})
	t.Run("replacement write", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://replacement-write.example"
		engine.putFailure = fingerprintBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.publishReplacementMarker(tx, fingerprintTransition{
				URL:          url,
				Current:      fingerprintRecord{URL: url},
				CurrentFound: true,
			})
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("replacement write error = %v", err)
		}
	})
}

func TestPublishDeletionMarkersReportEveryStateFailure(t *testing.T) {
	t.Run("deletion read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://publish-deletion-read.example"
		engine.putRaw(fingerprintBucketName, vault.Key(url), []byte("{"))
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.publishDeletionMarker(tx, fingerprintTransition{URL: url})
		})
		if err == nil {
			t.Fatal("corrupt deletion marker succeeded")
		}
	})
	t.Run("deletion conflict", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://deletion-conflict.example"
		putRawFingerprint(t, engine, fingerprintRecord{URL: url, ContentHash: "unexpected"})
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.publishDeletionMarker(tx, fingerprintTransition{
				URL:           url,
				PreviousFound: true,
				Previous:      fingerprintRecord{URL: url, ContentHash: "previous"},
			})
		})
		if !errors.Is(err, errEvidenceTransitionConflict) {
			t.Fatalf("deletion conflict error = %v", err)
		}
	})
	t.Run("deletion write", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		record := fingerprintRecord{URL: "https://deletion-write.example"}
		putRawFingerprint(t, engine, record)
		engine.deleteFailure = fingerprintBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.publishDeletionMarker(tx, fingerprintTransition{
				URL:           record.URL,
				PreviousFound: true,
				Previous:      record,
			})
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("deletion write error = %v", err)
		}
	})
}
