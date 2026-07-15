package crawlresults

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type contentClusterScript struct {
	evidence     []contentcluster.Evidence
	assignment   contentcluster.Assignment
	cluster      contentcluster.Cluster
	lookup       contentcluster.Assignment
	lookupFound  bool
	deletedURLs  []string
	replaceErr   error
	deleteErr    error
	lookupErr    error
	clusterErr   error
	clusterFound bool
}

type replacedClusterScript struct {
	contentClusterScript
	previousClusterID string
}

type orderedRemovalClusterScript struct {
	contentClusterScript
	events *[]string
}

type orderedRemovalPurger struct {
	events *[]string
}

func (p orderedRemovalPurger) Purge(context.Context, []yagomodel.Hash) error {
	*p.events = append(*p.events, "purge")

	return nil
}

func (s *orderedRemovalClusterScript) Delete(
	ctx context.Context,
	url string,
) (bool, error) {
	*s.events = append(*s.events, "cluster")

	return s.contentClusterScript.Delete(ctx, url)
}

func (s *replacedClusterScript) Cluster(
	_ context.Context,
	clusterID string,
) (contentcluster.Cluster, bool, error) {
	if clusterID == s.previousClusterID {
		return contentcluster.Cluster{}, false, nil
	}

	return s.cluster, s.clusterFound, s.clusterErr
}

type clusterDocumentDirectoryScript struct {
	documents  map[string]documentstore.Document
	receipt    documentstore.Receipt
	receiveErr error
	readErr    error
	received   []documentstore.Document
}

func (s *clusterDocumentDirectoryScript) Receive(
	_ context.Context,
	docs []documentstore.Document,
) (documentstore.Receipt, error) {
	s.received = append(s.received, docs...)

	return s.receipt, s.receiveErr
}

func (s *clusterDocumentDirectoryScript) Document(
	_ context.Context,
	url string,
) (documentstore.Document, bool, error) {
	if s.readErr != nil {
		return documentstore.Document{}, false, s.readErr
	}
	doc, found := s.documents[url]

	return doc, found, nil
}

func (s *clusterDocumentDirectoryScript) Count(context.Context) (int, error) {
	return len(s.documents), nil
}

func (s *contentClusterScript) Replace(
	_ context.Context,
	evidence contentcluster.Evidence,
) (contentcluster.Assignment, error) {
	s.evidence = append(s.evidence, evidence)

	return s.assignment, s.replaceErr
}

func (s *contentClusterScript) Delete(_ context.Context, url string) (bool, error) {
	s.deletedURLs = append(s.deletedURLs, url)

	return s.deleteErr == nil, s.deleteErr
}

func (s *contentClusterScript) Lookup(
	context.Context,
	string,
) (contentcluster.Assignment, bool, error) {
	return s.lookup, s.lookupFound, s.lookupErr
}

func (s *contentClusterScript) Cluster(
	context.Context,
	string,
) (contentcluster.Cluster, bool, error) {
	return s.cluster, s.clusterFound, s.clusterErr
}

type clusterLifecycle struct {
	consumer  *IngestConsumer
	directory documentstore.DocumentDirectory
	receiver  documentstore.DocumentReceiver
	index     searchindex.SearchIndex
	clusters  *contentcluster.Index
}

func openClusterLifecycle(t *testing.T) clusterLifecycle {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	directory, receiver, err := documentstore.Open(v)
	if err != nil {
		t.Fatalf("open documents: %v", err)
	}
	clusters, err := contentcluster.Open(v, contentcluster.Limits{})
	if err != nil {
		t.Fatalf("open clusters: %v", err)
	}
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	consumer := &IngestConsumer{
		documents: receiver,
		index:     index,
		clusters:  clusters,
	}

	return clusterLifecycle{
		consumer:  consumer,
		directory: directory,
		receiver:  receiver,
		index:     index,
		clusters:  clusters,
	}
}

