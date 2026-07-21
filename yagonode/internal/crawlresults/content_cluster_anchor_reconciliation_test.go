package crawlresults

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func TestStoredClusterProjectionReconcilesReplacedAnchorEvidence(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	directory, receiver, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	targetURL := "https://target.example/cluster-member"
	sourceURL := "https://source.example/cluster-member"
	previous := documentstore.AnchorText{URL: sourceURL, Text: "shared"}
	submitted := documentstore.AnchorText{
		URL:      sourceURL,
		Text:     "shared",
		NoFollow: true,
	}
	current := documentstore.AnchorText{URL: sourceURL, Text: "current"}
	replaceClusterAnchor(t, receiver, sourceURL, targetURL, previous.Text)
	if _, err := receiver.Receive(t.Context(), []documentstore.Document{{
		NormalizedURL: targetURL,
		ClusterID:     "previous-cluster",
		Inlinks:       []documentstore.AnchorText{submitted},
	}}); err != nil {
		t.Fatal(err)
	}
	consumer := &IngestConsumer{documents: receiver}
	cluster := contentcluster.Cluster{
		ID:                "current-cluster",
		RepresentativeURL: targetURL,
		MemberURLs:        []string{targetURL},
	}
	updates, err := consumer.storedClusterProjection(
		t.Context(),
		cluster,
		nil,
		true,
	)
	if err != nil || len(updates) != 1 ||
		!clusterDocumentHasAnchor(updates[0], previous) ||
		!clusterDocumentHasAnchor(updates[0], submitted) {
		t.Fatalf("cluster projection = %#v/%v", updates, err)
	}
	replaceClusterAnchor(t, receiver, sourceURL, targetURL, current.Text)
	if err := consumer.storeSurvivingDocumentClusterProjection(
		t.Context(),
		updates,
		false,
	); err != nil {
		t.Fatal(err)
	}
	stored, found, err := directory.Document(t.Context(), targetURL)
	if err != nil || !found || stored.ClusterID != cluster.ID ||
		stored.RepresentativeURL != cluster.RepresentativeURL ||
		len(stored.Inlinks) != 2 ||
		!clusterDocumentHasAnchor(stored, current) ||
		!clusterDocumentHasAnchor(stored, submitted) ||
		clusterDocumentHasAnchor(stored, previous) {
		t.Fatalf("reconciled cluster member = %#v/%t/%v", stored, found, err)
	}
}

func replaceClusterAnchor(
	t *testing.T,
	receiver documentstore.DocumentReceiver,
	sourceURL string,
	targetURL string,
	text string,
) {
	t.Helper()
	_, err := receiver.(documentstore.OutboundAnchorDocumentReplacer).
		ReplaceOutboundAnchorDocuments(
			t.Context(),
			[]documentstore.OutboundAnchorSet{{
				SourceURL: sourceURL,
				Anchors: []documentstore.OutboundAnchor{{
					TargetURL: targetURL,
					Text:      text,
				}},
			}},
			nil,
		)
	if err != nil {
		t.Fatal(err)
	}
}

func clusterDocumentHasAnchor(
	document documentstore.Document,
	want documentstore.AnchorText,
) bool {
	for _, anchor := range document.Inlinks {
		if anchor == want {
			return true
		}
	}

	return false
}
