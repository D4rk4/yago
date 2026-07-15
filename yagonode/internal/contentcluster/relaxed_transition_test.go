package contentcluster

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestReplaceRepairsEveryRelaxedCommitBoundary(t *testing.T) {
	for failedUpdate := 1; failedUpdate <= 5; failedUpdate++ {
		t.Run(fmt.Sprintf("update-%d", failedUpdate), func(t *testing.T) {
			index, engine := openFaultIndex(t, Limits{})
			evidence := Evidence{
				URL:         "https://replace.example/page",
				ContentHash: "replace-hash",
				Text:        "one two three four five six",
			}
			engine.failRelaxedUpdateAfter(failedUpdate, 1)
			if _, err := index.Replace(t.Context(), evidence); !errors.Is(
				err,
				errInjectedRelaxedClusterCommit,
			) {
				t.Fatalf("replace error = %v", err)
			}
			reopened := reopenFaultIndex(t, engine)
			if _, err := reopened.Replace(t.Context(), evidence); err != nil {
				t.Fatalf("repair replace: %v", err)
			}
			assertPublishedEvidence(t, reopened, evidence)
			assertNoTransition(t, reopened, evidence.URL)
		})
	}
}

func TestReplaceBatchRepairsEveryRelaxedCommitBoundary(t *testing.T) {
	for failedUpdate := 1; failedUpdate <= 5; failedUpdate++ {
		t.Run(fmt.Sprintf("update-%d", failedUpdate), func(t *testing.T) {
			assertReplaceBatchRelaxedBoundary(t, failedUpdate)
		})
	}
}

func assertReplaceBatchRelaxedBoundary(t *testing.T, failedUpdate int) {
	t.Helper()
	index, engine := openFaultIndex(t, Limits{})
	evidence := []Evidence{
		{URL: "https://batch-a.example", ContentHash: "shared", Text: "alpha beta"},
		{URL: "https://batch-b.example", ContentHash: "shared", Text: "alpha beta"},
		{URL: "https://batch-c.example", ContentHash: "other", Text: "gamma delta"},
	}
	engine.failRelaxedUpdateAfter(failedUpdate, 1)
	replacements, err := index.ReplaceBatch(t.Context(), evidence)
	interrupted := replacementBatchInterruption{
		index:        index,
		engine:       engine,
		evidence:     evidence,
		replacements: replacements,
		err:          err,
		failedUpdate: failedUpdate,
	}
	interrupted = repairInterruptedReplacementBatch(t, interrupted)
	index = finalizeInterruptedReplacementBatch(
		t,
		interrupted,
	)
	for _, item := range evidence {
		assertPublishedEvidence(t, index, item)
		assertNoTransition(t, index, item.URL)
	}
}

type replacementBatchInterruption struct {
	index        *Index
	engine       *clusterFaultEngine
	evidence     []Evidence
	replacements []EvidenceReplacement
	err          error
	failedUpdate int
}

func repairInterruptedReplacementBatch(
	t *testing.T,
	interrupted replacementBatchInterruption,
) replacementBatchInterruption {
	t.Helper()
	if interrupted.failedUpdate > 4 {
		if interrupted.err != nil {
			t.Fatalf("replace batch: %v", interrupted.err)
		}

		return interrupted
	}
	if !errors.Is(interrupted.err, errInjectedRelaxedClusterCommit) {
		t.Fatalf("batch error = %v", interrupted.err)
	}
	interrupted.index = reopenFaultIndex(t, interrupted.engine)
	interrupted.replacements, interrupted.err = interrupted.index.ReplaceBatch(
		t.Context(),
		interrupted.evidence,
	)
	if interrupted.err != nil {
		t.Fatalf("repair batch: %v", interrupted.err)
	}

	return interrupted
}

func finalizeInterruptedReplacementBatch(
	t *testing.T,
	interrupted replacementBatchInterruption,
) *Index {
	t.Helper()
	finalizations := replacementFinalizations(interrupted.replacements)
	err := interrupted.index.FinalizeEvidenceTransitions(t.Context(), finalizations)
	if interrupted.failedUpdate != 5 {
		if err != nil {
			t.Fatalf("finalize batch: %v", err)
		}

		return interrupted.index
	}
	if !errors.Is(err, errInjectedRelaxedClusterCommit) {
		t.Fatalf("batch finalization error = %v", err)
	}
	interrupted.index.ReleaseEvidenceTransitions(finalizations)
	interrupted.index = reopenFaultIndex(t, interrupted.engine)
	interrupted.replacements, err = interrupted.index.ReplaceBatch(
		t.Context(),
		interrupted.evidence,
	)
	if err != nil {
		t.Fatalf("replay finalized batch: %v", err)
	}
	if err := interrupted.index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(interrupted.replacements),
	); err != nil {
		t.Fatalf("complete replayed batch: %v", err)
	}

	return interrupted.index
}

