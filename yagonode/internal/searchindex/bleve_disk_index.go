package searchindex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/cespare/xxhash/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	bleveDiskBackendName = "bleve-disk"
	bleveSearchHitCap    = 1000

	// diskShardCount fixes the index shard pool (ADR-0025): documents route
	// by hash across M scorch indexes living in a three-level fanout, each
	// with a merge policy that keeps its zap files well under the 1 GB cap.
	diskShardCount = 8
	// diskMaxSegmentDocs bounds merged scorch segments; MaxSegmentSize is
	// measured in documents, not bytes.
	diskMaxSegmentDocs = 400_000
	diskSnapshotsKept  = 3
)

type BleveDiskIndex struct {
	mu        sync.RWMutex
	shards    []bleve.Index
	alias     bleve.Index
	documents documentstore.DocumentDirectory
	updatedAt time.Time
	closed    bool
	gram      bool
	now       func() time.Time
}

// diskShard picks the shard for one document id.
func diskShard(shards []bleve.Index, docID string) bleve.Index {
	return shards[int(xxhash.Sum64String(docID)%uint64(len(shards)))] //nolint:gosec // bounded by len.
}

// diskShardPath is the three-level fanout location of one index shard.
func diskShardPath(root string, shard int) string {
	id := fmt.Sprintf("%06x", shard)

	return filepath.Join(root, id[0:2], id[2:4], id[4:6], id+".idx")
}

var (
	newBleveDisk    = newBleveShard
	openBleveDisk   = bleve.Open
	removeBleveDisk = os.RemoveAll
)

// newBleveShard creates one scorch shard with the bounded merge policy. The
// merge options pass as a partial JSON object: scorch unmarshals it over its
// defaults, so only the overridden field is set (the full struct carries a
// non-serializable scoring func).
func newBleveShard(path string, indexMapping mapping.IndexMapping) (bleve.Index, error) {
	index, err := bleve.NewUsing(
		path,
		indexMapping,
		scorch.Name,
		scorch.Name,
		map[string]interface{}{
			"scorchMergePlanOptions": map[string]interface{}{
				"MaxSegmentSize": diskMaxSegmentDocs,
			},
			"numSnapshotsToKeep": diskSnapshotsKept,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create scorch shard: %w", err)
	}

	return index, nil
}

func NewBleveDiskIndex(
	ctx context.Context,
	path string,
	directory documentstore.DocumentDirectory,
	stored documentstore.StoredDocuments,
) (*BleveDiskIndex, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("bleve index path required")
	}
	if directory == nil {
		return nil, fmt.Errorf("document directory required")
	}

	shards, rebuild, updatedAt, err := openOrCreateBleveDisk(path, stored != nil)
	if err != nil {
		return nil, err
	}

	out := &BleveDiskIndex{
		shards:    shards,
		alias:     bleve.NewIndexAlias(shards...),
		documents: directory,
		updatedAt: updatedAt,
		gram:      supportsGramAnalyzer(shards[0]),
		now:       time.Now,
	}
	if rebuild && stored != nil {
		if err := out.rebuild(ctx, stored); err != nil {
			closeBleveShards(shards)

			return nil, err
		}
	}
	out.warm(ctx)

	return out, nil
}

func (b *BleveDiskIndex) warm(ctx context.Context) {
	request := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
	request.Size = 1
	_, _ = b.alias.SearchInContext(ctx, request)
}

func (b *BleveDiskIndex) Index(
	ctx context.Context,
	doc documentstore.Document,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	id := documentID(doc)
	if id == "" {
		return fmt.Errorf("document id required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fmt.Errorf("search index closed")
	}
	if err := diskShard(b.shards, id).Index(id, bleveDocumentFromStore(doc)); err != nil {
		return fmt.Errorf("index document: %w", err)
	}
	b.updatedAt = b.now().UTC()

	return nil
}

func (b *BleveDiskIndex) Delete(ctx context.Context, docID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	docID = strings.TrimSpace(docID)
	if docID == "" {
		return fmt.Errorf("document id required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fmt.Errorf("search index closed")
	}
	if err := diskShard(b.shards, docID).Delete(docID); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	b.updatedAt = b.now().UTC()

	return nil
}

func (b *BleveDiskIndex) Search(
	ctx context.Context,
	req SearchRequest,
) (SearchResultSet, error) {
	if err := ctx.Err(); err != nil {
		return SearchResultSet{}, fmt.Errorf("context: %w", err)
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" || req.MaxResults <= 0 {
		return SearchResultSet{}, nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return SearchResultSet{}, fmt.Errorf("search index closed")
	}
	count, err := b.docCount()
	if err != nil {
		return SearchResultSet{}, fmt.Errorf("count indexed documents: %w", err)
	}
	if count == 0 {
		return SearchResultSet{}, nil
	}

	searchRequest := bleve.NewSearchRequest(bleveSearchQuery(req, b.gram))
	searchRequest.Size = diskSearchSize(req.MaxResults, bleveDocumentCount(count))
	searchRequest.Explain = req.Explain
	result, err := b.alias.SearchInContext(ctx, searchRequest)
	if err != nil {
		return SearchResultSet{}, fmt.Errorf("search documents: %w", err)
	}

	results := make([]SearchResult, 0, min(req.MaxResults, len(result.Hits)))
	facets := newFacetCollector(req.WithFacets)
	total := 0
	for _, hit := range result.Hits {
		doc, found, err := b.documents.Document(ctx, hit.ID)
		if err != nil {
			return SearchResultSet{}, fmt.Errorf("load search document: %w", err)
		}
		if !found || !allowsDocument(doc, req) {
			continue
		}
		facets.observe(doc)
		total++
		if len(results) < req.MaxResults {
			results = append(
				results,
				searchResultFromDocument(hit.ID, doc, req, hit.Score, hitExplanation(req, hit)),
			)
		}
	}

	return SearchResultSet{Results: results, Total: total, Facets: facets.groups()}, nil
}

func (b *BleveDiskIndex) Stats(ctx context.Context) (IndexStats, error) {
	if err := ctx.Err(); err != nil {
		return IndexStats{}, fmt.Errorf("context: %w", err)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return IndexStats{}, fmt.Errorf("search index closed")
	}
	count, err := b.docCount()
	if err != nil {
		return IndexStats{}, fmt.Errorf("count indexed documents: %w", err)
	}

	return IndexStats{
		Documents: bleveDocumentCount(count),
		Backend:   bleveDiskBackendName,
		UpdatedAt: b.updatedAt,
	}, nil
}

func (b *BleveDiskIndex) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true

	for _, shard := range b.shards {
		if err := shard.Close(); err != nil {
			return fmt.Errorf("close bleve disk index: %w", err)
		}
	}

	return nil
}

// docCount sums the shards' document counts.
func (b *BleveDiskIndex) docCount() (uint64, error) {
	var total uint64
	for _, shard := range b.shards {
		count, err := shard.DocCount()
		if err != nil {
			return 0, err //nolint:wrapcheck // callers wrap with context.
		}
		total += count
	}

	return total, nil
}

func closeBleveShards(shards []bleve.Index) {
	for _, shard := range shards {
		_ = shard.Close()
	}
}

func (b *BleveDiskIndex) rebuild(
	ctx context.Context,
	stored documentstore.StoredDocuments,
) error {
	if err := stored.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		if err := b.Index(ctx, doc); err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("rebuild bleve disk index: %w", err)
	}

	return nil
}

