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
	// diskReclaimDeletesWeight biases scorch merge selection toward segments
	// carrying the most deleted documents, above the 2.0 default but below the
	// ~3.0 the planner warns is too aggressive. The node churns its index —
	// re-ingest purges a URL's stale postings and eviction deletes whole
	// documents — so reclaiming that tombstoned space sooner keeps segments
	// smaller and cuts the disk a search or merge must read, without raising the
	// total merge volume the way shrinking the tier width would (PERF-IO-02).
	diskReclaimDeletesWeight = 2.5
)

type BleveDiskIndex struct {
	mu               sync.RWMutex
	mutationMu       sync.Mutex
	updatedAtMu      sync.RWMutex
	shards           []bleve.Index
	alias            bleve.Index
	documents        documentstore.DocumentDirectory
	documentPresence documentstore.DocumentPresence
	updatedAt        time.Time
	closed           bool
	multilingual     bool
	analyzerScope    bool
	storedCandidates bool
	now              func() time.Time
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
				"MaxSegmentSize":       diskMaxSegmentDocs,
				"ReclaimDeletesWeight": diskReclaimDeletesWeight,
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
	for _, shard := range shards {
		// Shards created before BM25 was adopted persist the default TF-IDF
		// scoring; switch them in place so an existing index needs no rebuild
		// to gain saturation and length normalization.
		enableBM25Scoring(shard)
	}

	documentPresence, _ := directory.(documentstore.DocumentPresence)
	out := &BleveDiskIndex{
		shards:           shards,
		alias:            bleve.NewIndexAlias(shards...),
		documents:        directory,
		documentPresence: documentPresence,
		updatedAt:        updatedAt,
		multilingual:     supportsMultilingualAnalyzers(shards[0]),
		analyzerScope:    supportsAnalyzerScope(shards[0]),
		storedCandidates: supportsStoredCandidateProjection(shards[0]),
		now:              time.Now,
	}
	if rebuild && stored != nil {
		if err := out.rebuild(ctx, stored); err != nil {
			closeBleveShards(shards)

			return nil, err
		}
		if err := completeBleveRebuild(path); err != nil {
			closeBleveShards(shards)

			return nil, err
		}
	}
	out.warm(ctx)

	return out, nil
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

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return fmt.Errorf("search index closed")
	}
	b.mutationMu.Lock()
	defer b.mutationMu.Unlock()
	indexed, err := bleveDocumentFromStore(doc)
	if err != nil {
		return err
	}
	if err := diskShard(b.shards, id).Index(id, indexed); err != nil {
		return fmt.Errorf("index document: %w", err)
	}
	b.markUpdated()

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

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return fmt.Errorf("search index closed")
	}
	b.mutationMu.Lock()
	defer b.mutationMu.Unlock()
	if err := diskShard(b.shards, docID).Delete(docID); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	b.markUpdated()

	return nil
}

func (b *BleveDiskIndex) Search(
	ctx context.Context,
	req SearchRequest,
) (SearchResultSet, error) {
	set, orphans, err := b.searchHits(ctx, req)
	if err != nil {
		return SearchResultSet{}, err
	}
	b.dropOrphanedEntries(ctx, orphans)

	return set, nil
}

// dropOrphanedEntries deletes index entries whose stored document has vanished
// (quota eviction removes vault records without reaching into the index), so
// the index converges back onto the store instead of silently swallowing the
// best-ranked hits forever — YaCy parity: its search sorts out results whose
// document no longer verifies and purges the stale word references
// (SearchEvent.getSnippet, failURLsRegisterMissingWord). Best-effort: a failed
// delete is retried by whichever later search meets the orphan again.
func (b *BleveDiskIndex) dropOrphanedEntries(ctx context.Context, orphans []string) {
	for _, docID := range orphans {
		if err := b.Delete(ctx, docID); err != nil {
			return
		}
	}
}

func (b *BleveDiskIndex) searchHits(
	ctx context.Context,
	req SearchRequest,
) (SearchResultSet, []string, error) {
	if err := ctx.Err(); err != nil {
		return SearchResultSet{}, nil, fmt.Errorf("context: %w", err)
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" || req.MaxResults <= 0 {
		return SearchResultSet{}, nil, nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return SearchResultSet{}, nil, fmt.Errorf("search index closed")
	}
	count, err := b.docCount()
	if err != nil {
		return SearchResultSet{}, nil, fmt.Errorf("count indexed documents: %w", err)
	}
	if count == 0 {
		return SearchResultSet{}, nil, nil
	}
	indexedDocuments := bleveDocumentCount(count)
	if req.WithFacets || hasPostFilters(req) {
		return b.searchCompleteHits(ctx, req, indexedDocuments)
	}

	searchRequest := bleve.NewSearchRequest(bleveSearchQuery(
		req,
		b.multilingual,
		b.analyzerScope,
	))
	searchRequest.Size = diskSearchSize(req.MaxResults, indexedDocuments)
	searchRequest.Explain = req.Explain || req.IncludeFieldScores
	searchRequest.IncludeLocations = false
	searchRequest.Fields = storedSearchFields(req, b.storedCandidates)
	result, err := b.alias.SearchInContext(ctx, searchRequest)
	if err != nil {
		return SearchResultSet{}, nil, fmt.Errorf(
			"search documents: %w",
			bleveSearchOperationError(ctx, err),
		)
	}
	if err := bleveSearchCompletionError(ctx, result); err != nil {
		return SearchResultSet{}, nil, fmt.Errorf("search documents: %w", err)
	}

	set, orphans, err := b.collectHits(ctx, req, result)
	if err != nil {
		return SearchResultSet{}, nil, err
	}

	return set, orphans, nil
}