func persistClusterLifecycleDocument(
	t *testing.T,
	lifecycle clusterLifecycle,
	doc documentstore.Document,
) []documentstore.Document {
	t.Helper()
	projection, err := lifecycle.consumer.prepareDocumentClusters(
		t.Context(),
		[]documentstore.Document{doc},
	)
	if err != nil {
		t.Fatalf("cluster document: %v", err)
	}
	defer projection.release()
	writes := projection.documents
	if _, err := lifecycle.receiver.Receive(t.Context(), writes); err != nil {
		t.Fatalf("store document: %v", err)
	}
	if err := lifecycle.consumer.indexDocuments(t.Context(), writes); err != nil {
		t.Fatalf("index document: %v", err)
	}
	if err := projection.finalize(t.Context()); err != nil {
		t.Fatalf("finalize cluster document: %v", err)
	}

	return writes
}

func assertClusterLifecycleDocument(
	t *testing.T,
	lifecycle clusterLifecycle,
	url string,
	representativeURL string,
) {
	t.Helper()
	doc, found, err := lifecycle.directory.Document(t.Context(), url)
	if err != nil || !found {
		t.Fatalf("document %q = %+v, %v, %v", url, doc, found, err)
	}
	if doc.RepresentativeURL != representativeURL || doc.ClusterID == "" {
		t.Fatalf("document %q cluster assignment = %+v", url, doc)
	}
}

func TestClusterDocumentsPersistsRepresentativeChangesWithoutDroppingMembers(t *testing.T) {
	lifecycle := openClusterLifecycle(t)
	first := documentstore.Document{
		NormalizedURL: "https://a.example",
		CanonicalURL:  "https://elsewhere.example",
		ExtractedText: "alpha beta gamma delta",
		ContentHash:   "same",
		ContentQuality: documentstore.ContentQualityEvidence{
			Known: true,
			Score: 1,
		},
	}
	persistClusterLifecycleDocument(t, lifecycle, first)
	second := documentstore.Document{
		NormalizedURL: "https://b.example",
		CanonicalURL:  "https://b.example",
		ExtractedText: "alpha beta gamma delta",
		ContentHash:   "same",
	}
	secondWrites := persistClusterLifecycleDocument(t, lifecycle, second)
	if len(secondWrites) != 2 {
		t.Fatalf("representative refresh writes = %d, want 2", len(secondWrites))
	}
	for _, url := range []string{first.NormalizedURL, second.NormalizedURL} {
		assertClusterLifecycleDocument(t, lifecycle, url, second.NormalizedURL)
	}
	results, err := lifecycle.index.Search(t.Context(), searchindex.SearchRequest{
		Query:      "alpha",
		Terms:      []string{"alpha"},
		MaxResults: 10,
	})
	if err != nil || len(results.Results) != 2 {
		t.Fatalf("clustered search = %+v, %v", results, err)
	}
	for _, result := range results.Results {
		if result.RepresentativeURL != second.NormalizedURL || result.ClusterID == "" {
			t.Fatalf("search cluster assignment = %+v", result)
		}
	}
	if writes := persistClusterLifecycleDocument(t, lifecycle, second); len(writes) != 1 {
		t.Fatalf("idempotent recrawl writes = %d, want 1", len(writes))
	}
	if err := lifecycle.consumer.deleteDocumentCluster(
		t.Context(),
		second.NormalizedURL,
	); err != nil {
		t.Fatalf("delete representative cluster: %v", err)
	}
	remaining, found, err := lifecycle.directory.Document(t.Context(), first.NormalizedURL)
	if err != nil || !found || remaining.RepresentativeURL != first.NormalizedURL {
		t.Fatalf("surviving representative = %+v, %v, %v", remaining, found, err)
	}
	if _, found, err := lifecycle.clusters.Lookup(
		t.Context(),
		second.NormalizedURL,
	); err != nil ||
		found {
		t.Fatalf("deleted cluster assignment = %v, %v", found, err)
	}
	if count, err := lifecycle.directory.Count(t.Context()); err != nil || count != 2 {
		t.Fatalf("stored document count = %d, %v", count, err)
	}
}

