package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type lineageDocumentScript struct {
	docs          map[string]documentstore.Document
	readErr       error
	receiveErr    error
	deleteErr     error
	receipt       documentstore.Receipt
	received      []documentstore.Document
	deleted       []string
	commitReceipt bool
}

func (s *lineageDocumentScript) Document(
	_ context.Context,
	url string,
) (documentstore.Document, bool, error) {
	if s.readErr != nil {
		return documentstore.Document{}, false, s.readErr
	}
	doc, found := s.docs[url]

	return doc, found, nil
}

func (s *lineageDocumentScript) Count(context.Context) (int, error) {
	return len(s.docs), nil
}

func (s *lineageDocumentScript) Receive(
	_ context.Context,
	docs []documentstore.Document,
) (documentstore.Receipt, error) {
	if s.receiveErr != nil {
		return documentstore.Receipt{}, s.receiveErr
	}
	if s.receipt.Busy {
		return s.receipt, nil
	}
	if s.docs == nil {
		s.docs = make(map[string]documentstore.Document)
	}
	s.received = append(s.received, docs...)
	for _, doc := range docs {
		s.docs[doc.NormalizedURL] = doc
	}
	receipt := s.receipt
	if s.commitReceipt {
		receipt.CommittedDocuments = append([]documentstore.Document(nil), docs...)
	}

	return receipt, nil
}

func (s *lineageDocumentScript) Delete(
	_ context.Context,
	url string,
) (bool, error) {
	s.deleted = append(s.deleted, url)
	if s.deleteErr != nil {
		return false, s.deleteErr
	}
	_, found := s.docs[url]
	delete(s.docs, url)

	return found, nil
}

type lineageAnchorScript struct {
	update        documentstore.AnchorUpdate
	documents     []documentstore.Document
	err           error
	finalizeErr   error
	sets          []documentstore.OutboundAnchorSet
	finalizations []documentstore.OutboundAnchorFinalization
	finalizeCalls int
}

func (s *lineageAnchorScript) VisitOutboundAnchorDocuments(
	_ context.Context,
	_ []documentstore.OutboundAnchorFinalization,
	visit func([]documentstore.Document) error,
) error {
	if len(s.documents) == 0 {
		return nil
	}

	return visit(s.documents)
}

func (s *lineageAnchorScript) FinalizeOutboundAnchors(
	_ context.Context,
	finalizations []documentstore.OutboundAnchorFinalization,
) error {
	s.finalizeCalls++
	s.finalizations = append(
		[]documentstore.OutboundAnchorFinalization(nil),
		finalizations...,
	)

	return s.finalizeErr
}

func (*lineageAnchorScript) ReleaseOutboundAnchors(
	[]documentstore.OutboundAnchorFinalization,
) {
}

func (s *lineageAnchorScript) ReplaceOutboundAnchors(
	_ context.Context,
	sets []documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	s.sets = append(s.sets, sets...)

	return s.update, s.err
}

type lineageClusterScript struct {
	assignment   contentcluster.Assignment
	lookupFound  bool
	lookupErr    error
	deleteErr    error
	cluster      contentcluster.Cluster
	clusterFound bool
	clusterErr   error
	deleted      []string
}

func (s *lineageClusterScript) Lookup(
	context.Context,
	string,
) (contentcluster.Assignment, bool, error) {
	return s.assignment, s.lookupFound, s.lookupErr
}

func (s *lineageClusterScript) Delete(
	_ context.Context,
	url string,
) (bool, error) {
	s.deleted = append(s.deleted, url)

	return s.deleteErr == nil, s.deleteErr
}

func (s *lineageClusterScript) Cluster(
	context.Context,
	string,
) (contentcluster.Cluster, bool, error) {
	return s.cluster, s.clusterFound, s.clusterErr
}

type lineageIndexScript struct {
	docs      []documentstore.Document
	deleted   []string
	err       error
	deleteErr error
}

