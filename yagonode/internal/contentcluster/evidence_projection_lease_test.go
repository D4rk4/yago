package contentcluster

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestEvidenceProjectionLeasePreventsReversedCompletion(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	first := Evidence{
		URL:         "https://lease-a.example/page",
		ContentHash: "shared-lease",
		Text:        "one two three four",
	}
	second := Evidence{
		URL:         "https://lease-b.example/page",
		ContentHash: "shared-lease",
		Text:        "one two three four",
	}
	replaceEvidence(t, index, first)
	replaceEvidence(t, index, second)
	first.CanonicalPreferred = true
	firstReplacement, err := index.ReplaceBatch(t.Context(), []Evidence{first})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	completed := make(chan []EvidenceReplacement, 1)
	failures := make(chan error, 1)
	second.Quality = 2
	go func() {
		close(started)
		replacements, err := index.ReplaceBatch(context.Background(), []Evidence{second})
		if err != nil {
			failures <- err

			return
		}
		completed <- replacements
	}()
	<-started
	select {
	case err := <-failures:
		t.Fatalf("overlapping replacement failed early: %v", err)
	case <-completed:
		t.Fatal("overlapping replacement crossed the projection lease")
	case <-time.After(25 * time.Millisecond):
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(firstReplacement),
	); err != nil {
		t.Fatal(err)
	}
	var secondReplacement []EvidenceReplacement
	select {
	case err := <-failures:
		t.Fatal(err)
	case secondReplacement = <-completed:
	case <-time.After(time.Second):
		t.Fatal("overlapping replacement did not resume")
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(secondReplacement),
	); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceProjectionLeaseWaitHonorsCancellation(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	first := Evidence{
		URL:         "https://cancel-a.example/page",
		ContentHash: "shared-cancel",
		Text:        "one two three four",
	}
	second := Evidence{
		URL:         "https://cancel-b.example/page",
		ContentHash: "shared-cancel",
		Text:        "one two three four",
	}
	replaceEvidence(t, index, first)
	replaceEvidence(t, index, second)
	first.Quality = 1
	held, err := index.ReplaceBatch(t.Context(), []Evidence{first})
	if err != nil {
		t.Fatal(err)
	}
	second.Quality = 2
	cancelled, cancel := context.WithCancel(context.Background())
	completed := make(chan error, 1)
	go func() {
		_, err := index.ReplaceBatch(cancelled, []Evidence{second})
		completed <- err
	}()
	select {
	case err := <-completed:
		t.Fatalf("projection lease wait returned before cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("cancelled projection lease wait succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled projection lease wait did not return")
	}
	index.ReleaseEvidenceTransitions(replacementFinalizations(held))
}

func TestDeletionLeaseSerializesSurvivorReplacement(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	first := Evidence{
		URL:         "https://delete-lease-a.example/page",
		ContentHash: "delete-lease",
		Text:        "one two three four",
	}
	second := Evidence{
		URL:         "https://delete-lease-b.example/page",
		ContentHash: "delete-lease",
		Text:        "one two three four",
	}
	replaceEvidence(t, index, first)
	replaceEvidence(t, index, second)
	deletion, err := index.DeleteTransition(t.Context(), first.URL)
	if err != nil {
		t.Fatal(err)
	}
	second.Quality = 5
	completed := make(chan []EvidenceReplacement, 1)
	failures := make(chan error, 1)
	go func() {
		replacements, err := index.ReplaceBatch(context.Background(), []Evidence{second})
		if err != nil {
			failures <- err

			return
		}
		completed <- replacements
	}()
	select {
	case err := <-failures:
		t.Fatalf("survivor replacement failed early: %v", err)
	case <-completed:
		t.Fatal("survivor replacement crossed the deletion lease")
	case <-time.After(25 * time.Millisecond):
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		[]EvidenceFinalization{deletion.Finalization},
	); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-failures:
		t.Fatal(err)
	case replacements := <-completed:
		if err := index.FinalizeEvidenceTransitions(
			t.Context(),
			replacementFinalizations(replacements),
		); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("survivor replacement did not resume")
	}
}

func TestConcurrentSameURLTransitionsRemainConsistent(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	const replacements = 24
	failures := make(chan error, replacements)
	var ready sync.WaitGroup
	ready.Add(replacements)
	start := make(chan struct{})
	for position := 0; position < replacements; position++ {
		go func(position int) {
			ready.Done()
			<-start
			_, err := index.Replace(context.Background(), Evidence{
				URL:         "https://same-url.example/page",
				ContentHash: fmt.Sprintf("hash-%d", position),
				Text:        fmt.Sprintf("one two three four %d", position),
				Quality:     float64(position),
			})
			failures <- err
		}(position)
	}
	ready.Wait()
	close(start)
	for range replacements {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	assignment, found, err := index.Lookup(t.Context(), "https://same-url.example/page")
	if err != nil || !found {
		t.Fatalf("same URL lookup = %#v/%v/%v", assignment, found, err)
	}
	cluster, found, err := index.Cluster(t.Context(), assignment.ClusterID)
	if err != nil || !found || len(cluster.MemberURLs) != 1 ||
		cluster.MemberURLs[0] != "https://same-url.example/page" {
		t.Fatalf("same URL cluster = %#v/%v/%v", cluster, found, err)
	}
	assertNoTransition(t, index, "https://same-url.example/page")
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		record, found, err := index.fingerprints.Get(
			tx,
			vault.Key("https://same-url.example/page"),
		)
		if err != nil || !found {
			return fmt.Errorf("read final fingerprint: %w", err)
		}
		posting, found, err := index.exactBuckets.Get(tx, vault.Key(record.ContentHash))
		if err != nil || !found || len(posting.URLs) != 1 ||
			posting.URLs[0] != record.URL {
			return fmt.Errorf("final posting is inconsistent")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
