package crawlresults

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type durableContentClusterScript struct {
	contentClusterScript
	transitionErr error
	finalizeErr   error
	replay        bool
	incomplete    bool
	finalized     int
	released      int
}

func (s *durableContentClusterScript) ReplaceBatch(
	_ context.Context,
	evidence []contentcluster.Evidence,
) ([]contentcluster.EvidenceReplacement, error) {
	if s.transitionErr != nil {
		return nil, s.transitionErr
	}
	if s.incomplete {
		return nil, nil
	}
	replacements := make([]contentcluster.EvidenceReplacement, len(evidence))
	for index := range evidence {
		replacements[index] = contentcluster.EvidenceReplacement{
			Current:            s.assignment,
			Replay:             s.replay,
			AffectedClusterIDs: []string{s.assignment.ClusterID},
		}
	}

	return replacements, nil
}

func TestDurableContentClusterBatchMismatchReleasesProjection(t *testing.T) {
	script := &durableContentClusterScript{incomplete: true}
	consumer := &IngestConsumer{}
	_, err := consumer.replaceDocumentClusterBatch(
		t.Context(),
		[]documentstore.Document{{NormalizedURL: "https://example.org/"}},
		script,
	)
	if err == nil || script.released != 1 {
		t.Fatalf("batch mismatch = %v, releases %d", err, script.released)
	}
}

func (s *durableContentClusterScript) FinalizeEvidenceTransitions(
	context.Context,
	[]contentcluster.EvidenceFinalization,
) error {
	s.finalized++

	return s.finalizeErr
}

func (s *durableContentClusterScript) ReleaseEvidenceTransitions(
	[]contentcluster.EvidenceFinalization,
) {
	s.released++
}

func TestDurableContentClusterFinalizationRedeliversSingleAndGroup(t *testing.T) {
	sentinel := errors.New("finalize failed")
	newConsumer := func() (*IngestConsumer, *durableContentClusterScript) {
		script := &durableContentClusterScript{
			contentClusterScript: contentClusterScript{
				assignment: contentcluster.Assignment{
					ClusterID:         "cluster",
					RepresentativeURL: "https://example.org/",
				},
				cluster: contentcluster.Cluster{
					ID:                "cluster",
					RepresentativeURL: "https://example.org/",
					MemberURLs:        []string{"https://example.org/"},
				},
				clusterFound: true,
			},
			finalizeErr: sentinel,
		}

		return &IngestConsumer{
			clusters:  script,
			documents: &clusterDocumentDirectoryScript{},
			observer:  noopIngestObserver{},
		}, script
	}

	consumer, script := newConsumer()
	projection, err := consumer.prepareDocumentClusters(t.Context(), []documentstore.Document{{
		NormalizedURL: "https://example.org/",
	}})
	if err == nil {
		err = projection.finalize(t.Context())
	}
	projection.release()
	if !errors.Is(err, sentinel) || script.released != 1 {
		t.Fatalf("direct finalization = %v, releases %d", err, script.released)
	}

	consumer, script = newConsumer()
	batch := yagocrawlcontract.IngestBatch{
		SourceURL: "https://example.org/",
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: "https://example.org/",
			ExtractedText: "alpha beta gamma delta",
		},
	}
	naked := false
	if !consumer.storeDocument(t.Context(), replayDelivery(batch, &naked), batch) ||
		!naked || script.released != 1 {
		t.Fatalf("single finalization redelivery = %t, releases %d", naked, script.released)
	}

	consumer, script = newConsumer()
	naks := 0
	delivery := IngestDelivery{Nak: func(context.Context) error {
		naks++

		return nil
	}}
	if consumer.storeDocumentGroup(
		t.Context(),
		[]IngestDelivery{delivery, delivery},
		[]documentstore.Document{{NormalizedURL: "https://example.org/"}},
	) || naks != 2 || script.released != 1 {
		t.Fatalf("group finalization redelivery = %d, releases %d", naks, script.released)
	}
}

func TestDurableContentClusterGroupReplayIndexesPreparedDocuments(t *testing.T) {
	script := &durableContentClusterScript{
		contentClusterScript: contentClusterScript{
			assignment: contentcluster.Assignment{
				ClusterID:         "cluster",
				RepresentativeURL: "https://first.example/",
			},
			cluster: contentcluster.Cluster{
				ID:                "cluster",
				RepresentativeURL: "https://first.example/",
				MemberURLs: []string{
					"https://first.example/",
					"https://second.example/",
				},
			},
			clusterFound: true,
		},
		replay: true,
	}
	index := &anchorIndexScript{}
	consumer := &IngestConsumer{
		clusters:  script,
		documents: &clusterDocumentDirectoryScript{},
		index:     index,
		observer:  noopIngestObserver{},
	}
	documents := []documentstore.Document{
		{NormalizedURL: "https://first.example/"},
		{NormalizedURL: "https://second.example/"},
	}
	if !consumer.storeDocumentGroup(t.Context(), []IngestDelivery{{}, {}}, documents) {
		t.Fatal("group replay was deferred")
	}
	assertIndexedDocumentURLs(
		t,
		index.docs,
		"https://first.example/",
		"https://second.example/",
	)
}
