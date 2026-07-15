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
	deletion      contentcluster.EvidenceDeletion
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

func (s *durableContentClusterScript) DeleteTransition(
	context.Context,
	string,
) (contentcluster.EvidenceDeletion, error) {
	return s.deletion, s.transitionErr
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

func TestDurableContentClusterDeletionSurfacesProjectionFailures(t *testing.T) {
	sentinel := errors.New("failure")
	tests := []struct {
		name          string
		transitionErr error
		clusterErr    error
		clusterFound  bool
		readErr       error
		receiveErr    error
		busy          bool
		finalizeErr   error
		wantErr       bool
	}{
		{name: "transition", transitionErr: sentinel, wantErr: true},
		{name: "cluster", clusterErr: sentinel, wantErr: true},
		{name: "missing cluster"},
		{name: "document", clusterFound: true, readErr: sentinel, wantErr: true},
		{name: "receive", clusterFound: true, receiveErr: sentinel, wantErr: true},
		{name: "capacity", clusterFound: true, busy: true, wantErr: true},
		{name: "finalize", finalizeErr: sentinel, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			script := &durableContentClusterScript{
				contentClusterScript: contentClusterScript{
					cluster: contentcluster.Cluster{
						ID:                "cluster",
						RepresentativeURL: "https://member.example/",
						MemberURLs:        []string{"https://member.example/"},
					},
					clusterFound: test.clusterFound,
					clusterErr:   test.clusterErr,
				},
				deletion: contentcluster.EvidenceDeletion{
					AffectedClusterIDs: []string{"cluster"},
				},
				transitionErr: test.transitionErr,
				finalizeErr:   test.finalizeErr,
			}
			directory := &clusterDocumentDirectoryScript{
				documents: map[string]documentstore.Document{
					"https://member.example/": {NormalizedURL: "https://member.example/"},
				},
				readErr:    test.readErr,
				receiveErr: test.receiveErr,
				receipt:    documentstore.Receipt{Busy: test.busy},
			}
			consumer := &IngestConsumer{clusters: script, documents: directory}
			err := consumer.deleteDocumentCluster(t.Context(), "https://gone.example/")
			if (err != nil) != test.wantErr {
				t.Fatalf("delete error = %v, want error %t", err, test.wantErr)
			}
			if test.transitionErr != nil {
				if script.released != 0 {
					t.Fatalf("transition failure releases = %d", script.released)
				}

				return
			}
			if script.released != 1 {
				t.Fatalf("projection releases = %d", script.released)
			}
		})
	}
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

func TestLegacyContentClusterDeletionStoresAndIndexesSurvivor(t *testing.T) {
	memberURL := "https://member.example/"
	script := survivingContentClusterScript(contentcluster.Cluster{
		ID:                "cluster",
		RepresentativeURL: memberURL,
		MemberURLs:        []string{memberURL},
	})
	directory := &clusterDocumentDirectoryScript{documents: map[string]documentstore.Document{
		memberURL: {NormalizedURL: memberURL},
	}}
	index := &anchorIndexScript{}
	consumer := &IngestConsumer{clusters: script, documents: directory, index: index}
	if err := consumer.deleteDocumentCluster(t.Context(), "https://gone.example/"); err != nil {
		t.Fatalf("legacy cluster deletion: %v", err)
	}
	if len(directory.received) != 1 || len(index.docs) != 1 ||
		index.docs[0].RepresentativeURL != memberURL {
		t.Fatalf("legacy survivor = %#v / %#v", directory.received, index.docs)
	}
}
