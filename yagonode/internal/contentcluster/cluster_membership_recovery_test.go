package contentcluster

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestReplacementRejectsEmptyFingerprintClusterIdentity(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	evidence := Evidence{
		URL:         "https://invalid-cluster-identity.example/page",
		ContentHash: "retained-content",
		Text:        "one two three four",
	}
	prepared, err := prepareEvidence(t.Context(), index.limits, evidence)
	if err != nil {
		t.Fatal(err)
	}
	putRawFingerprint(t, engine, recordFrom(prepared, ""))
	if _, err := index.Replace(t.Context(), evidence); !errors.Is(
		err,
		errInvalidFingerprintClusterIdentity,
	) {
		t.Fatalf("empty cluster identity replacement error = %v", err)
	}
}

func TestReplacementRebuildsMissingClusterMembership(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	evidence := evidenceWithSeparateFingerprintAndClusterShards(t, engine)
	original := replaceEvidence(t, index, evidence)
	engine.loseShard(engine.route(clusterBucketName, vault.Key(original.ClusterID)))
	evidence.CanonicalPreferred = true
	evidence.Quality = 2
	replacements, err := index.ReplaceBatch(t.Context(), []Evidence{evidence})
	if err != nil || len(replacements) != 1 {
		t.Fatalf("missing membership replacement = %#v/%v", replacements, err)
	}
	replacement := replacements[0]
	if replacement.PreviousFound || replacement.Previous != (Assignment{}) ||
		replacement.Current.ClusterID != original.ClusterID ||
		replacement.Finalization.token == "" {
		t.Fatalf("missing membership replacement = %#v", replacement)
	}
	index.ReleaseEvidenceTransitions(replacementFinalizations(replacements))
	replayed, err := index.ReplaceBatch(t.Context(), []Evidence{evidence})
	if err != nil || len(replayed) != 1 || !replayed[0].Replay ||
		replayed[0].PreviousFound || replayed[0].Previous != (Assignment{}) ||
		replayed[0].Finalization.token != replacement.Finalization.token {
		t.Fatalf("missing membership replay = %#v/%v", replayed, err)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(replayed),
	); err != nil {
		t.Fatal(err)
	}
	if got := lookupAssignment(t, index, evidence.URL); got != replayed[0].Current {
		t.Fatalf("recovered assignment = %#v, want %#v", got, replayed[0].Current)
	}
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		record, found, readErr := index.fingerprints.Get(tx, vault.Key(evidence.URL))
		if readErr != nil {
			return readErr
		}
		if !found || !record.CanonicalPreferred || record.Quality != evidence.Quality {
			t.Fatalf("recovered fingerprint = %#v/%v", record, found)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func evidenceWithSeparateFingerprintAndClusterShards(
	t *testing.T,
	engine *clusterFaultEngine,
) Evidence {
	t.Helper()
	for position := 0; position < 256; position++ {
		evidence := Evidence{
			URL:         fmt.Sprintf("https://shard-loss-%d.example/page", position),
			ContentHash: fmt.Sprintf("retained-content-%d", position),
			Text:        "one two three four",
			Quality:     1,
		}
		clusterID := stableClusterID(evidence.URL, evidence.ContentHash)
		fingerprintShard := engine.route(fingerprintBucketName, vault.Key(evidence.URL))
		clusterShard := engine.route(clusterBucketName, vault.Key(clusterID))
		if fingerprintShard != clusterShard {
			return evidence
		}
	}
	t.Fatal("no separated fingerprint and cluster shard identity")

	return Evidence{}
}

func TestValidPreviousAssignmentSurvivesTransitionReplay(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	evidence := Evidence{
		URL:         "https://valid-replay.example/page",
		ContentHash: "before",
		Text:        "one two three four",
	}
	previous := replaceEvidence(t, index, evidence)
	evidence.ContentHash = "after"
	evidence.Text = "five six seven eight"
	first, err := index.ReplaceBatch(t.Context(), []Evidence{evidence})
	if err != nil || len(first) != 1 || !first[0].PreviousFound ||
		first[0].Previous != previous {
		t.Fatalf("valid replacement = %#v/%v", first, err)
	}
	index.ReleaseEvidenceTransitions(replacementFinalizations(first))
	replayed, err := index.ReplaceBatch(t.Context(), []Evidence{evidence})
	if err != nil || len(replayed) != 1 || !replayed[0].Replay ||
		!replayed[0].PreviousFound || replayed[0].Previous != previous {
		t.Fatalf("valid replacement replay = %#v/%v", replayed, err)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(replayed),
	); err != nil {
		t.Fatal(err)
	}
}

func TestChangedOrphanEvidenceUsesNormalReplacementPlanning(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	evidence := Evidence{
		URL:         "https://changed-orphan.example/page",
		ContentHash: "before",
		Text:        "one two three four",
	}
	previous := replaceEvidence(t, index, evidence)
	engine.deleteRaw(clusterBucketName, vault.Key(previous.ClusterID))
	evidence.ContentHash = "after"
	evidence.Text = "five six seven eight"
	replacements, err := index.ReplaceBatch(t.Context(), []Evidence{evidence})
	if err != nil || len(replacements) != 1 {
		t.Fatalf("changed orphan replacement = %#v/%v", replacements, err)
	}
	replacement := replacements[0]
	wantAffected := []string{previous.ClusterID, replacement.Current.ClusterID}
	slices.Sort(wantAffected)
	if replacement.PreviousFound || replacement.Previous != (Assignment{}) ||
		replacement.Current.ClusterID == previous.ClusterID ||
		!slices.Equal(replacement.AffectedClusterIDs, wantAffected) {
		t.Fatalf("changed orphan replacement = %#v", replacement)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(replacements),
	); err != nil {
		t.Fatal(err)
	}
	if got := lookupAssignment(t, index, evidence.URL); got != replacement.Current {
		t.Fatalf("changed orphan assignment = %#v, want %#v", got, replacement.Current)
	}
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		oldPosting, oldFound, readErr := index.exactBuckets.Get(tx, vault.Key("before"))
		if readErr != nil {
			return fmt.Errorf("read previous orphan posting: %w", readErr)
		}
		currentPosting, currentFound, readErr := index.exactBuckets.Get(
			tx,
			vault.Key(evidence.ContentHash),
		)
		if readErr != nil {
			return fmt.Errorf("read current orphan posting: %w", readErr)
		}
		if oldFound || len(oldPosting.URLs) != 0 || !currentFound ||
			!slices.Equal(currentPosting.URLs, []string{evidence.URL}) {
			t.Fatalf(
				"orphan postings = old %#v/%v current %#v/%v",
				oldPosting,
				oldFound,
				currentPosting,
				currentFound,
			)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOmittedClusterMembershipIsAbsentUntilReplacement(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	first := Evidence{
		URL:         "https://published-member.example/page",
		ContentHash: "shared-content",
		Text:        "one two three four",
	}
	second := Evidence{
		URL:         "https://omitted-member.example/page",
		ContentHash: first.ContentHash,
		Text:        first.Text,
	}
	assignment := replaceEvidence(t, index, first)
	replaceEvidence(t, index, second)
	putRawCluster(t, engine, clusterRecord{
		ID:      assignment.ClusterID,
		Members: []string{first.URL},
	})
	got, found, err := index.Lookup(t.Context(), second.URL)
	if err != nil || found || got != (Assignment{}) {
		t.Fatalf("omitted lookup = %#v/%v/%v", got, found, err)
	}
	replacements, err := index.ReplaceBatch(t.Context(), []Evidence{second})
	if err != nil || len(replacements) != 1 || replacements[0].PreviousFound {
		t.Fatalf("omitted membership replacement = %#v/%v", replacements, err)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(replacements),
	); err != nil {
		t.Fatal(err)
	}
	cluster, found, err := index.Cluster(t.Context(), assignment.ClusterID)
	if err != nil || !found || !slices.Equal(
		cluster.MemberURLs,
		[]string{second.URL, first.URL},
	) {
		t.Fatalf("recovered cluster = %#v/%v/%v", cluster, found, err)
	}
}

func TestFullClusterOmittedMembershipRehomesWithoutOverflow(t *testing.T) {
	limits := DefaultLimits()
	limits.MaximumClusterMembers = 2
	index, engine := openFaultIndex(t, limits)
	contentHash := "full-shared-content"
	text := "one two three four"
	first := replaceEvidence(t, index, Evidence{
		URL: "https://full-a.example/page", ContentHash: contentHash, Text: text,
	})
	replaceEvidence(t, index, Evidence{
		URL: "https://full-b.example/page", ContentHash: contentHash, Text: text,
	})
	orphan := Evidence{
		URL: "https://full-orphan.example/page", ContentHash: contentHash, Text: text,
	}
	prepared, err := prepareEvidence(t.Context(), limits, orphan)
	if err != nil {
		t.Fatal(err)
	}
	putRawFingerprint(t, engine, recordFrom(prepared, first.ClusterID))
	replacements, err := index.ReplaceBatch(t.Context(), []Evidence{orphan})
	if err != nil || len(replacements) != 1 {
		t.Fatalf("full-cluster recovery = %#v/%v", replacements, err)
	}
	wantClusterID := stableClusterID(
		orphan.URL,
		orphan.ContentHash+"\x00cluster-membership-recovery\x00"+first.ClusterID,
	)
	if replacements[0].PreviousFound || replacements[0].Current.ClusterID != wantClusterID ||
		replacements[0].Current.ClusterID == first.ClusterID {
		t.Fatalf("full-cluster recovery = %#v", replacements[0])
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(replacements),
	); err != nil {
		t.Fatal(err)
	}
	oldCluster, found, err := index.Cluster(t.Context(), first.ClusterID)
	if err != nil || !found || len(oldCluster.MemberURLs) != limits.MaximumClusterMembers {
		t.Fatalf("original full cluster = %#v/%v/%v", oldCluster, found, err)
	}
	newCluster, found, err := index.Cluster(t.Context(), wantClusterID)
	if err != nil || !found || !slices.Equal(newCluster.MemberURLs, []string{orphan.URL}) {
		t.Fatalf("recovered singleton = %#v/%v/%v", newCluster, found, err)
	}
}

func TestRecoveryCapacityIncludesEarlierBatchPlans(t *testing.T) {
	limits := DefaultLimits()
	limits.MaximumClusterMembers = 2
	index, engine := openFaultIndex(t, limits)
	contentHash := "planned-recovery-content"
	text := "one two three four"
	survivor := Evidence{
		URL: "https://planned-survivor.example/page", ContentHash: contentHash, Text: text,
	}
	original := replaceEvidence(t, index, survivor)
	orphans := []Evidence{
		{
			URL: "https://planned-orphan-a.example/page", ContentHash: contentHash, Text: text,
		},
		{
			URL: "https://planned-orphan-b.example/page", ContentHash: contentHash, Text: text,
		},
	}
	for _, orphan := range orphans {
		prepared, err := prepareEvidence(t.Context(), limits, orphan)
		if err != nil {
			t.Fatal(err)
		}
		putRawFingerprint(t, engine, recordFrom(prepared, original.ClusterID))
	}
	replacements, err := index.ReplaceBatch(t.Context(), orphans)
	if err != nil || len(replacements) != len(orphans) {
		t.Fatalf("planned recovery = %#v/%v", replacements, err)
	}
	wantRehomed := stableClusterID(
		orphans[1].URL,
		orphans[1].ContentHash+"\x00cluster-membership-recovery\x00"+original.ClusterID,
	)
	if replacements[0].Current.ClusterID != original.ClusterID ||
		replacements[1].Current.ClusterID != wantRehomed {
		t.Fatalf("planned recovery assignments = %#v", replacements)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(replacements),
	); err != nil {
		t.Fatal(err)
	}
	originalCluster, found, err := index.Cluster(t.Context(), original.ClusterID)
	if err != nil || !found || !slices.Equal(
		originalCluster.MemberURLs,
		[]string{orphans[0].URL, survivor.URL},
	) {
		t.Fatalf("planned original cluster = %#v/%v/%v", originalCluster, found, err)
	}
	rehomedCluster, found, err := index.Cluster(t.Context(), wantRehomed)
	if err != nil || !found || !slices.Equal(
		rehomedCluster.MemberURLs,
		[]string{orphans[1].URL},
	) {
		t.Fatalf("planned rehomed cluster = %#v/%v/%v", rehomedCluster, found, err)
	}
}

func TestRecoveryCapacityRemovesEarlierBatchDepartures(t *testing.T) {
	limits := DefaultLimits()
	limits.MaximumClusterMembers = 2
	index, engine := openFaultIndex(t, limits)
	contentHash := "departure-content"
	text := "one two three four"
	survivor := Evidence{
		URL: "https://departure-survivor.example/page", ContentHash: contentHash, Text: text,
	}
	mover := Evidence{
		URL: "https://departure-mover.example/page", ContentHash: contentHash, Text: text,
	}
	original := replaceEvidence(t, index, survivor)
	if got := replaceEvidence(t, index, mover); got.ClusterID != original.ClusterID {
		t.Fatalf("mover cluster = %#v, want %#v", got, original)
	}
	orphan := Evidence{
		URL: "https://departure-orphan.example/page", ContentHash: contentHash, Text: text,
	}
	prepared, err := prepareEvidence(t.Context(), limits, orphan)
	if err != nil {
		t.Fatal(err)
	}
	putRawFingerprint(t, engine, recordFrom(prepared, original.ClusterID))
	changedMover := mover
	changedMover.ContentHash = "departed-content"
	changedMover.Text = "five six seven eight"
	replacements, err := index.ReplaceBatch(
		t.Context(),
		[]Evidence{changedMover, orphan},
	)
	if err != nil || len(replacements) != 2 {
		t.Fatalf("departure recovery = %#v/%v", replacements, err)
	}
	if replacements[0].Current.ClusterID == original.ClusterID ||
		replacements[1].Current.ClusterID != original.ClusterID {
		t.Fatalf("departure recovery assignments = %#v", replacements)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(replacements),
	); err != nil {
		t.Fatal(err)
	}
	cluster, found, err := index.Cluster(t.Context(), original.ClusterID)
	wantMembers := []string{orphan.URL, survivor.URL}
	slices.Sort(wantMembers)
	if err != nil || !found || !slices.Equal(cluster.MemberURLs, wantMembers) {
		t.Fatalf("departure recovery cluster = %#v/%v/%v", cluster, found, err)
	}
}

func TestOrphanCandidateIsNotSelected(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	candidate := fingerprintRecord{
		URL:         "https://orphan-candidate.example/page",
		ContentHash: "candidate-content",
		ClusterID:   "candidate-cluster",
	}
	survivor := fingerprintRecord{
		URL:         "https://candidate-survivor.example/page",
		ContentHash: "different-content",
		ClusterID:   candidate.ClusterID,
	}
	putRawFingerprint(t, engine, candidate)
	putRawFingerprint(t, engine, survivor)
	putRawCluster(t, engine, clusterRecord{
		ID:      candidate.ClusterID,
		Members: []string{survivor.URL},
	})
	putRawPosting(
		t,
		engine,
		vault.Key(candidate.ContentHash),
		postingRecord{URLs: []string{candidate.URL}},
	)
	query := Evidence{
		URL:         "https://candidate-query.example/page",
		ContentHash: candidate.ContentHash,
	}
	assignment := replaceEvidence(t, index, query)
	if assignment.ClusterID != stableClusterID(query.URL, query.ContentHash) ||
		assignment.ClusterID == candidate.ClusterID {
		t.Fatalf("orphan candidate assignment = %#v", assignment)
	}
}

func TestOrphanDeletionRemovesDurableResidue(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	first := Evidence{
		URL:         "https://deletion-survivor.example/page",
		ContentHash: "deletion-content",
		Text:        "one two three four",
	}
	orphan := Evidence{
		URL:         "https://deletion-orphan.example/page",
		ContentHash: first.ContentHash,
		Text:        first.Text,
	}
	assignment := replaceEvidence(t, index, first)
	replaceEvidence(t, index, orphan)
	putRawCluster(t, engine, clusterRecord{
		ID:      assignment.ClusterID,
		Members: []string{first.URL},
	})
	deletion, err := index.DeleteTransition(t.Context(), orphan.URL)
	if err != nil || !deletion.Deleted || deletion.PreviousFound ||
		deletion.Previous != (Assignment{}) {
		t.Fatalf("orphan deletion = %#v/%v", deletion, err)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		[]EvidenceFinalization{deletion.Finalization},
	); err != nil {
		t.Fatal(err)
	}
	if _, found, err := index.Lookup(t.Context(), orphan.URL); err != nil || found {
		t.Fatalf("deleted orphan lookup = %v/%v", found, err)
	}
	if got := lookupAssignment(t, index, first.URL); got.ClusterID != assignment.ClusterID {
		t.Fatalf("survivor assignment = %#v", got)
	}
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		posting, found, readErr := index.exactBuckets.Get(tx, vault.Key(orphan.ContentHash))
		if readErr != nil {
			return fmt.Errorf("read cleaned posting: %w", readErr)
		}
		if !found || !slices.Equal(posting.URLs, []string{first.URL}) {
			t.Fatalf("cleaned posting = %#v/%v", posting, found)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMixedReplacementBatchRepairsOrphanWithoutPoisoningPeers(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	orphan := Evidence{
		URL:         "https://batch-orphan.example/page",
		ContentHash: "batch-orphan-content",
		Text:        "one two three four",
	}
	assignment := replaceEvidence(t, index, orphan)
	engine.deleteRaw(clusterBucketName, vault.Key(assignment.ClusterID))
	evidence := make([]Evidence, 16)
	evidence[0] = orphan
	for position := 1; position < len(evidence); position++ {
		evidence[position] = Evidence{
			URL:         "https://batch-fresh.example/page/" + string(rune('a'+position)),
			ContentHash: "batch-fresh-content-" + string(rune('a'+position)),
			Text:        "distinct content " + string(rune('a'+position)),
		}
	}
	replacements, err := index.ReplaceBatch(t.Context(), evidence)
	if err != nil || len(replacements) != len(evidence) {
		t.Fatalf("mixed replacement batch = %d/%v", len(replacements), err)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(replacements),
	); err != nil {
		t.Fatal(err)
	}
	for _, item := range evidence {
		lookupAssignment(t, index, item.URL)
	}
}