func TestNearDuplicateRecrawlKeepsStableClusterAssignment(t *testing.T) {
	lifecycle := openClusterLifecycle(t)
	first := documentstore.Document{
		NormalizedURL: "https://a.example",
		ExtractedText: "alpha beta gamma delta epsilon zeta eta theta",
		ContentHash:   "first",
	}
	second := documentstore.Document{
		NormalizedURL: "https://b.example",
		ExtractedText: first.ExtractedText,
		ContentHash:   "second",
	}
	persistClusterLifecycleDocument(t, lifecycle, first)
	persistClusterLifecycleDocument(t, lifecycle, second)
	firstAssignment, firstFound, err := lifecycle.clusters.Lookup(
		t.Context(),
		first.NormalizedURL,
	)
	if err != nil || !firstFound {
		t.Fatalf("first near-duplicate assignment = %+v, %v, %v", firstAssignment, firstFound, err)
	}
	secondAssignment, secondFound, err := lifecycle.clusters.Lookup(
		t.Context(),
		second.NormalizedURL,
	)
	if err != nil || !secondFound || secondAssignment.ClusterID != firstAssignment.ClusterID {
		t.Fatalf(
			"second near-duplicate assignment = %+v, %v, %v",
			secondAssignment,
			secondFound,
			err,
		)
	}
	if writes := persistClusterLifecycleDocument(t, lifecycle, second); len(writes) != 1 {
		t.Fatalf("near-duplicate recrawl writes = %d, want 1", len(writes))
	}
	if count, err := lifecycle.directory.Count(t.Context()); err != nil || count != 2 {
		t.Fatalf("near-duplicate document count = %d, %v", count, err)
	}
}

func TestClusterDocumentsRefreshesClusterLeftByReplacement(t *testing.T) {
	lifecycle := openClusterLifecycle(t)
	first := documentstore.Document{
		NormalizedURL: "https://a.example",
		CanonicalURL:  "https://a.example",
		ExtractedText: "alpha beta gamma delta",
		ContentHash:   "shared",
		ContentQuality: documentstore.ContentQualityEvidence{
			Known: true,
			Score: 1,
		},
	}
	second := documentstore.Document{
		NormalizedURL: "https://b.example",
		CanonicalURL:  "https://elsewhere.example",
		ExtractedText: first.ExtractedText,
		ContentHash:   first.ContentHash,
	}
	persistClusterLifecycleDocument(t, lifecycle, first)
	persistClusterLifecycleDocument(t, lifecycle, second)

	replacement := first
	replacement.ExtractedText = "unrelated replacement content"
	replacement.ContentHash = "replacement"
	writes := persistClusterLifecycleDocument(t, lifecycle, replacement)
	if len(writes) != 2 {
		t.Fatalf("replacement writes = %#v", writes)
	}
	assertClusterLifecycleDocument(t, lifecycle, second.NormalizedURL, second.NormalizedURL)
	firstAssignment, firstFound, err := lifecycle.clusters.Lookup(
		t.Context(),
		first.NormalizedURL,
	)
	if err != nil || !firstFound {
		t.Fatalf("replacement assignment = %#v, %v, %v", firstAssignment, firstFound, err)
	}
	secondAssignment, secondFound, err := lifecycle.clusters.Lookup(
		t.Context(),
		second.NormalizedURL,
	)
	if err != nil || !secondFound || secondAssignment.ClusterID == firstAssignment.ClusterID {
		t.Fatalf("surviving assignment = %#v, %v, %v", secondAssignment, secondFound, err)
	}
}

