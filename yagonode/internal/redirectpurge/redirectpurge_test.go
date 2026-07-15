package redirectpurge

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type fakeCorpus struct{ docs []documentstore.Document }

func (f fakeCorpus) StoredDocuments(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	for _, doc := range f.docs {
		if ok, err := visit(doc); !ok || err != nil {
			return err
		}
	}

	return nil
}

type fakeLineagePurger struct {
	purged []string
	hashes []yagomodel.Hash
	fail   map[string]bool
}

func (f *fakeLineagePurger) PurgeResolved(
	_ context.Context,
	urls []string,
	hashes []yagomodel.Hash,
) error {
	if len(urls) == 0 {
		return nil
	}
	if f.fail[urls[0]] {
		return errors.New("lineage busy")
	}
	f.purged = append(f.purged, urls...)
	f.hashes = append(f.hashes, hashes...)

	return nil
}

// TestSweepRemovesTrackingRedirects pins SEARCH-29: bing /ck/ documents leave
// both the index and the vault, real destinations stay, and a document whose
// canonical URL is the redirect is condemned even with a clean normalized URL.
func TestSweepRemovesTrackingRedirects(t *testing.T) {
	corpus := fakeCorpus{docs: []documentstore.Document{
		{NormalizedURL: "https://www.bing.com/ck/a?u=a1aHR0cHM6Ly9leGFtcGxlLm9yZw"},
		{NormalizedURL: "https://example.org/page"},
		{NormalizedURL: "https://kept.example/x", CanonicalURL: "https://bing.com/ck/a?u=a1x"},
		{NormalizedURL: "https://bingo.combo.example/ck/a"},
	}}
	purger := &fakeLineagePurger{}

	New(corpus, purger.PurgeResolved).Run(context.Background())

	want := []string{
		"https://www.bing.com/ck/a?u=a1aHR0cHM6Ly9leGFtcGxlLm9yZw",
		"https://kept.example/x",
	}
	if len(purger.purged) != 2 || purger.purged[0] != want[0] ||
		purger.purged[1] != want[1] || len(purger.hashes) != 2 {
		t.Fatalf("lineage purges = %v / %v", purger.purged, purger.hashes)
	}
}

func TestSweepSurvivesPerDocumentFailures(t *testing.T) {
	first := "https://bing.com/ck/a?u=a1one"
	second := "https://bing.com/ck/b?u=a1two"
	corpus := fakeCorpus{docs: []documentstore.Document{
		{NormalizedURL: first}, {NormalizedURL: second},
	}}
	purger := &fakeLineagePurger{fail: map[string]bool{first: true}}

	New(corpus, purger.PurgeResolved).Run(context.Background())

	if len(purger.purged) != 1 || purger.purged[0] != second {
		t.Fatalf("failed lineage purge must not stop the pass: %v", purger.purged)
	}
}

func TestSweepSkipsFailedLineagePurge(t *testing.T) {
	target := "https://bing.com/ck/a?u=a1one"
	corpus := fakeCorpus{docs: []documentstore.Document{{NormalizedURL: target}}}
	purger := &fakeLineagePurger{fail: map[string]bool{target: true}}

	New(corpus, purger.PurgeResolved).Run(context.Background())

	if len(purger.purged) != 0 {
		t.Fatalf("a failing lineage purge must record nothing: %v", purger.purged)
	}
}

func TestSweepSkipsUnhashableRedirect(t *testing.T) {
	target := "https://bing.com/ck/a?u=a1one"
	purger := &fakeLineagePurger{}
	sweeper := New(
		fakeCorpus{docs: []documentstore.Document{{NormalizedURL: target}}},
		purger.PurgeResolved,
	)
	sweeper.hashURL = func(string) (yagomodel.URLHash, error) {
		return "", errors.New("hash failed")
	}
	sweeper.Run(t.Context())
	if len(purger.purged) != 0 {
		t.Fatalf("unhashable redirect purge = %v", purger.purged)
	}
}

// TestSweepLogsCancelledCorpusScan pins that a context cancelled mid-scan aborts
// the corpus walk through the visitor's own guard and leaves the store
// untouched rather than deleting a partial set.
func TestSweepLogsCancelledCorpusScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	corpus := fakeCorpus{docs: []documentstore.Document{
		{NormalizedURL: "https://bing.com/ck/a?u=a1one"},
	}}
	purger := &fakeLineagePurger{}

	New(corpus, purger.PurgeResolved).Run(ctx)

	if len(purger.purged) != 0 {
		t.Fatalf("a cancelled scan must delete nothing: %v", purger.purged)
	}
}

type cancellingLineagePurger struct {
	cancel context.CancelFunc
	purged []string
}

func (c *cancellingLineagePurger) PurgeResolved(
	_ context.Context,
	urls []string,
	_ []yagomodel.Hash,
) error {
	c.cancel()
	c.purged = append(c.purged, urls...)

	return nil
}

// TestSweepAbortsDeletesOnCancel pins the shutdown contract behind
// SERVE-LIFECYCLE-01: once the context is cancelled, Run returns between
// documents instead of finishing the delete pass, so a shutdown joining the
// sweep is never held for the remaining corpus.
func TestSweepAbortsDeletesOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	corpus := fakeCorpus{docs: []documentstore.Document{
		{NormalizedURL: "https://bing.com/ck/a?u=a1one"},
		{NormalizedURL: "https://bing.com/ck/a?u=a1two"},
	}}
	purger := &cancellingLineagePurger{cancel: cancel}

	New(corpus, purger.PurgeResolved).Run(ctx)

	if len(purger.purged) != 1 {
		t.Fatalf("lineage purges = %v, want the pass aborted after one", purger.purged)
	}
}

func TestNewDisablesOnMissingDependencies(t *testing.T) {
	purger := &fakeLineagePurger{}
	if New(nil, purger.PurgeResolved) != nil ||
		New(fakeCorpus{}, nil) != nil {
		t.Fatal("missing dependency must disable the sweeper")
	}
	var disabled *Sweeper
	disabled.Run(context.Background())
}

func TestIsTrackingRedirect(t *testing.T) {
	cases := map[string]bool{
		"https://www.bing.com/ck/a?u=a1x": true,
		"https://bing.com/ck/a":           true,
		"https://cn.bing.com/ck/a?u=a1y":  true,
		"https://bing.com/search?q=x":     false,
		"https://notbing.com/ck/a":        false,
		"https://example.org/":            false,
		"":                                false,
		"://bad":                          false,
	}
	for rawURL, want := range cases {
		if got := IsTrackingRedirect(rawURL); got != want {
			t.Fatalf("IsTrackingRedirect(%q) = %v, want %v", rawURL, got, want)
		}
	}
}