func (s *lineageIndexScript) Index(
	_ context.Context,
	doc documentstore.Document,
) error {
	s.docs = append(s.docs, doc)

	return s.err
}

func (s *lineageIndexScript) Delete(_ context.Context, url string) error {
	s.deleted = append(s.deleted, url)

	return s.deleteErr
}

type lineageBatchIndexScript struct {
	lineageIndexScript
	batches int
}

func (s *lineageBatchIndexScript) IndexBatch(
	_ context.Context,
	docs []documentstore.Document,
) error {
	s.batches++
	s.docs = append(s.docs, docs...)

	return s.err
}

func TestDocumentLineageEvictorRefreshesAnchorsAndCluster(t *testing.T) {
	sourceURL := "https://source.example/"
	memberURL := "https://member.example/"
	currentURL := "https://current.example/"
	targetURL := "https://target.example/"
	documents := &lineageDocumentScript{
		docs: map[string]documentstore.Document{
			sourceURL: {NormalizedURL: sourceURL},
			memberURL: {
				NormalizedURL:     memberURL,
				RepresentativeURL: sourceURL,
			},
			currentURL: {
				NormalizedURL:     currentURL,
				ClusterID:         "cluster",
				RepresentativeURL: memberURL,
			},
		},
		commitReceipt: true,
	}
	anchors := &lineageAnchorScript{
		documents: []documentstore.Document{{
			NormalizedURL: targetURL,
			Title:         "anchor refresh",
		}},
	}
	clusters := &lineageClusterScript{
		assignment:  contentcluster.Assignment{ClusterID: "cluster"},
		lookupFound: true,
		cluster: contentcluster.Cluster{
			ID:                "cluster",
			RepresentativeURL: memberURL,
			MemberURLs:        []string{currentURL, "https://missing.example/", memberURL},
		},
		clusterFound: true,
	}
	index := &lineageIndexScript{}
	evictor := documentLineageEvictor{
		directory: documents,
		receiver:  documents,
		documents: documents,
		anchors:   anchors,
		clusters:  clusters,
		index:     index,
	}

	removed, err := evictor.Delete(t.Context(), sourceURL)
	if err != nil || !removed {
		t.Fatalf("delete lineage = %v, %v", removed, err)
	}
	if len(anchors.sets) != 1 || anchors.sets[0].SourceURL != sourceURL ||
		len(anchors.sets[0].Anchors) != 0 || anchors.finalizeCalls != 1 {
		t.Fatalf("anchor cleanup = %#v", anchors.sets)
	}
	if len(clusters.deleted) != 1 || len(documents.deleted) != 1 ||
		len(index.deleted) != 1 || index.deleted[0] != sourceURL {
		t.Fatalf("deletions = %#v / %#v", clusters.deleted, documents.deleted)
	}
	if len(documents.received) != 2 || len(index.docs) != 3 {
		t.Fatalf("refreshes = %#v / %#v", documents.received, index.docs)
	}
	member := documents.docs[memberURL]
	if member.ClusterID != "cluster" || member.RepresentativeURL != memberURL {
		t.Fatalf("surviving member = %#v", member)
	}
	if index.docs[0].NormalizedURL != targetURL || index.docs[0].Title != "anchor refresh" {
		t.Fatalf("anchor target = %#v", index.docs[0])
	}
}