func TestDeleteTransitionRepairsEveryRelaxedCommitBoundary(t *testing.T) {
	for failedUpdate := 1; failedUpdate <= 4; failedUpdate++ {
		t.Run(fmt.Sprintf("update-%d", failedUpdate), func(t *testing.T) {
			assertDeleteRelaxedBoundary(t, failedUpdate)
		})
	}
}

func assertDeleteRelaxedBoundary(t *testing.T, failedUpdate int) {
	t.Helper()
	index, engine := openFaultIndex(t, Limits{})
	evidence := Evidence{
		URL:         "https://delete.example/page",
		ContentHash: "delete-hash",
		Text:        "one two three four five six",
	}
	assignment := replaceEvidence(t, index, evidence)
	engine.failRelaxedUpdateAfter(failedUpdate, 1)
	deletion, err := index.DeleteTransition(t.Context(), evidence.URL)
	interrupted := deletionInterruption{
		index:        index,
		engine:       engine,
		url:          evidence.URL,
		deletion:     deletion,
		err:          err,
		failedUpdate: failedUpdate,
	}
	interrupted = repairInterruptedDeletion(t, interrupted)
	index = interrupted.index
	deletion = interrupted.deletion
	if !deletion.Deleted || deletion.Previous != assignment {
		t.Fatalf("deletion = %#v, want previous %#v", deletion, assignment)
	}
	interrupted.index = index
	interrupted.deletion = deletion
	index = finalizeInterruptedDeletion(t, interrupted)
	if _, found, err := index.Lookup(t.Context(), evidence.URL); err != nil || found {
		t.Fatalf("deleted lookup = %v/%v", found, err)
	}
	assertNoTransition(t, index, evidence.URL)
}

type deletionInterruption struct {
	index        *Index
	engine       *clusterFaultEngine
	url          string
	deletion     EvidenceDeletion
	err          error
	failedUpdate int
}

func repairInterruptedDeletion(
	t *testing.T,
	interrupted deletionInterruption,
) deletionInterruption {
	t.Helper()
	if interrupted.failedUpdate > 3 {
		if interrupted.err != nil {
			t.Fatalf("delete transition: %v", interrupted.err)
		}

		return interrupted
	}
	if !errors.Is(interrupted.err, errInjectedRelaxedClusterCommit) {
		t.Fatalf("delete transition error = %v", interrupted.err)
	}
	interrupted.index = reopenFaultIndex(t, interrupted.engine)
	interrupted.deletion, interrupted.err = interrupted.index.DeleteTransition(
		t.Context(),
		interrupted.url,
	)
	if interrupted.err != nil {
		t.Fatalf("repair delete transition: %v", interrupted.err)
	}

	return interrupted
}

func finalizeInterruptedDeletion(
	t *testing.T,
	interrupted deletionInterruption,
) *Index {
	t.Helper()
	finalizations := []EvidenceFinalization{interrupted.deletion.Finalization}
	err := interrupted.index.FinalizeEvidenceTransitions(t.Context(), finalizations)
	if interrupted.failedUpdate != 4 {
		if err != nil {
			t.Fatalf("finalize delete: %v", err)
		}

		return interrupted.index
	}
	if !errors.Is(err, errInjectedRelaxedClusterCommit) {
		t.Fatalf("delete finalization error = %v", err)
	}
	interrupted.index.ReleaseEvidenceTransitions(finalizations)
	interrupted.index = reopenFaultIndex(t, interrupted.engine)
	interrupted.deletion, err = interrupted.index.DeleteTransition(
		t.Context(),
		interrupted.url,
	)
	if err != nil {
		t.Fatalf("replay finalized delete: %v", err)
	}
	if interrupted.deletion.Finalization.token == "" {
		return interrupted.index
	}
	if err := interrupted.index.FinalizeEvidenceTransitions(
		t.Context(),
		[]EvidenceFinalization{interrupted.deletion.Finalization},
	); err != nil {
		t.Fatalf("complete replayed delete: %v", err)
	}

	return interrupted.index
}