func TestClusterDocumentsToleratesEmptyPreviousCluster(t *testing.T) {
	script := &replacedClusterScript{
		contentClusterScript: contentClusterScript{
			assignment: contentcluster.Assignment{
				ClusterID:         "new",
				RepresentativeURL: "https://example.org/",
			},
			lookup:      contentcluster.Assignment{ClusterID: "old"},
			lookupFound: true,
			cluster: contentcluster.Cluster{
				ID:                "new",
				RepresentativeURL: "https://example.org/",
				MemberURLs:        []string{"https://example.org/"},
			},
			clusterFound: true,
		},
		previousClusterID: "old",
	}
	consumer := &IngestConsumer{clusters: script}
	projection, err := consumer.prepareDocumentClusters(t.Context(), []documentstore.Document{{
		NormalizedURL: "https://example.org/",
	}})
	defer projection.release()
	docs := projection.documents
	if err != nil || len(docs) != 1 || docs[0].ClusterID != "new" {
		t.Fatalf("replacement cluster = %#v, %v", docs, err)
	}
}

func TestStoreDocumentClustersAndIndexesCanonicalEvidence(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	directory, receiver, err := documentstore.Open(v)
	if err != nil {
		t.Fatalf("open documents: %v", err)
	}
	url := "https://target.example/"
	if _, err := receiver.Receive(t.Context(), []documentstore.Document{{
		NormalizedURL: url,
		ExtractedText: "stored text",
	}}); err != nil {
		t.Fatalf("seed document: %v", err)
	}
	anchors := receiver.(documentstore.InboundAnchorReceiver)
	seedClusterAnchorEvidence(t, anchors, url)
	clusters := &contentClusterScript{
		assignment: contentcluster.Assignment{
			ClusterID:         "cluster",
			RepresentativeURL: url,
		},
		cluster: contentcluster.Cluster{
			ID:                "cluster",
			RepresentativeURL: url,
			MemberURLs:        []string{url},
		},
		clusterFound: true,
	}
	index := &anchorIndexScript{}
	consumer := &IngestConsumer{
		documents: receiver,
		clusters:  clusters,
		index:     index,
		observer:  noopIngestObserver{},
	}
	delivery := IngestDelivery{Nak: func(context.Context) error {
		t.Fatal("canonical ingest was redelivered")

		return nil
	}}
	batch := yagocrawlcontract.IngestBatch{
		SourceURL: url,
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: url,
			ExtractedText: "incoming text",
		},
	}
	if consumer.storeDocument(t.Context(), delivery, batch) {
		t.Fatal("canonical ingest deferred")
	}
	if len(clusters.evidence) != 1 || clusters.evidence[0].InboundAuthority != 1 {
		t.Fatalf("cluster evidence = %#v", clusters.evidence)
	}
	if len(index.docs) != 1 || len(index.docs[0].Inlinks) != 1 ||
		index.docs[0].ClusterID != "cluster" {
		t.Fatalf("indexed canonical document = %#v", index.docs)
	}
	stored, found, err := directory.Document(t.Context(), url)
	if err != nil || !found || len(stored.Inlinks) != 1 || stored.ClusterID != "cluster" {
		t.Fatalf("stored canonical document = %#v, %v, %v", stored, found, err)
	}
}

func seedClusterAnchorEvidence(
	t *testing.T,
	anchors documentstore.InboundAnchorReceiver,
	targetURL string,
) {
	t.Helper()
	update, err := anchors.ReplaceOutboundAnchors(
		t.Context(),
		[]documentstore.OutboundAnchorSet{{
			SourceURL: "https://source.example/",
			Anchors: []documentstore.OutboundAnchor{{
				TargetURL: targetURL,
				Text:      "trusted",
			}},
		}},
	)
	if err != nil {
		t.Fatalf("seed anchors: %v", err)
	}
	if err := anchors.FinalizeOutboundAnchors(
		t.Context(),
		update.Finalizations,
	); err != nil {
		t.Fatalf("finalize seed anchors: %v", err)
	}
}

