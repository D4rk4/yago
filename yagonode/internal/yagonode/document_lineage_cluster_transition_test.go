package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

type durableLineageClusterScript struct {
	lineageClusterScript
	deletion      contentcluster.EvidenceDeletion
	transitionErr error
	finalizeErr   error
	finalized     int
	released      int
}

func (s *durableLineageClusterScript) DeleteTransition(
	context.Context,
	string,
) (contentcluster.EvidenceDeletion, error) {
	return s.deletion, s.transitionErr
}

func (s *durableLineageClusterScript) FinalizeEvidenceTransitions(
	context.Context,
	[]contentcluster.EvidenceFinalization,
) error {
	s.finalized++

	return s.finalizeErr
}

func (s *durableLineageClusterScript) ReleaseEvidenceTransitions(
	[]contentcluster.EvidenceFinalization,
) {
	s.released++
}

func TestDocumentLineageClusterDeletionReplaysUnchangedSurvivor(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	directory, receiver, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	clusters, err := contentcluster.Open(storage, contentcluster.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	source := documentstore.Document{
		NormalizedURL: "https://source.example/",
		CanonicalURL:  "https://source.example/",
		ExtractedText: "alpha beta gamma delta",
		ContentHash:   "same",
	}
	member := documentstore.Document{
		NormalizedURL: "https://member.example/",
		ExtractedText: source.ExtractedText,
		ContentHash:   source.ContentHash,
	}
	seedLineageClusterDocument(t, clusters, receiver, source)
	seedLineageClusterDocument(t, clusters, receiver, member)

	interrupted := &lineageBatchIndexScript{
		lineageIndexScript: lineageIndexScript{err: errors.New("index interrupted")},
	}
	evictor := documentLineageEvictor{
		directory: directory,
		receiver:  receiver,
		documents: directory.(documentstore.DocumentEvictor),
		clusters:  clusters,
		index:     interrupted,
	}
	if removed, err := evictor.Delete(t.Context(), source.NormalizedURL); err == nil || removed {
		t.Fatalf("interrupted lineage deletion = %t, %v", removed, err)
	}

	replayed := &lineageBatchIndexScript{}
	evictor.index = replayed
	removed, err := evictor.Delete(t.Context(), source.NormalizedURL)
	if err != nil || !removed {
		t.Fatalf("replayed lineage deletion = %t, %v", removed, err)
	}
	if interrupted.batches != 1 || replayed.batches != 1 || len(replayed.docs) != 1 ||
		replayed.docs[0].NormalizedURL != member.NormalizedURL ||
		replayed.docs[0].RepresentativeURL != member.NormalizedURL {
		t.Fatalf("replayed lineage survivor = %#v", replayed.docs)
	}
	removed, err = evictor.Delete(t.Context(), source.NormalizedURL)
	if err != nil || removed || len(replayed.docs) != 1 {
		t.Fatalf("finalized lineage deletion = %t, %v, %#v", removed, err, replayed.docs)
	}
}

func TestDurableLineageClusterRetriesIndexedDocumentDeletionBeforeFinalization(t *testing.T) {
	sentinel := errors.New("index delete failed")
	sourceURL := "https://source.example/"
	documents := &lineageDocumentScript{docs: map[string]documentstore.Document{
		sourceURL: {NormalizedURL: sourceURL},
	}}
	clusters := &durableLineageClusterScript{}
	index := &lineageIndexScript{deleteErr: sentinel}
	evictor := documentLineageEvictor{
		directory: documents,
		documents: documents,
		clusters:  clusters,
		index:     index,
	}
	if removed, err := evictor.Delete(t.Context(), sourceURL); removed ||
		!errors.Is(err, sentinel) || clusters.finalized != 0 {
		t.Fatalf("failed indexed deletion = %t, %v, finalized %d", removed, err, clusters.finalized)
	}
	index.deleteErr = nil
	removed, err := evictor.Delete(t.Context(), sourceURL)
	if err != nil || removed || clusters.finalized != 1 || clusters.released != 2 ||
		len(index.deleted) != 2 {
		t.Fatalf(
			"retried indexed deletion = %t, %v, finalized %d, released %d, deletes %#v",
			removed,
			err,
			clusters.finalized,
			clusters.released,
			index.deleted,
		)
	}
}

func seedLineageClusterDocument(
	t *testing.T,
	clusters *contentcluster.Index,
	receiver documentstore.DocumentReceiver,
	document documentstore.Document,
) {
	t.Helper()
	replacements, err := clusters.ReplaceBatch(t.Context(), []contentcluster.Evidence{{
		URL:                document.NormalizedURL,
		Text:               document.ExtractedText,
		ContentHash:        document.ContentHash,
		CanonicalPreferred: document.CanonicalURL == document.NormalizedURL,
	}})
	if err != nil {
		t.Fatalf("replace seed cluster: %v", err)
	}
	document.ClusterID = replacements[0].Current.ClusterID
	document.RepresentativeURL = replacements[0].Current.RepresentativeURL
	if _, err := receiver.Receive(t.Context(), []documentstore.Document{document}); err != nil {
		clusters.ReleaseEvidenceTransitions([]contentcluster.EvidenceFinalization{
			replacements[0].Finalization,
		})
		t.Fatalf("store seed cluster document: %v", err)
	}
	if err := clusters.FinalizeEvidenceTransitions(
		t.Context(),
		[]contentcluster.EvidenceFinalization{replacements[0].Finalization},
	); err != nil {
		clusters.ReleaseEvidenceTransitions([]contentcluster.EvidenceFinalization{
			replacements[0].Finalization,
		})
		t.Fatalf("finalize seed cluster: %v", err)
	}
}

func TestDurableLineageClusterTransitionFailurePaths(t *testing.T) {
	sentinel := errors.New("failure")
	transitionFailure := &durableLineageClusterScript{transitionErr: sentinel}
	evictor := documentLineageEvictor{clusters: transitionFailure}
	if _, err := evictor.prepareContentClusterDeletion(
		t.Context(),
		"https://source.example/",
		documentstore.Document{},
		false,
	); !errors.Is(err, sentinel) || transitionFailure.released != 0 {
		t.Fatalf("transition failure = %v, releases %d", err, transitionFailure.released)
	}

	clusterFailure := &durableLineageClusterScript{
		lineageClusterScript: lineageClusterScript{clusterErr: sentinel},
		deletion: contentcluster.EvidenceDeletion{
			AffectedClusterIDs: []string{"cluster"},
		},
	}
	evictor.clusters = clusterFailure
	if _, err := evictor.prepareContentClusterDeletion(
		t.Context(),
		"https://source.example/",
		documentstore.Document{},
		false,
	); !errors.Is(err, sentinel) || clusterFailure.released != 1 {
		t.Fatalf("cluster failure = %v, releases %d", err, clusterFailure.released)
	}

	readFailure := &durableLineageClusterScript{
		lineageClusterScript: lineageClusterScript{
			cluster: contentcluster.Cluster{
				ID:                "cluster",
				RepresentativeURL: "https://member.example/",
				MemberURLs: []string{
					"https://source.example/",
					"https://member.example/",
				},
			},
			clusterFound: true,
		},
		deletion: contentcluster.EvidenceDeletion{
			AffectedClusterIDs: []string{"cluster"},
		},
	}
	evictor = documentLineageEvictor{
		clusters:  readFailure,
		directory: &lineageDocumentScript{readErr: sentinel},
	}
	if _, err := evictor.prepareContentClusterDeletion(
		t.Context(),
		"https://source.example/",
		documentstore.Document{},
		false,
	); !errors.Is(err, sentinel) || readFailure.released != 1 {
		t.Fatalf("document failure = %v, releases %d", err, readFailure.released)
	}
}

func TestDurableLineageClusterTransitionFiltersAndFallbacks(t *testing.T) {
	memberURL := "https://member.example/"
	missingURL := "https://missing.example/"
	staleURL := "https://stale.example/"
	script := &durableLineageClusterScript{
		lineageClusterScript: lineageClusterScript{
			cluster: contentcluster.Cluster{
				ID:                "cluster",
				RepresentativeURL: memberURL,
				MemberURLs:        []string{missingURL, memberURL, staleURL},
			},
			clusterFound: true,
		},
		deletion: contentcluster.EvidenceDeletion{
			AffectedClusterIDs: []string{"cluster"},
		},
	}
	documents := &lineageDocumentScript{docs: map[string]documentstore.Document{
		memberURL: {
			NormalizedURL:     memberURL,
			ClusterID:         "cluster",
			RepresentativeURL: memberURL,
		},
		staleURL: {NormalizedURL: staleURL},
	}}
	evictor := documentLineageEvictor{clusters: script, directory: documents}
	projection, err := evictor.prepareContentClusterDeletion(
		t.Context(),
		"https://source.example/",
		documentstore.Document{},
		false,
	)
	if err != nil || len(projection.updates) != 1 ||
		projection.updates[0].NormalizedURL != staleURL {
		t.Fatalf("filtered projection = %#v, %v", projection.updates, err)
	}
	if err := projection.finalize(t.Context()); err != nil {
		t.Fatalf("finalize filtered projection: %v", err)
	}
	projection.release()

	fallback := &durableLineageClusterScript{}
	evictor = documentLineageEvictor{clusters: fallback}
	projection, err = evictor.prepareContentClusterDeletion(
		t.Context(),
		"https://fallback.example/",
		documentstore.Document{ClusterID: "stored-cluster"},
		true,
	)
	if err != nil {
		t.Fatalf("stored cluster fallback: %v", err)
	}
	if err := projection.finalize(t.Context()); err != nil {
		t.Fatalf("finalize stored cluster fallback: %v", err)
	}
	projection.release()
}

func TestDurableLineageClusterFinalizationFailureFollowsDocumentDeletion(t *testing.T) {
	sentinel := errors.New("finalize failed")
	sourceURL := "https://source.example/"
	documents := &lineageDocumentScript{docs: map[string]documentstore.Document{
		sourceURL: {NormalizedURL: sourceURL},
	}}
	script := &durableLineageClusterScript{finalizeErr: sentinel}
	evictor := documentLineageEvictor{
		directory: documents,
		documents: documents,
		clusters:  script,
	}
	removed, err := evictor.Delete(t.Context(), sourceURL)
	if removed || !errors.Is(err, sentinel) || len(documents.deleted) != 1 ||
		script.released != 1 {
		t.Fatalf(
			"finalization failure = %t, %v, deletes %#v, releases %d",
			removed,
			err,
			documents.deleted,
			script.released,
		)
	}
}