func TestDocumentLineageEvictorReemitsSurvivorsAfterPartialIndexRetry(t *testing.T) {
	source := "https://source.example/"
	first := "https://member.example/first"
	second := "https://member.example/second"
	documents := &lineageDocumentScript{
		docs: map[string]documentstore.Document{
			source: {NormalizedURL: source, ClusterID: "cluster"},
			first: {
				NormalizedURL:     first,
				ClusterID:         "cluster",
				RepresentativeURL: first,
			},
			second: {
				NormalizedURL:     second,
				ClusterID:         "cluster",
				RepresentativeURL: first,
			},
		},
		commitReceipt: true,
	}
	clusters := &lineageClusterScript{
		assignment:  contentcluster.Assignment{ClusterID: "cluster"},
		lookupFound: true,
		cluster: contentcluster.Cluster{
			ID:                "cluster",
			RepresentativeURL: first,
			MemberURLs:        []string{source, first, second},
		},
		clusterFound: true,
	}
	indexFailure := errors.New("partial index failure")
	index := &lineageIndexScript{err: indexFailure}
	evictor := documentLineageEvictor{
		directory: documents,
		receiver:  documents,
		documents: documents,
		clusters:  clusters,
		index:     index,
	}
	if removed, err := evictor.Delete(t.Context(), source); err == nil || removed {
		t.Fatalf("partial delete = %t, %v", removed, err)
	}
	if len(index.docs) != 1 || len(documents.deleted) != 0 {
		t.Fatalf("partial index/deletes = %#v/%#v", index.docs, documents.deleted)
	}
	clusters.lookupFound = false
	index.err = nil
	removed, err := evictor.Delete(t.Context(), source)
	if err != nil || !removed {
		t.Fatalf("retried delete = %t, %v", removed, err)
	}
	if len(index.docs) != 3 || index.docs[1].NormalizedURL != first ||
		index.docs[2].NormalizedURL != second {
		t.Fatalf("retried survivor index = %#v", index.docs)
	}
}

type lineageFailureCase struct {
	name    string
	evictor documentLineageEvictor
}

func lineageClusterWithMember() *lineageClusterScript {
	return &lineageClusterScript{
		assignment:  contentcluster.Assignment{ClusterID: "cluster"},
		lookupFound: true,
		cluster: contentcluster.Cluster{
			ID:                "cluster",
			RepresentativeURL: "https://member.example/",
			MemberURLs:        []string{"https://member.example/"},
		},
		clusterFound: true,
	}
}

func lineageDocumentsWithMember() *lineageDocumentScript {
	return &lineageDocumentScript{docs: map[string]documentstore.Document{
		"https://source.example/": {NormalizedURL: "https://source.example/"},
		"https://member.example/": {NormalizedURL: "https://member.example/"},
	}}
}

func assertLineageFailures(t *testing.T, tests []lineageFailureCase) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.evictor.Delete(t.Context(), "https://source.example/"); err == nil {
				t.Fatal("dependency failure succeeded")
			}
		})
	}
}

func TestDocumentLineageEvictorSurfacesAnchorAndPersistenceFailures(t *testing.T) {
	sentinel := errors.New("failure")
	assertLineageFailures(t, []lineageFailureCase{
		{
			name: "anchor store",
			evictor: documentLineageEvictor{
				documents: lineageDocumentsWithMember(),
				anchors:   &lineageAnchorScript{err: sentinel},
			},
		},
		{
			name: "anchor capacity",
			evictor: documentLineageEvictor{
				documents: lineageDocumentsWithMember(),
				anchors: &lineageAnchorScript{update: documentstore.AnchorUpdate{
					Busy: true,
				}},
			},
		},
		{
			name: "anchor finalization",
			evictor: documentLineageEvictor{
				documents: lineageDocumentsWithMember(),
				anchors: &lineageAnchorScript{
					finalizeErr: sentinel,
				},
			},
		},
		{
			name: "document store",
			evictor: documentLineageEvictor{
				directory: lineageDocumentsWithMember(),
				receiver:  &lineageDocumentScript{receiveErr: sentinel},
				documents: lineageDocumentsWithMember(),
				clusters:  lineageClusterWithMember(),
			},
		},
		{
			name: "document capacity",
			evictor: documentLineageEvictor{
				directory: lineageDocumentsWithMember(),
				receiver: &lineageDocumentScript{receipt: documentstore.Receipt{
					Busy: true,
				}},
				documents: lineageDocumentsWithMember(),
				clusters:  lineageClusterWithMember(),
			},
		},
		{
			name: "index update",
			evictor: documentLineageEvictor{
				directory: lineageDocumentsWithMember(),
				documents: lineageDocumentsWithMember(),
				clusters:  lineageClusterWithMember(),
				index:     &lineageIndexScript{err: sentinel},
			},
		},
		{
			name: "document delete",
			evictor: documentLineageEvictor{
				documents: &lineageDocumentScript{
					docs:      map[string]documentstore.Document{},
					deleteErr: sentinel,
				},
			},
		},
	})
}

