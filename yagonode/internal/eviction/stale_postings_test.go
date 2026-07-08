package eviction_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type multiWordReferences struct {
	words []yagomodel.Hash
	err   error
}

func (f multiWordReferences) WordsReferencing(
	_ *vault.Txn,
	_ yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return f.words, f.err
}

func (f multiWordReferences) ReferencedURLs(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return nil, nil
}

func (f multiWordReferences) ReferencedURLCount(context.Context) (int, error) {
	return 0, nil
}

type wordTrackingPostings struct {
	purged  []yagomodel.Hash
	missing map[yagomodel.Hash]bool
	err     error
}

func (f *wordTrackingPostings) PurgePosting(
	_ *vault.Txn,
	word, _ yagomodel.Hash,
) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if f.missing[word] {
		return false, nil
	}
	f.purged = append(f.purged, word)

	return true, nil
}

func stalePostingsEvictor(
	t *testing.T,
	references multiWordReferences,
	postings *wordTrackingPostings,
) eviction.Evictor {
	t.Helper()

	return eviction.NewEvictor(openVault(t, 1024), postings, references, &fakeURLs{}, nil, nil)
}

func TestPurgeStalePostingsDropsOnlyVanishedWords(t *testing.T) {
	t.Parallel()

	old := []yagomodel.Hash{
		yagomodel.WordHash("alpha"),
		yagomodel.WordHash("beta"),
		yagomodel.WordHash("gamma"),
	}
	postings := &wordTrackingPostings{missing: map[yagomodel.Hash]bool{old[2]: true}}
	evictor := stalePostingsEvictor(t, multiWordReferences{words: old}, postings)

	live := map[yagomodel.Hash]struct{}{old[0]: {}}
	url, err := yagomodel.HashURL("https://example.com/page")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	purged, err := evictor.PurgeStalePostings(context.Background(), url.Hash(), live)
	if err != nil {
		t.Fatalf("PurgeStalePostings: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want 1 (beta deleted, gamma already absent)", purged)
	}
	if len(postings.purged) != 1 || postings.purged[0] != old[1] {
		t.Fatalf("deleted = %v, want only beta (alpha kept, gamma already absent)", postings.purged)
	}
}

func TestPurgeStalePostingsNoopWhenAllWordsSurvive(t *testing.T) {
	t.Parallel()

	word := yagomodel.WordHash("alpha")
	postings := &wordTrackingPostings{}
	evictor := stalePostingsEvictor(
		t,
		multiWordReferences{words: []yagomodel.Hash{word}},
		postings,
	)

	purged, err := evictor.PurgeStalePostings(
		context.Background(),
		yagomodel.WordHash("url"),
		map[yagomodel.Hash]struct{}{word: {}},
	)
	if err != nil || purged != 0 || len(postings.purged) != 0 {
		t.Fatalf("survivors must not purge: %d %v %v", purged, postings.purged, err)
	}
}

func TestPurgeStalePostingsSurfacesErrors(t *testing.T) {
	t.Parallel()

	refErr := errors.New("references failed")
	evictor := stalePostingsEvictor(
		t,
		multiWordReferences{err: refErr},
		&wordTrackingPostings{},
	)
	if _, err := evictor.PurgeStalePostings(
		context.Background(),
		yagomodel.WordHash("url"),
		nil,
	); err == nil || !errors.Is(err, refErr) {
		t.Fatalf("references error lost: %v", err)
	}

	purgeErr := errors.New("purge failed")
	evictor = stalePostingsEvictor(
		t,
		multiWordReferences{words: []yagomodel.Hash{yagomodel.WordHash("alpha")}},
		&wordTrackingPostings{err: purgeErr},
	)
	if _, err := evictor.PurgeStalePostings(
		context.Background(),
		yagomodel.WordHash("url"),
		nil,
	); err == nil || !errors.Is(err, purgeErr) ||
		!strings.Contains(err.Error(), "purge stale posting") {
		t.Fatalf("purge error lost: %v", err)
	}
}
