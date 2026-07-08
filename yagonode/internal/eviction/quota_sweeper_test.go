package eviction_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type seedCodec struct{}

func (seedCodec) Encode(value []byte) ([]byte, error) { return value, nil }
func (seedCodec) Decode(raw []byte) ([]byte, error)   { return raw, nil }

func openVault(t *testing.T, quotaBytes int64) *vault.Vault {
	t.Helper()

	v, err := memvault.Open(quotaBytes)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	seedUsage(t, v)

	return v
}

func seedUsage(t *testing.T, v *vault.Vault) {
	t.Helper()

	collection, err := vault.Register(v, vault.Name("seed"), seedCodec{})
	if err != nil {
		t.Fatalf("Register seed: %v", err)
	}
	if err := v.Update(context.Background(), func(tx *vault.Txn) error {
		if err := collection.Put(tx, vault.Key("seed"), make([]byte, 64)); err != nil {
			return fmt.Errorf("put seed: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
}

type fakeReferences struct {
	word  yagomodel.Hash
	err   error
	empty bool
}

func (f fakeReferences) WordsReferencing(
	_ *vault.Txn,
	_ yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.empty {
		return nil, nil
	}
	return []yagomodel.Hash{f.word}, nil
}

func (f fakeReferences) ReferencedURLs(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return nil, f.err
}

func (f fakeReferences) ReferencedURLCount(context.Context) (int, error) {
	return 0, nil
}

type fakePostings struct {
	purged []yagomodel.Hash
	err    error
}

func (f *fakePostings) PurgePosting(
	_ *vault.Txn,
	_, url yagomodel.Hash,
) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.purged = append(f.purged, url)

	return true, nil
}

type fakeURLs struct {
	remaining []yagomodel.Hash
	selected  [][]yagomodel.Hash
	selectErr error
	noDelete  bool
	purgeErr  error
}

func (f *fakeURLs) StalestURLs(_ context.Context, limit int) ([]yagomodel.Hash, error) {
	if f.selectErr != nil {
		return nil, f.selectErr
	}
	if limit > len(f.remaining) {
		limit = len(f.remaining)
	}
	batch := f.remaining[:limit]
	f.selected = append(f.selected, batch)

	return batch, nil
}

func (f *fakeURLs) Purge(
	_ context.Context,
	_ *vault.Txn,
	urls []yagomodel.Hash,
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

func hashes(n int) []yagomodel.Hash {
	out := make([]yagomodel.Hash, n)
	for i := range out {
		out[i] = yagomodel.WordHash(string(rune('a' + i)))
	}

	return out
}

func newSweeper(
	vault *vault.Vault,
	postings *fakePostings,
	urls *fakeURLs,
	target float64,
	batch int,
) eviction.Sweeper {
	return eviction.NewSweeper(
		vault,
		postings,
		fakeReferences{word: yagomodel.WordHash("w")},
		urls,
		nil,
		nil,
		urls,
		eviction.Config{TargetFraction: target, BatchSize: batch},
	)
}

func newSweeperWithReferences(
	vault *vault.Vault,
	postings *fakePostings,
	references fakeReferences,
	urls *fakeURLs,
) eviction.Sweeper {
	return eviction.NewSweeper(
		vault,
		postings,
		references,
		urls,
		nil,
		nil,
		urls,
		eviction.Config{TargetFraction: 1, BatchSize: 1},
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

func TestSweepNoopWithoutBatch(t *testing.T) {
	result, err := newSweeper(
		openVault(t, 1),
		&fakePostings{},
		&fakeURLs{remaining: hashes(4)},
		0.5,
		0,
	).
		Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result != (eviction.Result{}) {
		t.Fatalf("result = %+v, want empty", result)
	}
}

func TestSweepReportsUsageError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newSweeper(openVault(t, 1), &fakePostings{}, &fakeURLs{remaining: hashes(1)}, 1, 1).
		Sweep(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSweepReportsCandidateSelectionError(t *testing.T) {
	wantErr := errors.New("selection failed")

	_, err := newSweeper(
		openVault(t, 1),
		&fakePostings{},
		&fakeURLs{remaining: hashes(1), selectErr: wantErr},
		1,
		1,
	).
		Sweep(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
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

func TestSweepReportsReferenceError(t *testing.T) {
	wantErr := errors.New("references failed")

	_, err := newSweeperWithReferences(
		openVault(t, 1),
		&fakePostings{},
		fakeReferences{err: wantErr},
		&fakeURLs{remaining: hashes(1)},
	).Sweep(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestSweepReportsPostingError(t *testing.T) {
	wantErr := errors.New("posting failed")

	_, err := newSweeper(
		openVault(t, 1),
		&fakePostings{err: wantErr},
		&fakeURLs{remaining: hashes(1)},
		1,
		1,
	).Sweep(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestEvictorEvictsURLs(t *testing.T) {
	v := openVault(t, 1024)
	postings := &fakePostings{}
	urls := &fakeURLs{remaining: hashes(2)}
	evictor := eviction.NewEvictor(
		v, postings, fakeReferences{word: yagomodel.WordHash("w")}, urls, nil, nil,
	)

	result, err := evictor.EvictURLs(context.Background(), hashes(2))
	if err != nil {
		t.Fatalf("EvictURLs: %v", err)
	}
	if result.URLsDeleted != 2 || result.PostingsDeleted != 2 {
		t.Fatalf("result = %+v", result)
	}
	if len(postings.purged) != 2 {
		t.Fatalf("purged = %v", postings.purged)
	}
}

func TestEvictorEvictsAbsentURLsNoop(t *testing.T) {
	v := openVault(t, 1024)
	postings := &fakePostings{}
	urls := &fakeURLs{noDelete: true}
	evictor := eviction.NewEvictor(v, postings, fakeReferences{empty: true}, urls, nil, nil)

	result, err := evictor.EvictURLs(context.Background(), hashes(1))
	if err != nil {
		t.Fatalf("EvictURLs: %v", err)
	}
	if result.URLsDeleted != 0 || result.PostingsDeleted != 0 {
		t.Fatalf("purging an absent url must delete nothing, result = %+v", result)
	}
	if len(postings.purged) != 0 {
		t.Fatalf("purged = %v, want none", postings.purged)
	}
}

func TestEvictorSurfacesPurgeError(t *testing.T) {
	v := openVault(t, 1024)
	evictor := eviction.NewEvictor(
		v, &fakePostings{}, fakeReferences{}, &fakeURLs{purgeErr: errors.New("boom")},
		nil, nil,
	)

	if _, err := evictor.EvictURLs(context.Background(), hashes(1)); err == nil {
		t.Fatal("EvictURLs should surface a purge error")
	}
}

func TestEvictorPurgeDropsURLs(t *testing.T) {
	v := openVault(t, 1024)
	postings := &fakePostings{}
	urls := &fakeURLs{remaining: hashes(1)}
	evictor := eviction.NewEvictor(
		v, postings, fakeReferences{word: yagomodel.WordHash("w")}, urls, nil, nil,
	)

	if err := evictor.Purge(context.Background(), hashes(1)); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(postings.purged) != 1 {
		t.Fatalf("purged = %v, want one", postings.purged)
	}
}

func TestEvictorPurgeSurfacesError(t *testing.T) {
	v := openVault(t, 1024)
	evictor := eviction.NewEvictor(
		v, &fakePostings{}, fakeReferences{}, &fakeURLs{purgeErr: errors.New("boom")},
		nil, nil,
	)

	if err := evictor.Purge(context.Background(), hashes(1)); err == nil {
		t.Fatal("Purge should surface a purge error")
	}
}

type fakeResolver struct {
	rows []yagomodel.URIMetadataRow
	err  error
}

func (f fakeResolver) RowsByHash(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.URIMetadataRow, error) {
	return f.rows, f.err
}

type fakeDocuments struct {
	deleted []string
	absent  map[string]bool
	err     error
}

func (f *fakeDocuments) Delete(_ context.Context, url string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.deleted = append(f.deleted, url)
	if f.absent[url] {
		return false, nil
	}

	return true, nil
}

func urlMetaRow(url string) yagomodel.URIMetadataRow {
	return yagomodel.URIMetadataRow{
		Properties: map[string]string{
			yagomodel.URLMetaURL: yagomodel.EncodeBase64WireForm(url),
		},
	}
}

// TestEvictURLsDropsResolvedDocuments proves ADR-0036 B: a hash-driven purge
// resolves each URL through its metadata row (whose "url" is wire-encoded) and
// drops the document keyed by that URL. A resolved URL whose document is already
// gone is an idempotent no-op that does not count.
func TestEvictURLsDropsResolvedDocuments(t *testing.T) {
	v := openVault(t, 1024)
	docs := &fakeDocuments{absent: map[string]bool{"http://b/": true}}
	resolver := fakeResolver{rows: []yagomodel.URIMetadataRow{
		urlMetaRow("http://a/"), urlMetaRow("http://b/"),
	}}
	evictor := eviction.NewEvictor(
		v, &fakePostings{}, fakeReferences{word: yagomodel.WordHash("w")},
		&fakeURLs{remaining: hashes(2)}, docs, resolver,
	)

	result, err := evictor.EvictURLs(context.Background(), hashes(2))
	if err != nil {
		t.Fatalf("EvictURLs: %v", err)
	}
	if len(docs.deleted) != 2 || docs.deleted[0] != "http://a/" || docs.deleted[1] != "http://b/" {
		t.Fatalf("document delete attempts = %v, want [http://a/ http://b/]", docs.deleted)
	}
	if result.DocumentsDeleted != 1 {
		t.Fatalf("documents deleted = %d, want 1 (the present one)", result.DocumentsDeleted)
	}
}

// TestEvictURLsSurfacesDocumentError makes a failing document delete fail the
// purge rather than silently dropping the postings and metadata.
func TestEvictURLsSurfacesDocumentError(t *testing.T) {
	v := openVault(t, 1024)
	docs := &fakeDocuments{err: errors.New("boom")}
	resolver := fakeResolver{rows: []yagomodel.URIMetadataRow{urlMetaRow("http://a/")}}
	evictor := eviction.NewEvictor(
		v, &fakePostings{}, fakeReferences{}, &fakeURLs{}, docs, resolver,
	)

	if _, err := evictor.EvictURLs(context.Background(), hashes(1)); err == nil {
		t.Fatal("EvictURLs should surface a document delete error")
	}
}