func TestDocumentLineageEvictorSurfacesClusterFailures(t *testing.T) {
	sentinel := errors.New("failure")
	assertLineageFailures(t, []lineageFailureCase{
		{
			name: "cluster lookup",
			evictor: documentLineageEvictor{
				documents: lineageDocumentsWithMember(),
				clusters:  &lineageClusterScript{lookupErr: sentinel},
			},
		},
		{
			name: "cluster delete",
			evictor: documentLineageEvictor{
				documents: lineageDocumentsWithMember(),
				clusters:  &lineageClusterScript{deleteErr: sentinel},
			},
		},
		{
			name: "cluster read",
			evictor: documentLineageEvictor{
				documents: lineageDocumentsWithMember(),
				clusters: &lineageClusterScript{
					assignment:  contentcluster.Assignment{ClusterID: "cluster"},
					lookupFound: true,
					clusterErr:  sentinel,
				},
			},
		},
		{
			name: "document read",
			evictor: documentLineageEvictor{
				directory: &lineageDocumentScript{readErr: sentinel},
				documents: lineageDocumentsWithMember(),
				clusters:  lineageClusterWithMember(),
			},
		},
	})
}

func TestDocumentLineageEvictorHandlesOptionalAndMissingState(t *testing.T) {
	documents := &lineageDocumentScript{docs: map[string]documentstore.Document{
		"https://source.example/": {NormalizedURL: "https://source.example/"},
	}}
	evictor := documentLineageEvictor{documents: documents}
	removed, err := evictor.Delete(t.Context(), "https://source.example/")
	if err != nil || !removed {
		t.Fatalf("optional cleanup = %v, %v", removed, err)
	}

	if err := evictor.commitUpdates(t.Context(), nil); err != nil {
		t.Fatalf("empty updates: %v", err)
	}
	if err := evictor.commitUpdates(t.Context(), map[string]documentstore.Document{
		"https://updated.example/": {NormalizedURL: "https://updated.example/"},
	}); err != nil {
		t.Fatalf("optional update persistence: %v", err)
	}
}

func TestDocumentLineageEvictorIndexesLineageUpdatesInOneBatch(t *testing.T) {
	index := &lineageBatchIndexScript{}
	evictor := documentLineageEvictor{index: index}
	err := evictor.commitUpdates(t.Context(), map[string]documentstore.Document{
		"https://b.example/": {NormalizedURL: "https://b.example/"},
		"https://a.example/": {NormalizedURL: "https://a.example/"},
	})
	if err != nil || index.batches != 1 || len(index.docs) != 2 ||
		index.docs[0].NormalizedURL != "https://a.example/" {
		t.Fatalf("batch lineage update = %v, %d, %#v", err, index.batches, index.docs)
	}
}

func TestNewDocumentLineageEvictorRequiresDocumentDeletion(t *testing.T) {
	if got := newDocumentLineageEvictor(nodeStorage{}); got != nil {
		t.Fatalf("empty storage evictor = %#v", got)
	}
	documents := &lineageDocumentScript{docs: map[string]documentstore.Document{}}
	got := newDocumentLineageEvictor(nodeStorage{
		documentDirectory: documents,
		documentReceiver:  documents,
	})
	if got == nil {
		t.Fatal("document lineage evictor missing")
	}
}