// collectHits hydrates the bleve hits into results. Hydrating a hit is a
// vault read plus a zstd+JSON decode of the full page, so hits that can
// neither reach the page nor change a count are never loaded: without
// post-filters or facets the loop stops at a full page and the total comes
// from bleve itself (PERF-03).
func (b *BleveDiskIndex) collectHits(
	ctx context.Context,
	req SearchRequest,
	result *bleve.SearchResult,
) (SearchResultSet, []string, error) {
	results := make([]SearchResult, 0, min(req.MaxResults, len(result.Hits)))
	facets := newFacetCollector(req.WithFacets)
	scanAll := req.WithFacets || hasPostFilters(req)
	total := 0
	var orphans []string
	for _, hit := range result.Hits {
		if !scanAll && len(results) >= req.MaxResults {
			break
		}
		projection, found, err := b.loadSearchHitProjection(ctx, hit, req)
		if err != nil {
			return SearchResultSet{}, nil, fmt.Errorf("load search document: %w", err)
		}
		if !found {
			orphans = append(orphans, hit.ID)

			continue
		}
		if !allowsDocument(projection.document, req) {
			continue
		}
		facets.observe(projection.document)
		total++
		if len(results) < req.MaxResults {
			mapped, err := projection.result(ctx, hit, req)
			if err != nil {
				return SearchResultSet{}, nil, err
			}
			results = append(results, mapped)
		}
	}
	if !scanAll {
		total = max(bleveDocumentCount(result.Total)-len(orphans), len(results))
	}
	rescoreStoredQuotedPhrasePrefix(results, req)
	rescoreStoredProximity(results, req)

	return SearchResultSet{Results: results, Total: total, Facets: facets.groups()}, orphans, nil
}

// hasPostFilters reports whether the request carries a constraint only the
// stored document can answer, forcing the hit loop to hydrate past a full
// page for honest totals.
func hasPostFilters(req SearchRequest) bool {
	return req.SafeSearch ||
		req.Language != "" ||
		len(req.IncludeDomain) > 0 ||
		len(req.ExcludeDomain) > 0 ||
		!req.Since.IsZero() ||
		!req.Until.IsZero() ||
		!req.MinDate.IsZero() ||
		!req.MaxDate.IsZero() ||
		req.Author != "" ||
		req.Near ||
		contentDomainNeedsPostFilter(req.ContentDomain) ||
		req.FileType != "" ||
		req.InURL != "" ||
		req.TLD != ""
}

func contentDomainNeedsPostFilter(contentDomain string) bool {
	return contentDomain != "" &&
		!strings.EqualFold(contentDomain, "text") &&
		!strings.EqualFold(contentDomain, "all")
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
		UpdatedAt: b.lastUpdate(),
	}, nil
}

func (b *BleveDiskIndex) markUpdated() {
	b.updatedAtMu.Lock()
	b.updatedAt = b.now().UTC()
	b.updatedAtMu.Unlock()
}

func (b *BleveDiskIndex) lastUpdate() time.Time {
	b.updatedAtMu.RLock()
	defer b.updatedAtMu.RUnlock()

	return b.updatedAt
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

func openOrCreateBleveDisk(root string, canRebuild bool) ([]bleve.Index, bool, time.Time, error) {
	rebuildRequirement, err := prepareBleveRebuildRequirement(root, canRebuild)
	if err != nil {
		return nil, false, time.Time{}, err
	}
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
		if err := rebuildRequirement.require(); err != nil {
			return nil, false, time.Time{}, err
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
		shard, created, modTime, err := openOrCreateBleveShardForRebuild(
			diskShardPath(root, i),
			canRebuild,
			rebuildRequirement.require,
		)
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

func openOrCreateBleveShard(path string, canRebuild bool) (bleve.Index, bool, time.Time, error) {
	return openOrCreateBleveShardForRebuild(path, canRebuild, func() error { return nil })
}

func openOrCreateBleveShardForRebuild(
	path string,
	canRebuild bool,
	rebuildRequired func() error,
) (bleve.Index, bool, time.Time, error) {
	info, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		index, err := openBleveDisk(path)
		if err == nil {
			if shardMappingIsCurrent(index) || !canRebuild {
				return index, false, info.ModTime().UTC(), nil
			}
			_ = index.Close()
		}
		if !canRebuild {
			return nil, false, time.Time{}, fmt.Errorf("open bleve index shard: %w", err)
		}
		if err := rebuildRequired(); err != nil {
			return nil, false, time.Time{}, err
		}
		if err := removeBleveDisk(path); err != nil {
			return nil, false, time.Time{}, fmt.Errorf("repair bleve index shard: %w", err)
		}
	case !errors.Is(statErr, os.ErrNotExist):
		return nil, false, time.Time{}, fmt.Errorf("stat bleve index shard: %w", statErr)
	case canRebuild:
		if err := rebuildRequired(); err != nil {
			return nil, false, time.Time{}, err
		}
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
