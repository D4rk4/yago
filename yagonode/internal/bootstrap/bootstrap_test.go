package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingSeedImportObserver struct {
	imports int
	seeds   int
}

func (o *recordingSeedImportObserver) ObserveSeedlistImport(seedCount int) {
	o.imports++
	o.seeds += seedCount
}

func TestSeedlistImporterFetchesOneURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = strings.NewReader(seedlistLine(t, "BBBBBBBBBBBB", "203.0.113.2")).WriteTo(w)
	}))
	defer server.Close()

	importer := NewSeedlistImporter(server.Client())
	seeds, err := importer.Import(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(seeds) != 1 {
		t.Fatalf("got %d seeds, want 1", len(seeds))
	}
}

func TestSeedlistsFetchAllURLsAndSkipFailures(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = strings.NewReader(seedlistLine(t, "AAAAAAAAAAAA", "203.0.113.1")).WriteTo(w)
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer bad.Close()

	observer := &recordingSeedImportObserver{}
	source := NewObserved(good.Client(), []string{good.URL, bad.URL}, observer)
	seeds := source.Fetch(context.Background())

	if len(seeds) != 1 {
		t.Fatalf("got %d seeds, want 1 (failed source skipped)", len(seeds))
	}
	if observer.imports != 1 || observer.seeds != 1 {
		t.Fatalf(
			"imports = %d/%d, want one successful seedlist with one seed",
			observer.imports,
			observer.seeds,
		)
	}
}

func TestSeedlistsDefaultObserver(t *testing.T) {
	source := New(http.DefaultClient, nil)
	if seeds := source.Fetch(context.Background()); seeds != nil {
		t.Fatalf("seeds = %v, want nil", seeds)
	}
}