func TestAssignDocumentClusterUsesPreparedQualityCanonicalAndAuthorityEvidence(t *testing.T) {
	script := &contentClusterScript{
		assignment: contentcluster.Assignment{
			ClusterID:         "cluster",
			RepresentativeURL: "https://a.example",
		},
	}
	consumer := &IngestConsumer{clusters: script}
	doc := documentstore.Document{
		NormalizedURL: "https://a.example",
		CanonicalURL:  "https://a.example",
		Title:         "Alpha",
		Headings:      []string{"Beta"},
		ExtractedText: "Gamma",
		ContentQuality: documentstore.ContentQualityEvidence{
			Known: true,
			Score: 0.75,
		},
		Inlinks: []documentstore.AnchorText{
			{URL: "https://trusted.example"},
			{URL: "https://trusted.example"},
			{URL: "https://nofollow.example", NoFollow: true},
			{URL: "https://ugc.example", UserGenerated: true},
			{URL: "https://sponsored.example", Sponsored: true},
		},
	}
	assigned, err := consumer.assignDocumentCluster(t.Context(), doc)
	if err != nil {
		t.Fatalf("assign cluster: %v", err)
	}
	if len(script.evidence) != 1 {
		t.Fatalf("cluster evidence calls = %d", len(script.evidence))
	}
	evidence := script.evidence[0]
	if evidence.URL != doc.NormalizedURL || !evidence.CanonicalPreferred ||
		evidence.Quality != 0.75 || evidence.InboundAuthority != 1 ||
		evidence.ContentHash == "" ||
		evidence.Text != "Alpha Beta Gamma" {
		t.Fatalf("cluster evidence = %+v", evidence)
	}
	if assigned.ContentHash != evidence.ContentHash || assigned.ClusterID != "cluster" ||
		assigned.RepresentativeURL != doc.NormalizedURL {
		t.Fatalf("assigned document = %+v", assigned)
	}
}

func TestRemovalTombstoneLeavesClusterDeletionToConfiguredPurger(t *testing.T) {
	script := &contentClusterScript{}
	consumer := &IngestConsumer{
		clusters: script,
		owner:    allowAllOwnership{},
		purger:   noopURLPurger{},
		hashURL:  yagomodel.HashURL,
		observer: noopIngestObserver{},
	}
	acked := false
	consumer.absorbRemoval(t.Context(), IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://gone.example",
			Removed:   true,
		},
		Ack: func(context.Context) error {
			acked = true

			return nil
		},
		Nak: func(context.Context) error {
			t.Fatal("unexpected tombstone redelivery")

			return nil
		},
	})
	if !acked || len(script.deletedURLs) != 0 {
		t.Fatalf("tombstone cluster deletion = %v, %v", acked, script.deletedURLs)
	}
}

func TestRemovalTombstoneInvokesConfiguredPurgerOnce(t *testing.T) {
	events := []string{}
	clusters := &orderedRemovalClusterScript{events: &events}
	consumer := NewIngestConsumer(stubStream{}, nil, nil, nil)
	consumer.clusters = clusters
	consumer.PurgeURLs(orderedRemovalPurger{events: &events})
	acked := false
	consumer.purgeRemoval(t.Context(), IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://gone.example/",
			Removed:   true,
		},
		Ack: func(context.Context) error {
			acked = true

			return nil
		},
	})
	if !acked || len(events) != 1 || events[0] != "purge" {
		t.Fatalf("removal ordering = %t, %#v", acked, events)
	}
}

