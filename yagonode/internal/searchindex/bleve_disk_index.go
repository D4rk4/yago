package searchindex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	bleveDiskBackendName = "bleve-disk"
	bleveSearchHitCap    = 1000
)

type BleveDiskIndex struct {
	mu        sync.RWMutex
	index     bleve.Index
	documents documentstore.DocumentDirectory
	updatedAt time.Time
	closed    bool
	now       func() time.Time
}

var (
	newBleveDisk    = bleve.New
	openBleveDisk   = bleve.Open
	removeBleveDisk = os.RemoveAll
)

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

	index, rebuild, updatedAt, err := openOrCreateBleveDisk(path)
	if err != nil {
		return nil, err
	}

	out := &BleveDiskIndex{
		index:     index,
		documents: directory,
		updatedAt: updatedAt,
		now:       time.Now,
	}
	if rebuild && stored != nil {
		if err := out.rebuild(ctx, stored); err != nil {
			_ = index.Close()
			return nil, err
		}
	}
	out.warm(ctx)

	return out, nil
}

func (b *BleveDiskIndex) warm(ctx context.Context) {
	request := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
	request.Size = 1
	_, _ = b.index.SearchInContext(ctx, request)
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
	if err := b.index.Index(id, bleveDocumentFromStore(doc)); err != nil {
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
	if err := b.index.Delete(docID); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	b.updatedAt = b.now().UTC()

	return nil
}

func (b *BleveDiskIndex) Search(
	ctx context.Context,
	req SearchRequest,
) (SearchResultSet, error) {
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" || req.MaxResults <= 0 {
		return SearchResultSet{}, nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return SearchResultSet{}, fmt.Errorf("search index closed")
	}
	count, err := b.index.DocCount()
	if err != nil {
		return SearchResultSet{}, fmt.Errorf("count indexed documents: %w", err)
	}
	if count == 0 {
		return SearchResultSet{}, nil
	}

	searchRequest := bleve.NewSearchRequest(bleveSearchQuery(req))
	searchRequest.Size = diskSearchSize(req.MaxResults, bleveDocumentCount(count))
	searchRequest.Explain = req.Explain
	result, err := b.index.SearchInContext(ctx, searchRequest)
	if err != nil {
		return SearchResultSet{}, fmt.Errorf("search documents: %w", err)
	}

	results := make([]SearchResult, 0, min(req.MaxResults, len(result.Hits)))
	total := 0
	for _, hit := range result.Hits {
		doc, found, err := b.documents.Document(ctx, hit.ID)
		if err != nil {
			return SearchResultSet{}, fmt.Errorf("load search document: %w", err)
		}
		if !found || !allowsDocument(doc, req) {
			continue
		}
		total++
		if len(results) < req.MaxResults {
			results = append(
				results,
				searchResultFromDocument(hit.ID, doc, req, hit.Score, hitExplanation(req, hit)),
			)
		}
	}

	return SearchResultSet{Results: results, Total: total}, nil
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
	count, err := b.index.DocCount()
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

	if err := b.index.Close(); err != nil {
		return fmt.Errorf("close bleve disk index: %w", err)
	}

	return nil
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

func openOrCreateBleveDisk(path string) (bleve.Index, bool, time.Time, error) {
	info, statErr := os.Stat(path)
	if errors.Is(statErr, os.ErrNotExist) {
		indexMapping, err := newSearchIndexMapping()
		if err != nil {
			return nil, false, time.Time{}, fmt.Errorf("build search index mapping: %w", err)
		}
		index, err := newBleveDisk(path, indexMapping)
		if err != nil {
			return nil, false, time.Time{}, fmt.Errorf("create bleve disk index: %w", err)
		}

		return index, true, time.Time{}, nil
	} else if statErr != nil {
		return nil, false, time.Time{}, fmt.Errorf("stat bleve disk index: %w", statErr)
	}

	index, err := openBleveDisk(path)
	if err == nil {
		return index, false, info.ModTime().UTC(), nil
	}
	if err := removeBleveDisk(path); err != nil {
		return nil, false, time.Time{}, fmt.Errorf("repair bleve disk index: %w", err)
	}
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		return nil, false, time.Time{}, fmt.Errorf("build search index mapping: %w", err)
	}
	index, err = newBleveDisk(path, indexMapping)
	if err != nil {
		return nil, false, time.Time{}, fmt.Errorf("recreate bleve disk index: %w", err)
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