// openOrCreateBleveDisk opens the sharded index layout under root: M scorch
// shards at root/xx/zz/yy/xxzzyy.idx (ADR-0025). A legacy single bleve index
// found at root is removed and rebuilt from the stored documents; a shard that
// fails to open or predates the trigram analyzer recreates alone (its slice of
// the corpus re-indexes on the rebuild pass).
func openOrCreateBleveDisk(root string, canRebuild bool) ([]bleve.Index, bool, time.Time, error) {
	if legacy, info := legacyBleveLayout(root); legacy {
		if !canRebuild {
			// Without a rebuild source the legacy index keeps serving as a
			// single shard in its compatibility mode.
			index, err := openBleveDisk(root)
			if err != nil {
				return nil, false, time.Time{}, fmt.Errorf("open bleve index shard: %w", err)
			}

			return []bleve.Index{index}, false, info.ModTime().UTC(), nil
		}
		// A legacy single bleve index (or an unreadable remnant) occupies the
		// root: rebuild it into the sharded layout from the stored documents.
		if err := removeBleveDisk(root); err != nil {
			return nil, false, time.Time{}, fmt.Errorf("retire legacy bleve index: %w", err)
		}
	}
	shards := make([]bleve.Index, 0, diskShardCount)
	rebuild := false
	var updatedAt time.Time
	for i := 0; i < diskShardCount; i++ {
		shard, created, modTime, err := openOrCreateBleveShard(diskShardPath(root, i), canRebuild)
		if err != nil {
			closeBleveShards(shards)

			return nil, false, time.Time{}, err
		}
		rebuild = rebuild || created
		if modTime.After(updatedAt) {
			updatedAt = modTime
		}
		shards = append(shards, shard)
	}
	if rebuild {
		updatedAt = time.Time{}
	}

	return shards, rebuild, updatedAt, nil
}

// legacyBleveLayout reports whether the root holds a pre-shard layout: a
// single bleve index directory or an unreadable non-directory remnant.
func legacyBleveLayout(root string) (bool, os.FileInfo) {
	info, err := os.Stat(root)
	if err != nil {
		return false, nil
	}
	if !info.IsDir() {
		return true, info
	}
	if _, err := os.Stat(filepath.Join(root, "index_meta.json")); err == nil {
		return true, info
	}

	return false, info
}

// openOrCreateBleveShard opens one shard, recreating it when it is missing,
// unreadable, or predates the trigram analyzer.
func openOrCreateBleveShard(path string, canRebuild bool) (bleve.Index, bool, time.Time, error) {
	info, statErr := os.Stat(path)
	if statErr == nil {
		index, err := openBleveDisk(path)
		if err == nil {
			if supportsGramAnalyzer(index) || !canRebuild {
				return index, false, info.ModTime().UTC(), nil
			}
			_ = index.Close()
		}
		if !canRebuild {
			return nil, false, time.Time{}, fmt.Errorf("open bleve index shard: %w", err)
		}
		if err := removeBleveDisk(path); err != nil {
			return nil, false, time.Time{}, fmt.Errorf("repair bleve index shard: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, false, time.Time{}, fmt.Errorf("stat bleve index shard: %w", statErr)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, false, time.Time{}, fmt.Errorf("create index shard directory: %w", err)
	}
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		return nil, false, time.Time{}, fmt.Errorf("build search index mapping: %w", err)
	}
	index, err := newBleveDisk(path, indexMapping)
	if err != nil {
		return nil, false, time.Time{}, fmt.Errorf("create bleve index shard: %w", err)
	}

	return index, true, time.Time{}, nil
}

func diskSearchSize(maxResults int, indexedDocuments int) int {
	size := max(maxResults*4, maxResults)
	size = min(size, bleveSearchHitCap)
	size = min(size, indexedDocuments)
	return max(size, 0)
}

func bleveDocumentCount(count uint64) int {
	maxInt := int(^uint(0) >> 1)
	if count > uint64(maxInt) {
		return maxInt
	}

	value, _ := strconv.Atoi(strconv.FormatUint(count, 10))
	return value
}