func TestContentClusterFailuresRedeliverAdmissionAndRemoval(t *testing.T) {
	sentinel := errors.New("cluster unavailable")
	replaceScript := &contentClusterScript{lookupErr: sentinel}
	consumer := &IngestConsumer{clusters: replaceScript}
	if _, err := consumer.prepareDocumentClusters(t.Context(), []documentstore.Document{{
		NormalizedURL: "https://a.example",
	}}); !errors.Is(err, sentinel) {
		t.Fatalf("replacement lookup failure = %v", err)
	}
	replaceScript.lookupErr = nil
	replaceScript.replaceErr = sentinel
	if _, err := consumer.prepareDocumentClusters(t.Context(), []documentstore.Document{{
		NormalizedURL: "https://a.example",
	}}); !errors.Is(err, sentinel) {
		t.Fatalf("replacement failure = %v", err)
	}
	deleteScript := &contentClusterScript{lookupErr: sentinel}
	consumer.clusters = deleteScript
	if err := consumer.deleteDocumentCluster(
		t.Context(),
		"https://a.example",
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("deletion lookup failure = %v", err)
	}
	deleteScript.lookupErr = nil
	deleteScript.deleteErr = sentinel
	if err := consumer.deleteDocumentCluster(
		t.Context(),
		"https://a.example",
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("deletion failure = %v", err)
	}
}

func TestContentClusterSetterAndNoopPaths(t *testing.T) {
	script := &contentClusterScript{}
	consumer := &IngestConsumer{}
	consumer.TrackContentClusters(script)
	consumer.TrackContentClusters(nil)
	if consumer.clusters != script {
		t.Fatal("content cluster setter did not retain the configured index")
	}
	withoutClusters := &IngestConsumer{}
	docs := []documentstore.Document{{NormalizedURL: "https://a.example"}}
	projection, err := withoutClusters.prepareDocumentClusters(t.Context(), docs)
	if err != nil || len(projection.documents) != 1 {
		t.Fatalf("clusterless documents = %+v, %v", projection.documents, err)
	}
	projection, err = consumer.prepareDocumentClusters(t.Context(), nil)
	if err != nil || projection.documents != nil {
		t.Fatalf("empty documents = %+v, %v", projection.documents, err)
	}
	if err := withoutClusters.deleteDocumentCluster(t.Context(), "https://a.example"); err != nil {
		t.Fatalf("clusterless deletion: %v", err)
	}
}

func TestClusterDocumentsReportsClusterAndDirectoryFailures(t *testing.T) {
	doc := documentstore.Document{NormalizedURL: "https://a.example"}
	for _, script := range []*contentClusterScript{
		{
			assignment:   contentcluster.Assignment{ClusterID: "cluster"},
			clusterErr:   errors.New("read failed"),
			clusterFound: true,
		},
		{
			assignment:   contentcluster.Assignment{ClusterID: "cluster"},
			clusterFound: false,
		},
	} {
		consumer := &IngestConsumer{clusters: script}
		if _, err := consumer.prepareDocumentClusters(
			t.Context(),
			[]documentstore.Document{doc},
		); err == nil {
			t.Fatalf("cluster failure %+v succeeded", script)
		}
	}
	directory := &clusterDocumentDirectoryScript{
		documents: map[string]documentstore.Document{},
		readErr:   errors.New("document read failed"),
	}
	script := &contentClusterScript{
		assignment: contentcluster.Assignment{ClusterID: "cluster"},
		cluster: contentcluster.Cluster{
			ID:                "cluster",
			RepresentativeURL: "https://a.example",
			MemberURLs:        []string{"https://stored.example"},
		},
		clusterFound: true,
	}
	consumer := &IngestConsumer{clusters: script, documents: directory}
	if _, err := consumer.prepareDocumentClusters(
		t.Context(),
		[]documentstore.Document{doc},
	); err == nil {
		t.Fatal("clustered document read failure succeeded")
	}
}

