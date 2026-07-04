package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
)

type fakeSearchDeleter struct {
	deleted []string
	err     error
}

func (f *fakeSearchDeleter) Delete(_ context.Context, docID string) error {
	f.deleted = append(f.deleted, docID)

	return f.err
}

type fakeDocDeleter struct {
	deleted []string
	err     error
}

func (f *fakeDocDeleter) Delete(_ context.Context, normalizedURL string) (bool, error) {
	f.deleted = append(f.deleted, normalizedURL)

	return f.err == nil, f.err
}

type fakeURLEvictor struct {
	evicted [][]yagomodel.Hash
	err     error
}

func (f *fakeURLEvictor) EvictURLs(
	_ context.Context,
	urls []yagomodel.Hash,
) (eviction.Result, error) {
	f.evicted = append(f.evicted, urls)
	if f.err != nil {
		return eviction.Result{}, f.err
	}

	return eviction.Result{URLsDeleted: len(urls)}, nil
}

func newTestController(
	index *fakeSearchDeleter,
	docs *fakeDocDeleter,
	evict *fakeURLEvictor,
	stored documentstore.StoredDocuments,
) *indexAdminController {
	return &indexAdminController{
		index:     index,
		documents: docs,
		stored:    stored,
		evictor:   evict,
		hashURL:   yagomodel.HashURL,
	}
}

func TestDeleteDocumentRemovesFromEveryLineage(t *testing.T) {
	index := &fakeSearchDeleter{}
	docs := &fakeDocDeleter{}
	evict := &fakeURLEvictor{}
	ctrl := newTestController(index, docs, evict, nil)

	if err := ctrl.DeleteDocument(context.Background(), "https://a.example/1"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if len(index.deleted) != 1 || index.deleted[0] != "https://a.example/1" {
		t.Fatalf("index deletes = %v", index.deleted)
	}
	if len(docs.deleted) != 1 {
		t.Fatalf("document deletes = %v", docs.deleted)
	}
	if len(evict.evicted) != 1 || len(evict.evicted[0]) != 1 {
		t.Fatalf("evictions = %v", evict.evicted)
	}
}

func TestDeleteDocumentEmptyKeyIsNoOp(t *testing.T) {
	index := &fakeSearchDeleter{}
	ctrl := newTestController(index, &fakeDocDeleter{}, &fakeURLEvictor{}, nil)

	if err := ctrl.DeleteDocument(context.Background(), "   "); err != nil {
		t.Fatalf("empty key should be a no-op: %v", err)
	}
	if len(index.deleted) != 0 {
		t.Fatal("nothing should be deleted for an empty key")
	}
}

func TestDeleteDocumentSurfacesErrors(t *testing.T) {
	cases := []struct {
		name string
		ctrl *indexAdminController
	}{
		{
			"index",
			newTestController(
				&fakeSearchDeleter{err: errors.New("x")},
				&fakeDocDeleter{},
				&fakeURLEvictor{},
				nil,
			),
		},
		{
			"document",
			newTestController(
				&fakeSearchDeleter{},
				&fakeDocDeleter{err: errors.New("x")},
				&fakeURLEvictor{},
				nil,
			),
		},
		{
			"evict",
			newTestController(
				&fakeSearchDeleter{},
				&fakeDocDeleter{},
				&fakeURLEvictor{err: errors.New("x")},
				nil,
			),
		},
	}
	for _, tc := range cases {
		if err := tc.ctrl.DeleteDocument(context.Background(), "https://a.example/1"); err == nil {
			t.Fatalf("%s failure should be surfaced", tc.name)
		}
	}
}

func TestDeleteDocumentSkipsEvictionForUnhashableURL(t *testing.T) {
	evict := &fakeURLEvictor{}
	ctrl := newTestController(&fakeSearchDeleter{}, &fakeDocDeleter{}, evict, nil)
	ctrl.hashURL = func(string) (yagomodel.URLHash, error) {
		return "", errors.New("cannot hash")
	}

	if err := ctrl.DeleteDocument(context.Background(), "https://a.example/1"); err != nil {
		t.Fatalf("an unhashable URL should still delete the document parts: %v", err)
	}
	if len(evict.evicted) != 0 {
		t.Fatal("no posting eviction should run when the URL hash cannot be derived")
	}
}

func TestDeleteDocumentToleratesNilDocumentDeleter(t *testing.T) {
	ctrl := &indexAdminController{
		index:   &fakeSearchDeleter{},
		evictor: &fakeURLEvictor{},
		hashURL: yagomodel.HashURL,
	}

	if err := ctrl.DeleteDocument(context.Background(), "https://a.example/1"); err != nil {
		t.Fatalf("a nil document deleter should be tolerated: %v", err)
	}
}

func TestDeleteDomainDeletesMatchingHosts(t *testing.T) {
	stored := fakeStoredDocuments{docs: []documentstore.Document{
		{NormalizedURL: "https://a.example/1", CanonicalURL: "https://a.example/1"},
		{NormalizedURL: "https://b.example/2", CanonicalURL: "https://b.example/2"},
		{NormalizedURL: "https://sub.a.example/3", CanonicalURL: "https://sub.a.example/3"},
	}}
	index := &fakeSearchDeleter{}
	ctrl := newTestController(index, &fakeDocDeleter{}, &fakeURLEvictor{}, stored)

	deleted, err := ctrl.DeleteDomain(context.Background(), "a.example")
	if err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}
	if deleted != 2 || len(index.deleted) != 2 {
		t.Fatalf("deleted = %d, index = %v; want 2 (host + subdomain)", deleted, index.deleted)
	}
}

func TestDeleteDomainEmptyIsNoOp(t *testing.T) {
	ctrl := newTestController(
		&fakeSearchDeleter{},
		&fakeDocDeleter{},
		&fakeURLEvictor{},
		fakeStoredDocuments{},
	)

	if deleted, err := ctrl.DeleteDomain(context.Background(), "  "); err != nil || deleted != 0 {
		t.Fatalf("empty domain = %d, %v; want no-op", deleted, err)
	}
}

func TestDeleteDomainSurfacesScanError(t *testing.T) {
	stored := fakeStoredDocuments{err: errors.New("scan failed")}
	ctrl := newTestController(&fakeSearchDeleter{}, &fakeDocDeleter{}, &fakeURLEvictor{}, stored)

	if _, err := ctrl.DeleteDomain(context.Background(), "a.example"); err == nil {
		t.Fatal("a scan error should be surfaced")
	}
}

func TestDeleteDomainStopsOnDeleteError(t *testing.T) {
	stored := fakeStoredDocuments{docs: []documentstore.Document{
		{NormalizedURL: "https://a.example/1", CanonicalURL: "https://a.example/1"},
	}}
	ctrl := newTestController(
		&fakeSearchDeleter{err: errors.New("x")}, &fakeDocDeleter{}, &fakeURLEvictor{}, stored,
	)

	if _, err := ctrl.DeleteDomain(context.Background(), "a.example"); err == nil {
		t.Fatal("a delete failure mid-domain should be surfaced")
	}
}
