package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

type fakeTermPostings struct {
	count    int
	postings []yagomodel.RWIPosting
	countErr error
	scanErr  error
}

func (f fakeTermPostings) RWICount(context.Context) (int, error) { return 0, nil }

func (f fakeTermPostings) RWIURLCount(context.Context, yagomodel.Hash) (int, error) {
	return f.count, f.countErr
}

func (f fakeTermPostings) ScanWord(
	_ context.Context,
	_ yagomodel.Hash,
	visit func(yagomodel.RWIPosting) (bool, error),
) error {
	for _, posting := range f.postings {
		cont, err := visit(posting)
		if err != nil || !cont {
			return err
		}
	}

	return f.scanErr
}

type fakeTermURLs struct {
	rows    []yagomodel.URIMetadataRow
	rowsErr error
}

func (f fakeTermURLs) RowsByHash(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.URIMetadataRow, error) {
	return f.rows, f.rowsErr
}

func (f fakeTermURLs) MissingURLs(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return nil, nil
}

func (f fakeTermURLs) Count(context.Context) (int, error) { return len(f.rows), nil }

func termPosting(urlHash string) yagomodel.RWIPosting {
	return yagomodel.RWIPosting{Properties: map[string]string{yagomodel.ColURLHash: urlHash}}
}

func termRow(rawURL, title string) yagomodel.URIMetadataRow {
	return yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaURL:            yagomodel.EncodeBase64WireForm(rawURL),
		yagomodel.URLMetaColDescription: yagomodel.EncodeBase64WireForm(title),
	}}
}

func TestTermSourceLookupResolvesSample(t *testing.T) {
	postings := fakeTermPostings{
		count: 2,
		postings: []yagomodel.RWIPosting{
			termPosting("MNOPQRSTUVWX"),
			termPosting("0123456789AB"),
		},
	}
	urls := fakeTermURLs{rows: []yagomodel.URIMetadataRow{
		termRow("http://a.example/1", "Alpha"),
		termRow("http://b.example/2", "Beta"),
	}}
	source := newTermSource(postings, urls)

	report := source.LookupTerm(context.Background(), "golang")
	if report.Count != 2 || report.NotFound || report.Error != nil || report.SampleError != nil {
		t.Fatalf("report = %+v", report)
	}
	if report.Hash == "" {
		t.Fatal("term hash not set")
	}
	if len(report.Sample) != 2 {
		t.Fatalf("sample = %+v", report.Sample)
	}
	if report.Sample[0].URL != "http://a.example/1" || report.Sample[0].Title != "Alpha" {
		t.Fatalf("first sample = %+v", report.Sample[0])
	}
}

func TestTermSourceLookupKeepsPartialSampleError(t *testing.T) {
	scanFailure := errors.New("scan stopped")
	rowsFailure := errors.New("rows stopped")
	postings := fakeTermPostings{
		count:    3,
		postings: []yagomodel.RWIPosting{termPosting("MNOPQRSTUVWX")},
		scanErr:  scanFailure,
	}
	urls := fakeTermURLs{
		rows:    []yagomodel.URIMetadataRow{termRow("http://a.example/1", "Alpha")},
		rowsErr: rowsFailure,
	}
	report := newTermSource(postings, urls).LookupTerm(context.Background(), "golang")
	if report.Error != nil || len(report.Sample) != 1 ||
		!errors.Is(report.SampleError, scanFailure) ||
		!errors.Is(report.SampleError, rowsFailure) {
		t.Fatalf("partial report = %+v", report)
	}
}

func TestTermSourceSampleOnlyCountsResolvedDocuments(t *testing.T) {
	postings := fakeTermPostings{
		count: 2,
		postings: []yagomodel.RWIPosting{
			termPosting("MNOPQRSTUVWX"),
			termPosting("0123456789AB"),
		},
	}
	urls := fakeTermURLs{rows: []yagomodel.URIMetadataRow{
		termRow("http://a.example/1", "Alpha"),
		{Properties: map[string]string{yagomodel.URLMetaURL: "x|invalid"}},
	}}
	report := newTermSource(postings, urls).LookupTerm(context.Background(), "golang")
	if report.SampleError != nil || len(report.Sample) != 1 {
		t.Fatalf("resolved sample = %+v", report)
	}
}

func TestTermSourceEmptySuccessfulSample(t *testing.T) {
	report := newTermSource(
		fakeTermPostings{count: 1},
		fakeTermURLs{},
	).LookupTerm(context.Background(), "golang")
	if report.SampleError != nil || len(report.Sample) != 0 {
		t.Fatalf("empty sample = %+v", report)
	}
}

func TestTermSourceNotFound(t *testing.T) {
	source := newTermSource(fakeTermPostings{count: 0}, fakeTermURLs{})
	report := source.LookupTerm(context.Background(), "absent")
	if !report.NotFound || report.Count != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestTermSourceEmptyTerm(t *testing.T) {
	source := newTermSource(fakeTermPostings{}, fakeTermURLs{})
	report := source.LookupTerm(context.Background(), "")
	if report.Count != 0 || report.Hash != "" || report.NotFound {
		t.Fatalf("empty term report = %+v", report)
	}
}

func TestNewTermSourceNilWhenStorageMissing(t *testing.T) {
	if newTermSource(nil, nil) != nil {
		t.Fatal("expected a nil term source without storage")
	}
}

func TestIndexSchemaGroupsNonEmpty(t *testing.T) {
	groups := indexSchemaGroups()
	if len(groups) == 0 {
		t.Fatal("no schema groups")
	}
	for _, group := range groups {
		if group.Title == "" || len(group.Fields) == 0 {
			t.Fatalf("empty schema group: %+v", group)
		}
	}
}
