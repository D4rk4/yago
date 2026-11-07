package eviction_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
)

func TestEvictorDropsWaitingOutboundPostingsForDeletedURLs(t *testing.T) {
	v := openVault(t, 1024)
	postings := &fakePostings{}
	deleted := hashes(2)
	evictor := eviction.NewEvictor(
		v, postings, fakeReferences{word: yagomodel.WordHash("w")},
		&fakeURLs{remaining: deleted}, nil, nil,
	)

	if _, err := evictor.EvictURLs(context.Background(), deleted); err != nil {
		t.Fatalf("EvictURLs: %v", err)
	}
	if !slices.Equal(postings.waiting, deleted) {
		t.Fatalf("waiting postings dropped for %v, want %v", postings.waiting, deleted)
	}
}

func TestEvictorSurfacesWaitingOutboundPostingError(t *testing.T) {
	v := openVault(t, 1024)
	want := errors.New("drop waiting failed")
	urls := &fakeURLs{remaining: hashes(1)}
	evictor := eviction.NewEvictor(
		v, &fakePostings{waitingErr: want}, fakeReferences{word: yagomodel.WordHash("w")},
		urls, nil, nil,
	)

	if _, err := evictor.EvictURLs(context.Background(), hashes(1)); !errors.Is(err, want) {
		t.Fatalf("EvictURLs error = %v, want %v", err, want)
	}
	if len(urls.remaining) != 1 {
		t.Fatalf("remaining urls = %d, want metadata kept after a failed drop", len(urls.remaining))
	}
}