func TestPendingReplacementAndDeletionReplayUntilFinalized(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	evidence := Evidence{
		URL:         "https://replay.example/page",
		ContentHash: "replay-hash",
		Text:        "one two three four",
	}
	first, err := index.ReplaceBatch(t.Context(), []Evidence{evidence})
	if err != nil || first[0].Replay {
		t.Fatalf("first replacement = %#v/%v", first, err)
	}
	index.ReleaseEvidenceTransitions(replacementFinalizations(first))
	second, err := index.ReplaceBatch(t.Context(), []Evidence{evidence})
	if err != nil || !second[0].Replay ||
		second[0].Finalization.token != first[0].Finalization.token {
		t.Fatalf("replayed replacement = %#v/%v", second, err)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(second),
	); err != nil {
		t.Fatal(err)
	}
	deletion, err := index.DeleteTransition(t.Context(), evidence.URL)
	if err != nil || deletion.Replay {
		t.Fatalf("first deletion = %#v/%v", deletion, err)
	}
	index.ReleaseEvidenceTransitions([]EvidenceFinalization{deletion.Finalization})
	replayed, err := index.DeleteTransition(t.Context(), evidence.URL)
	if err != nil || !replayed.Replay ||
		replayed.Finalization.token != deletion.Finalization.token {
		t.Fatalf("replayed deletion = %#v/%v", replayed, err)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		[]EvidenceFinalization{replayed.Finalization},
	); err != nil {
		t.Fatal(err)
	}
}

func TestSupersededTransitionRetainsEveryAffectedCluster(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	first := Evidence{
		URL:         "https://affected-a.example/page",
		ContentHash: "shared-affected",
		Text:        "one two three four",
	}
	second := Evidence{
		URL:         "https://affected-b.example/page",
		ContentHash: "shared-affected",
		Text:        "one two three four",
	}
	original := replaceEvidence(t, index, first)
	replaceEvidence(t, index, second)
	first.ContentHash = "intermediate"
	first.Text = "five six seven eight"
	intermediate, err := index.ReplaceBatch(t.Context(), []Evidence{first})
	if err != nil {
		t.Fatal(err)
	}
	intermediateCluster := intermediate[0].Current.ClusterID
	index.ReleaseEvidenceTransitions(replacementFinalizations(intermediate))
	first.ContentHash = "final"
	first.Text = "nine ten eleven twelve"
	final, err := index.ReplaceBatch(t.Context(), []Evidence{first})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{original.ClusterID, intermediateCluster, final[0].Current.ClusterID}
	slices.Sort(want)
	want = slices.Compact(want)
	if !slices.Equal(final[0].AffectedClusterIDs, want) || !final[0].Replay {
		t.Fatalf("affected clusters = %v, want %v", final[0].AffectedClusterIDs, want)
	}
	index.ReleaseEvidenceTransitions(replacementFinalizations(final))
	replayed, err := index.ReplaceBatch(t.Context(), []Evidence{first})
	if err != nil || !replayed[0].Replay ||
		!slices.Equal(replayed[0].AffectedClusterIDs, want) {
		t.Fatalf("replayed affected clusters = %#v/%v", replayed, err)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(replayed),
	); err != nil {
		t.Fatal(err)
	}
}

func TestContentClusterCollectionsDoNotWriteLengthCounters(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	evidence := Evidence{
		URL:         "https://uncounted.example/page",
		ContentHash: "uncounted-hash",
		Text:        "one two three four",
	}
	if _, err := index.Replace(t.Context(), evidence); err != nil {
		t.Fatal(err)
	}
	engine.mu.RLock()
	defer engine.mu.RUnlock()
	for _, name := range []vault.Name{
		fingerprintBucketName,
		clusterBucketName,
		exactBucketName,
		bandBucketName,
	} {
		if _, found := engine.buckets[vault.Name("__lengths__")][string(name)]; found {
			t.Fatalf("length counter exists for %s", name)
		}
	}
}

func replacementFinalizations(replacements []EvidenceReplacement) []EvidenceFinalization {
	finalizations := make([]EvidenceFinalization, 0, len(replacements))
	for _, replacement := range replacements {
		if replacement.Finalization.token != "" {
			finalizations = append(finalizations, replacement.Finalization)
		}
	}

	return finalizations
}

func assertPublishedEvidence(t *testing.T, index *Index, evidence Evidence) {
	t.Helper()
	assignment, found, err := index.Lookup(t.Context(), evidence.URL)
	if err != nil || !found {
		t.Fatalf("lookup %q = %#v/%v/%v", evidence.URL, assignment, found, err)
	}
	cluster, found, err := index.Cluster(t.Context(), assignment.ClusterID)
	if err != nil || !found || !slices.Contains(cluster.MemberURLs, evidence.URL) {
		t.Fatalf("cluster %q = %#v/%v/%v", evidence.URL, cluster, found, err)
	}
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		posting, found, err := index.exactBuckets.Get(tx, vault.Key(evidence.ContentHash))
		if err != nil {
			return fmt.Errorf("read exact evidence posting: %w", err)
		}
		if !found || !slices.Contains(posting.URLs, evidence.URL) {
			return fmt.Errorf("exact posting does not contain %q", evidence.URL)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertNoTransition(t *testing.T, index *Index, url string) {
	t.Helper()
	err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, found, err := index.fingerprints.transition(tx, url)
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("transition remains for %q", url)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
