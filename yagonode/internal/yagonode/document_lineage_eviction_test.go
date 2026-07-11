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
	update documentstore.AnchorUpdate
	err    error
	sets   []documentstore.OutboundAnchorSet
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
	docs []documentstore.Document
	err  error
}

func (s *lineageIndexScript) Index(
	_ context.Context,
	doc documentstore.Document,
) error {
	s.docs = append(s.docs, doc)

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
		update: documentstore.AnchorUpdate{
			Documents: []documentstore.Document{{
				NormalizedURL: targetURL,
				Title:         "anchor refresh",
			}},
		},
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
		len(anchors.sets[0].Anchors) != 0 {
		t.Fatalf("anchor cleanup = %#v", anchors.sets)
	}
	if len(clusters.deleted) != 1 || len(documents.deleted) != 1 {
		t.Fatalf("deletions = %#v / %#v", clusters.deleted, documents.deleted)
	}
	if len(documents.received) != 2 || len(index.docs) != 2 {
		t.Fatalf("refreshes = %#v / %#v", documents.received, index.docs)
	}
	member := documents.docs[memberURL]
	if member.ClusterID != "cluster" || member.RepresentativeURL != memberURL {
		t.Fatalf("surviving member = %#v", member)
	}
	if documents.docs[targetURL].Title != "anchor refresh" {
		t.Fatalf("anchor target = %#v", documents.docs[targetURL])
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

	missing := &lineageClusterScript{}
	evictor = documentLineageEvictor{documents: documents, clusters: missing}
	if updates, err := evictor.deleteContentCluster(
		t.Context(),
		"https://missing.example/",
	); err != nil || updates != nil {
		t.Fatalf("missing assignment = %#v, %v", updates, err)
	}
	missing.lookupFound = true
	missing.assignment.ClusterID = "gone"
	if updates, err := evictor.deleteContentCluster(
		t.Context(),
		"https://missing.example/",
	); err != nil || updates != nil {
		t.Fatalf("missing cluster = %#v, %v", updates, err)
	}
	missing.clusterFound = true
	missing.cluster = contentcluster.Cluster{ID: "gone"}
	if updates, err := evictor.deleteContentCluster(
		t.Context(),
		"https://missing.example/",
	); err != nil || updates != nil {
		t.Fatalf("missing directory = %#v, %v", updates, err)
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
