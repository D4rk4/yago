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
}

func (fakePostingIndex) RWICount(context.Context) (int, error) { return 0, nil }

func (f fakePostingIndex) RWIURLCount(context.Context, yagomodel.Hash) (int, error) {
	return f.count, f.countErr
}

func (f fakePostingIndex) ScanWord(
	context.Context,
	yagomodel.Hash,
	func(yagomodel.RWIPosting) (bool, error),
) error {
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
	src := newTermSource(fakePostingIndex{countErr: errors.New("boom")}, fakeURLDirectory{})
	report := src.LookupTerm(context.Background(), "hello")
	if report.Error == "" {
		t.Fatalf("expected a lookup error, got %+v", report)
	}
}

func TestTermSourceSampleScanError(t *testing.T) {
	src := newTermSource(
		fakePostingIndex{count: 5, scanErr: errors.New("scan")},
		fakeURLDirectory{},
	)
	report := src.LookupTerm(context.Background(), "hello")
	if report.Count != 5 || report.Sample != nil {
		t.Fatalf("scan failure should yield no sample: %+v", report)
	}
}

func TestTermSourceSampleRowsError(t *testing.T) {
	src := newTermSource(
		fakePostingIndex{count: 5},
		fakeURLDirectory{rowsErr: errors.New("rows")},
	)
	report := src.LookupTerm(context.Background(), "hello")
	if report.Count != 5 || report.Sample != nil {
		t.Fatalf("rows failure should yield no sample: %+v", report)
	}
}
