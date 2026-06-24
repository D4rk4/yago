package eviction_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/eviction"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
)

func openVault(t *testing.T, quotaBytes int64) *boltvault.Vault {
	t.Helper()

	vault, err := boltvault.Open(filepath.Join(t.TempDir(), "node.db"), quotaBytes)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := vault.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	return vault
}

type fakeReferences struct {
	word yacymodel.Hash
}

func (f fakeReferences) WordsReferencing(
	_ *boltvault.Txn,
	_ yacymodel.Hash,
) ([]yacymodel.Hash, error) {
	return []yacymodel.Hash{f.word}, nil
}

func (f fakeReferences) ReferencedURLCount(context.Context) (int, error) {
	return 0, nil
}

type fakePostings struct {
	purged []yacymodel.Hash
}

func (f *fakePostings) PurgePosting(
	_ *boltvault.Txn,
	_, url yacymodel.Hash,
) (bool, error) {
	f.purged = append(f.purged, url)

	return true, nil
}

type fakeURLs struct {
	remaining []yacymodel.Hash
	selected  [][]yacymodel.Hash
	noDelete  bool
	purgeErr  error
}

func (f *fakeURLs) StalestURLs(_ context.Context, limit int) ([]yacymodel.Hash, error) {
	if limit > len(f.remaining) {
		limit = len(f.remaining)
	}
	batch := f.remaining[:limit]
	f.selected = append(f.selected, batch)

	return batch, nil
}

func (f *fakeURLs) Purge(
	_ context.Context,
	_ *boltvault.Txn,
	urls []yacymodel.Hash,
) (urlmeta.PurgeResult, error) {
	if f.purgeErr != nil {
		return urlmeta.PurgeResult{}, f.purgeErr
	}
	if f.noDelete {
		return urlmeta.PurgeResult{}, nil
	}
	f.remaining = f.remaining[len(urls):]

	return urlmeta.PurgeResult{URLsDeleted: len(urls)}, nil
}

func hashes(n int) []yacymodel.Hash {
	out := make([]yacymodel.Hash, n)
	for i := range out {
		out[i] = yacymodel.WordHash(string(rune('a' + i)))
	}

	return out
}

func newSweeper(
	vault *boltvault.Vault,
	postings *fakePostings,
	urls *fakeURLs,
	target float64,
	batch int,
) eviction.Sweeper {
	return eviction.NewSweeper(
		vault,
		postings,
		fakeReferences{word: yacymodel.WordHash("w")},
		urls,
		urls,
		eviction.Config{TargetFraction: target, BatchSize: batch},
	)
}

func TestSweepDrainsCandidatesAboveTarget(t *testing.T) {
	vault := openVault(t, 1)
	postings := &fakePostings{}
	urls := &fakeURLs{remaining: hashes(5)}

	result, err := newSweeper(vault, postings, urls, 1, 2).Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.URLsDeleted != 5 || result.PostingsDeleted != 5 {
		t.Fatalf("result = %+v, want 5/5", result)
	}
	if len(urls.remaining) != 0 {
		t.Fatalf("remaining = %d, want fully drained", len(urls.remaining))
	}
	if len(urls.selected) != 4 {
		t.Fatalf("select calls = %d, want 4 (2+2+1+empty)", len(urls.selected))
	}
}

func TestSweepStopsOnNoProgress(t *testing.T) {
	vault := openVault(t, 1)
	urls := &fakeURLs{remaining: hashes(4), noDelete: true}

	result, err := newSweeper(vault, &fakePostings{}, urls, 1, 2).Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.URLsDeleted != 0 {
		t.Fatalf("URLsDeleted = %d, want 0", result.URLsDeleted)
	}
	if len(urls.selected) != 1 {
		t.Fatalf("select calls = %d, want 1 before bailing", len(urls.selected))
	}
}

func TestSweepNoopUnderTarget(t *testing.T) {
	vault := openVault(t, 1<<30)
	urls := &fakeURLs{remaining: hashes(4)}

	result, err := newSweeper(vault, &fakePostings{}, urls, 0.9, 2).Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result != (eviction.Result{}) {
		t.Fatalf("result = %+v, want empty", result)
	}
	if len(urls.selected) != 0 {
		t.Fatalf("select calls = %d, want 0", len(urls.selected))
	}
}

func TestSweepNoopWithoutQuota(t *testing.T) {
	result, err := newSweeper(
		openVault(t, 0),
		&fakePostings{},
		&fakeURLs{remaining: hashes(4)},
		0.5,
		2,
	).
		Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result != (eviction.Result{}) {
		t.Fatalf("result = %+v, want empty", result)
	}
}

func TestSweepReportsPurgeError(t *testing.T) {
	wantErr := errors.New("boom")
	urls := &fakeURLs{remaining: hashes(4), purgeErr: wantErr}

	_, err := newSweeper(openVault(t, 1), &fakePostings{}, urls, 1, 1).Sweep(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