func TestStoredClusterUpdatesBoundsAndSkipsCurrentRecords(t *testing.T) {
	cluster := contentcluster.Cluster{
		ID:                "cluster",
		RepresentativeURL: "https://representative.example",
		MemberURLs: []string{
			"https://excluded.example",
			"https://missing.example",
			"https://current.example",
			"https://stale.example",
		},
	}
	if updates, err := (&IngestConsumer{}).storedClusterUpdates(
		t.Context(),
		cluster,
		nil,
	); err != nil || updates != nil {
		t.Fatalf("directory-free updates = %+v, %v", updates, err)
	}
	directory := &clusterDocumentDirectoryScript{documents: map[string]documentstore.Document{
		"https://current.example": {
			NormalizedURL:     "https://current.example",
			ClusterID:         cluster.ID,
			RepresentativeURL: cluster.RepresentativeURL,
		},
		"https://stale.example": {NormalizedURL: "https://stale.example"},
	}}
	consumer := &IngestConsumer{documents: directory}
	updates, err := consumer.storedClusterUpdates(
		t.Context(),
		cluster,
		map[string]struct{}{"https://excluded.example": {}},
	)
	if err != nil || len(updates) != 1 ||
		updates[0].NormalizedURL != "https://stale.example" ||
		updates[0].ClusterID != cluster.ID ||
		updates[0].RepresentativeURL != cluster.RepresentativeURL {
		t.Fatalf("stored cluster updates = %+v, %v", updates, err)
	}
}

func TestAssignDocumentClusterDefaultsCanonicalAndNormalizedURL(t *testing.T) {
	script := &contentClusterScript{
		assignment: contentcluster.Assignment{
			ClusterID:         "cluster",
			RepresentativeURL: "https://canonical.example",
		},
	}
	consumer := &IngestConsumer{clusters: script}
	assigned, err := consumer.assignDocumentCluster(t.Context(), documentstore.Document{
		CanonicalURL: " https://canonical.example ",
	})
	if err != nil || assigned.NormalizedURL != "https://canonical.example" ||
		assigned.CanonicalURL != " https://canonical.example " {
		t.Fatalf("canonical fallback = %+v, %v", assigned, err)
	}
	assigned, err = consumer.assignDocumentCluster(t.Context(), documentstore.Document{
		NormalizedURL: " https://normalized.example ",
	})
	if err != nil || assigned.NormalizedURL != "https://normalized.example" ||
		assigned.CanonicalURL != "https://normalized.example" {
		t.Fatalf("normalized fallback = %+v, %v", assigned, err)
	}
}

func TestDeleteDocumentClusterReportsSurvivorUpdateFailures(t *testing.T) {
	sentinel := errors.New("failure")
	baseCluster := contentcluster.Cluster{
		ID:                "cluster",
		RepresentativeURL: "https://member.example",
		MemberURLs:        []string{"https://member.example"},
	}
	tests := []struct {
		name      string
		script    *contentClusterScript
		directory *clusterDocumentDirectoryScript
		index     *anchorIndexScript
	}{
		{
			name:      "surviving cluster read",
			script:    survivingContentClusterScript(baseCluster),
			directory: &clusterDocumentDirectoryScript{},
		},
		{
			name:      "surviving document read",
			script:    survivingContentClusterScript(baseCluster),
			directory: &clusterDocumentDirectoryScript{readErr: sentinel},
		},
		{
			name:   "surviving document store",
			script: survivingContentClusterScript(baseCluster),
			directory: &clusterDocumentDirectoryScript{
				documents: map[string]documentstore.Document{
					"https://member.example": {NormalizedURL: "https://member.example"},
				},
				receiveErr: sentinel,
			},
		},
		{
			name:   "surviving document capacity",
			script: survivingContentClusterScript(baseCluster),
			directory: &clusterDocumentDirectoryScript{
				documents: map[string]documentstore.Document{
					"https://member.example": {NormalizedURL: "https://member.example"},
				},
				receipt: documentstore.Receipt{Busy: true},
			},
		},
		{
			name:   "surviving document index",
			script: survivingContentClusterScript(baseCluster),
			directory: &clusterDocumentDirectoryScript{documents: map[string]documentstore.Document{
				"https://member.example": {NormalizedURL: "https://member.example"},
			}},
			index: &anchorIndexScript{err: sentinel},
		},
	}
	tests[0].script.clusterErr = sentinel
	for _, test := range tests {
		consumer := &IngestConsumer{
			clusters:  test.script,
			documents: test.directory,
			index:     test.index,
		}
		if err := consumer.deleteDocumentCluster(t.Context(), "https://gone.example"); err == nil {
			t.Fatalf("%s succeeded", test.name)
		}
	}
}

