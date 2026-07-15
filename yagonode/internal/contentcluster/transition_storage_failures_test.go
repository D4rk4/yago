package contentcluster

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestFingerprintTransitionStorageRejectsInvalidState(t *testing.T) {
	t.Run("malformed transition", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://malformed-transition.example"
		engine.putRaw(fingerprintBucketName, transitionKey(url), []byte("{"))
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, _, err := index.fingerprints.transition(tx, url)

			return err
		})
		if err == nil {
			t.Fatal("malformed transition succeeded")
		}
	})
	t.Run("invalid transition identity", func(t *testing.T) {
		for _, transition := range []fingerprintTransition{
			{Token: "token", URL: "https://other.example"},
			{URL: "https://identity.example"},
		} {
			index, engine := openFaultIndex(t, Limits{})
			url := "https://identity.example"
			raw, err := json.Marshal(transition)
			if err != nil {
				t.Fatal(err)
			}
			engine.putRaw(fingerprintBucketName, transitionKey(url), raw)
			err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
				_, _, err := index.fingerprints.transition(tx, url)

				return err
			})
			if err == nil {
				t.Fatalf("invalid transition %#v succeeded", transition)
			}
		}
	})
	t.Run("transition encoding", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.fingerprints.putTransition(tx, fingerprintTransition{
				Token: "token",
				URL:   "https://encoding.example",
				Current: fingerprintRecord{
					Quality: math.NaN(),
				},
			})
		})
		if err == nil {
			t.Fatal("unencodable transition succeeded")
		}
	})
	t.Run("transition write", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putFailure = fingerprintBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.fingerprints.putTransition(tx, fingerprintTransition{
				Token: "token",
				URL:   "https://transition-write.example",
			})
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("transition write error = %v", err)
		}
	})
}

func TestFingerprintTransitionDeletionValidatesTokenAndStorage(t *testing.T) {
	url := "https://transition-delete.example"
	t.Run("stale token", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		putRawTransition(t, engine, fingerprintTransition{Token: "current", URL: url})
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			deleted, err := index.fingerprints.deleteTransition(
				tx,
				EvidenceFinalization{url: url, token: "stale"},
			)
			if deleted {
				t.Fatal("stale transition token deleted state")
			}

			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("transition read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putRaw(fingerprintBucketName, transitionKey(url), []byte("{"))
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, err := index.fingerprints.deleteTransition(
				tx,
				EvidenceFinalization{url: url, token: "token"},
			)

			return err
		})
		if err == nil {
			t.Fatal("corrupt transition deletion succeeded")
		}
	})
	t.Run("transition delete", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		putRawTransition(t, engine, fingerprintTransition{Token: "token", URL: url})
		engine.deleteFailure = fingerprintBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, err := index.fingerprints.deleteTransition(
				tx,
				EvidenceFinalization{url: url, token: "token"},
			)

			return err
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("transition delete error = %v", err)
		}
	})
}

func TestFingerprintRecordStorageReportsCodecAndWriteFailures(t *testing.T) {
	t.Run("malformed record", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		key := vault.Key("https://malformed-record.example")
		engine.putRaw(fingerprintBucketName, key, []byte("{"))
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, _, err := index.fingerprints.Get(tx, key)

			return err
		})
		if err == nil {
			t.Fatal("malformed fingerprint succeeded")
		}
	})
	t.Run("record encoding", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.fingerprints.Put(
				tx,
				vault.Key("https://record-encoding.example"),
				fingerprintRecord{Quality: math.NaN()},
			)
		})
		if err == nil {
			t.Fatal("unencodable fingerprint succeeded")
		}
	})
	t.Run("record write", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putFailure = fingerprintBucketName
		err := index.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return index.fingerprints.Put(
				tx,
				vault.Key("https://record-write.example"),
				fingerprintRecord{},
			)
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("fingerprint write error = %v", err)
		}
	})
}

func TestFinalizationBoundaryAndLeaseFailures(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	if err := index.FinalizeEvidenceTransitions(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	url := "https://finalization-boundary.example"
	held, err := index.boundaries.acquireLease(t.Context(), []string{url})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	err = index.FinalizeEvidenceTransitions(
		cancelled,
		[]EvidenceFinalization{{url: url, token: "token"}},
	)
	held.close()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("finalization boundary error = %v", err)
	}
	var nilLease *evidenceLease
	nilLease.close()
	lease, err := index.boundaries.acquireLease(t.Context(), []string{"https://lease.example"})
	if err != nil {
		t.Fatal(err)
	}
	releaseEvidenceLeases([]EvidenceFinalization{
		{projection: nil, urlLease: lease},
		{projection: lease, urlLease: lease},
	})
}

func putRawTransition(
	t *testing.T,
	engine *clusterFaultEngine,
	transition fingerprintTransition,
) {
	t.Helper()
	raw, err := json.Marshal(transition)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(fingerprintBucketName, transitionKey(transition.URL), raw)
}
