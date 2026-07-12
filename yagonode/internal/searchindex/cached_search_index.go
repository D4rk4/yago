package searchindex

import (
	"container/list"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	defaultQueryCacheCapacity     = 256
	defaultQueryCacheMaximumBytes = 16 << 20
)

type CachedSearchIndex struct {
	inner    SearchIndex
	capacity int

	mu         sync.Mutex
	entries    map[string]*cachedSearchEntry
	order      *list.List
	generation uint64
	retained   int
	limit      int
}

type cachedSearchEntry struct {
	generation uint64
	key        string
	results    SearchResultSet
	element    *list.Element
	retained   int
}

var detachCachedSearchResultSet = cloneResultSet

func NewCachedSearchIndex(inner SearchIndex, capacity int) *CachedSearchIndex {
	if capacity <= 0 {
		capacity = defaultQueryCacheCapacity
	}

	return &CachedSearchIndex{
		inner:    inner,
		capacity: capacity,
		entries:  make(map[string]*cachedSearchEntry, capacity),
		order:    list.New(),
		limit:    defaultQueryCacheMaximumBytes,
	}
}

func (c *CachedSearchIndex) Index(ctx context.Context, doc documentstore.Document) error {
	if err := c.inner.Index(ctx, doc); err != nil {
		return fmt.Errorf("cached index: %w", err)
	}
	c.invalidate()

	return nil
}

func (c *CachedSearchIndex) Delete(ctx context.Context, docID string) error {
	if err := c.inner.Delete(ctx, docID); err != nil {
		return fmt.Errorf("cached delete: %w", err)
	}
	c.invalidate()

	return nil
}

func (c *CachedSearchIndex) Search(
	ctx context.Context,
	req SearchRequest,
) (SearchResultSet, error) {
	key := cacheKey(req)

	c.mu.Lock()
	generation := c.generation
	if entry, ok := c.entries[key]; ok && entry.generation == generation {
		c.order.MoveToFront(entry.element)
		results := entry.results
		c.mu.Unlock()

		return cloneResultSet(results), nil
	}
	c.mu.Unlock()

	results, err := c.inner.Search(ctx, req)
	if err != nil {
		return SearchResultSet{}, fmt.Errorf("cached search: %w", err)
	}
	c.store(key, generation, results)

	return results, nil
}

func (c *CachedSearchIndex) SearchEvidence(
	ctx context.Context,
	req SearchRequest,
	results []SearchResult,
) ([]SearchResult, error) {
	source, ok := c.inner.(SearchEvidenceSource)
	if !ok {
		return results, nil
	}

	enriched, err := source.SearchEvidence(ctx, req, results)
	if err != nil {
		return nil, fmt.Errorf("cached search evidence: %w", err)
	}

	return enriched, nil
}

func (c *CachedSearchIndex) Stats(ctx context.Context) (IndexStats, error) {
	stats, err := c.inner.Stats(ctx)
	if err != nil {
		return IndexStats{}, fmt.Errorf("cached stats: %w", err)
	}

	return stats, nil
}

func (c *CachedSearchIndex) Close() error {
	closer, ok := c.inner.(io.Closer)
	if !ok {
		return nil
	}
	if err := closer.Close(); err != nil {
		return fmt.Errorf("close search index: %w", err)
	}

	return nil
}

func (c *CachedSearchIndex) invalidate() {
	c.mu.Lock()
	c.generation++
	clear(c.entries)
	c.order.Init()
	c.retained = 0
	c.mu.Unlock()
}

func (c *CachedSearchIndex) store(key string, generation uint64, results SearchResultSet) {
	probe := &cachedSearchEntry{generation: generation, key: key, results: results}
	probe.retained = retainedSearchEntryBytes(probe)
	c.mu.Lock()
	if c.generation != generation {
		c.mu.Unlock()
		return
	}
	if probe.retained > c.limit {
		if existing, exists := c.entries[key]; exists {
			c.removeLocked(existing)
		}
		c.mu.Unlock()

		return
	}
	c.mu.Unlock()

	key = strings.Clone(key)
	stored := detachCachedSearchResultSet(results)
	entry := &cachedSearchEntry{generation: generation, key: key, results: stored}
	entry.retained = retainedSearchEntryBytes(entry)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != generation {
		return
	}
	if existing, exists := c.entries[key]; exists {
		c.removeLocked(existing)
	}
	entry.element = c.order.PushFront(entry)
	c.entries[key] = entry
	c.retained += entry.retained
	for len(c.entries) > c.capacity || c.retained > c.limit {
		c.removeLocked(c.order.Back().Value.(*cachedSearchEntry))
	}
}

func (c *CachedSearchIndex) removeLocked(entry *cachedSearchEntry) {
	c.order.Remove(entry.element)
	delete(c.entries, entry.key)
	c.retained -= entry.retained
}