func survivingContentClusterScript(cluster contentcluster.Cluster) *contentClusterScript {
	return &contentClusterScript{
		lookup:       contentcluster.Assignment{ClusterID: cluster.ID},
		lookupFound:  true,
		cluster:      cluster,
		clusterFound: true,
	}
}

func TestDeleteDocumentClusterHandlesEmptySurvivorPaths(t *testing.T) {
	baseCluster := contentcluster.Cluster{
		ID:                "cluster",
		RepresentativeURL: "https://member.example",
		MemberURLs:        []string{"https://member.example"},
	}
	noAssignment := &IngestConsumer{clusters: &contentClusterScript{}}
	if err := noAssignment.deleteDocumentCluster(t.Context(), "https://gone.example"); err != nil {
		t.Fatalf("missing assignment deletion: %v", err)
	}
	noCluster := &IngestConsumer{clusters: &contentClusterScript{
		lookup:       contentcluster.Assignment{ClusterID: "cluster"},
		lookupFound:  true,
		clusterFound: false,
	}}
	if err := noCluster.deleteDocumentCluster(t.Context(), "https://gone.example"); err != nil {
		t.Fatalf("empty surviving cluster deletion: %v", err)
	}
	noUpdates := &IngestConsumer{
		clusters: &contentClusterScript{
			lookup:       contentcluster.Assignment{ClusterID: "cluster"},
			lookupFound:  true,
			cluster:      baseCluster,
			clusterFound: true,
		},
		documents: &clusterDocumentDirectoryScript{documents: map[string]documentstore.Document{}},
	}
	if err := noUpdates.deleteDocumentCluster(t.Context(), "https://gone.example"); err != nil {
		t.Fatalf("unchanged surviving cluster deletion: %v", err)
	}
}

func TestClusterFailuresUseAdmissionRedeliveryPaths(t *testing.T) {
	sentinel := errors.New("cluster failed")
	script := &contentClusterScript{replaceErr: sentinel}
	directory := &clusterDocumentDirectoryScript{}
	consumer := &IngestConsumer{
		clusters:  script,
		documents: directory,
		observer:  noopIngestObserver{},
	}
	naked := 0
	delivery := IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://a.example",
			Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL: "https://a.example",
				ExtractedText: "alpha",
			},
		},
		Nak: func(context.Context) error {
			naked++

			return nil
		},
	}
	if deferred := consumer.storeDocument(
		t.Context(),
		delivery,
		delivery.Batch,
	); !deferred ||
		naked != 1 {
		t.Fatalf("single cluster redelivery = %v, %d", deferred, naked)
	}
	naked = 0
	group := []IngestDelivery{delivery, delivery}
	if stored := consumer.storeDocumentGroup(
		t.Context(),
		group,
		[]documentstore.Document{{NormalizedURL: "https://a.example"}},
	); stored || naked != len(group) {
		t.Fatalf("group cluster redelivery = %v, %d", stored, naked)
	}
}

func TestRemovalTombstoneDoesNotInvokeIndependentClusterFallback(t *testing.T) {
	sentinel := errors.New("cluster failed")
	consumer := &IngestConsumer{
		clusters: &contentClusterScript{lookupErr: sentinel},
		owner:    allowAllOwnership{},
		purger:   noopURLPurger{},
		hashURL:  yagomodel.HashURL,
		observer: noopIngestObserver{},
	}
	acked := false
	consumer.absorbRemoval(t.Context(), IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://gone.example",
			Removed:   true,
		},
		Ack: func(context.Context) error {
			acked = true

			return nil
		},
		Nak: func(context.Context) error {
			t.Fatal("unexpected tombstone redelivery")

			return nil
		},
	})
	if !acked {
		t.Fatal("tombstone was not acknowledged")
	}
}
