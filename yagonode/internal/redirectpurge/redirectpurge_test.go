package redirectpurge

import (
	"context"
	"errors"
	"testing"

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

type fakeEvictor struct {
	deleted []string
	fail    map[string]bool
}

func (f *fakeEvictor) Delete(_ context.Context, url string) (bool, error) {
	if f.fail[url] {
		return false, errors.New("vault busy")
	}
	f.deleted = append(f.deleted, url)

	return true, nil
}

type fakeIndex struct {
	deleted []string
	fail    map[string]bool
}

func (f *fakeIndex) Delete(_ context.Context, docID string) error {
	if f.fail[docID] {
		return errors.New("index busy")
	}
	f.deleted = append(f.deleted, docID)

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
	evictor := &fakeEvictor{}
	index := &fakeIndex{}

	New(corpus, evictor, index).Run(context.Background())

	want := []string{
		"https://www.bing.com/ck/a?u=a1aHR0cHM6Ly9leGFtcGxlLm9yZw",
		"https://kept.example/x",
	}
	if len(index.deleted) != 2 || index.deleted[0] != want[0] || index.deleted[1] != want[1] {
		t.Fatalf("index deletions = %v", index.deleted)
	}
	if len(evictor.deleted) != 2 {
		t.Fatalf("vault deletions = %v", evictor.deleted)
	}
}

// TestSweepSurvivesPerDocumentFailures pins the resilience contract: one
// stubborn document does not stop the sweep, and an index failure skips the
// vault delete so the pair stays consistent.
func TestSweepSurvivesPerDocumentFailures(t *testing.T) {
	first := "https://bing.com/ck/a?u=a1one"
	second := "https://bing.com/ck/b?u=a1two"
	corpus := fakeCorpus{docs: []documentstore.Document{
		{NormalizedURL: first}, {NormalizedURL: second},
	}}
	evictor := &fakeEvictor{}
	index := &fakeIndex{fail: map[string]bool{first: true}}

	New(corpus, evictor, index).Run(context.Background())

	if len(index.deleted) != 1 || index.deleted[0] != second {
		t.Fatalf("index deletions = %v", index.deleted)
	}
	if len(evictor.deleted) != 1 || evictor.deleted[0] != second {
		t.Fatalf("failed index delete must skip the vault: %v", evictor.deleted)
	}
}

func TestNewDisablesOnMissingDependencies(t *testing.T) {
	if New(nil, &fakeEvictor{}, &fakeIndex{}) != nil ||
		New(fakeCorpus{}, nil, &fakeIndex{}) != nil ||
		New(fakeCorpus{}, &fakeEvictor{}, nil) != nil {
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
