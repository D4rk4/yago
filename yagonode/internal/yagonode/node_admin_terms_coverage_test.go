package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

type fakePostingIndex struct {
	count    int
	countErr error
	scanErr  error
	postings []yagomodel.RWIPosting
}

func (fakePostingIndex) RWICount(context.Context) (int, error) { return 0, nil }

func (f fakePostingIndex) RWIURLCount(context.Context, yagomodel.Hash) (int, error) {
	return f.count, f.countErr
}

func (f fakePostingIndex) ScanWord(
	_ context.Context,
	_ yagomodel.Hash,
	visit func(yagomodel.RWIPosting) (bool, error),
) error {
	for _, posting := range f.postings {
		more, err := visit(posting)
		if err != nil || !more {
			return err
		}
	}

	return f.scanErr
}

type fakeURLDirectory struct {
	rows    []yagomodel.URIMetadataRow
	rowsErr error
}

func (f fakeURLDirectory) RowsByHash(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.URIMetadataRow, error) {
	return f.rows, f.rowsErr
}

func (fakeURLDirectory) MissingURLs(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return nil, nil
}

func (fakeURLDirectory) Count(context.Context) (int, error) { return 0, nil }

func TestTermSourceLookupCountError(t *testing.T) {
	failure := errors.New("boom")
	src := newTermSource(fakePostingIndex{countErr: failure}, fakeURLDirectory{})
	report := src.LookupTerm(context.Background(), "hello")
	if !errors.Is(report.Error, failure) {
		t.Fatalf("expected a lookup error, got %+v", report)
	}
}

func TestTermSourceSampleScanError(t *testing.T) {
	failure := errors.New("scan")
	src := newTermSource(
		fakePostingIndex{count: 5, scanErr: failure},
		fakeURLDirectory{},
	)
	report := src.LookupTerm(context.Background(), "hello")
	if report.Count != 5 || report.Sample != nil || !errors.Is(report.SampleError, failure) {
		t.Fatalf("scan failure should yield no sample: %+v", report)
	}
}

func TestTermSourceSampleRowsError(t *testing.T) {
	failure := errors.New("rows")
	src := newTermSource(
		fakePostingIndex{
			count:    5,
			postings: []yagomodel.RWIPosting{termPosting("MNOPQRSTUVWX")},
		},
		fakeURLDirectory{rowsErr: failure},
	)
	report := src.LookupTerm(context.Background(), "hello")
	if report.Count != 5 || len(report.Sample) != 0 ||
		!errors.Is(report.SampleError, failure) {
		t.Fatalf("rows failure should yield no sample: %+v", report)
	}
}
