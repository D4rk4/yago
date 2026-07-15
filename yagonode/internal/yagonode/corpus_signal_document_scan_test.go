package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type pagedCorpusProbe struct {
	document       documentstore.Document
	pagedScans     int
	legacyScans    int
	pagedScanError error
}

func (p *pagedCorpusProbe) StoredDocuments(
	context.Context,
	func(documentstore.Document) (bool, error),
) error {
	p.legacyScans++

	return nil
}

func (p *pagedCorpusProbe) ScanStoredDocumentPages(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	p.pagedScans++
	if p.pagedScanError != nil {
		return p.pagedScanError
	}
	_, err := visit(p.document)

	return err
}

func TestCorpusSignalDocumentScanPrefersPagedSource(t *testing.T) {
	probe := &pagedCorpusProbe{document: documentstore.Document{Title: "paged"}}
	visited := ""
	err := scanCorpusSignalDocuments(
		t.Context(),
		probe,
		func(document documentstore.Document) (bool, error) {
			visited = document.Title

			return true, nil
		},
	)
	if err != nil || visited != "paged" || probe.pagedScans != 1 || probe.legacyScans != 0 {
		t.Fatalf("paged scan = %v, %q, %#v", err, visited, probe)
	}

	sentinel := errors.New("paged failure")
	probe.pagedScanError = sentinel
	if err := scanCorpusSignalDocuments(
		t.Context(),
		probe,
		func(documentstore.Document) (bool, error) {
			return true, nil
		},
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("paged scan error = %v", err)
	}
}

func TestCorpusSignalDocumentScanFallsBackToStoredDocuments(t *testing.T) {
	corpus := &countedCorpus{documents: corpusSignalDocuments()}
	visited := 0
	err := scanCorpusSignalDocuments(
		t.Context(),
		corpus,
		func(documentstore.Document) (bool, error) {
			visited++

			return true, nil
		},
	)
	if err != nil || visited != len(corpus.documents) || corpus.scans.Load() != 1 {
		t.Fatalf("legacy scan = %v, %d, %d", err, visited, corpus.scans.Load())
	}
	sentinel := errors.New("legacy failure")
	corpus.err = sentinel
	if err := scanCorpusSignalDocuments(
		t.Context(),
		corpus,
		func(documentstore.Document) (bool, error) { return true, nil },
	); !errors.Is(err, sentinel) {
		t.Fatalf("legacy scan error = %v", err)
	}
}

var _ documentstore.StoredDocumentPageScanner = (*pagedCorpusProbe)(nil)
