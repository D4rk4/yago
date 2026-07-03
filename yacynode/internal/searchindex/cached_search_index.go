package searchindex

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/D4rk4/yago/yacynode/internal/documentstore"
)

const defaultQueryCacheCapacity = 256

type CachedSearchIndex struct {
	inner    SearchIndex
	capacity int

	mu         sync.Mutex
	entries    map[string]cachedSearchEntry
	order      []string
	generation uint64
}

type cachedSearchEntry struct {
	generation uint64
	results    SearchResultSet
}

func NewCachedSearchIndex(inner SearchIndex, capacity int) *CachedSearchIndex {
	if capacity <= 0 {
		capacity = defaultQueryCacheCapacity
	}

	return &CachedSearchIndex{
		inner:    inner,
		capacity: capacity,
		entries:  make(map[string]cachedSearchEntry, capacity),
	}
}

func (c *CachedSearchIndex) Index(ctx context.Context, doc documentstore.Document) error {
	err := c.inner.Index(ctx, doc)
	if err == nil {
		c.invalidate()
	}

	return err
}

func (c *CachedSearchIndex) Delete(ctx context.Context, docID string) error {
	err := c.inner.Delete(ctx, docID)
	if err == nil {
		c.invalidate()
	}

	return err
}

func (c *CachedSearchIndex) Search(ctx context.Context, req SearchRequest) (SearchResultSet, error) {
	key := cacheKey(req)

	c.mu.Lock()
	generation := c.generation
	if entry, ok := c.entries[key]; ok && entry.generation == generation {
		results := cloneResultSet(entry.results)
		c.mu.Unlock()

		return results, nil
	}
	c.mu.Unlock()

	results, err := c.inner.Search(ctx, req)
	if err != nil {
		return SearchResultSet{}, err
	}
	c.store(key, generation, results)

	return results, nil
}

func (c *CachedSearchIndex) Stats(ctx context.Context) (IndexStats, error) {
	return c.inner.Stats(ctx)
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
	c.mu.Unlock()
}

func (c *CachedSearchIndex) store(key string, generation uint64, results SearchResultSet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != generation {
		return
	}
	if _, exists := c.entries[key]; !exists {
		c.order = append(c.order, key)
	}
	c.entries[key] = cachedSearchEntry{generation: generation, results: cloneResultSet(results)}
	for len(c.order) > c.capacity {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
}

func cacheKey(req SearchRequest) string {
	var builder strings.Builder
	writeCacheField(&builder, req.Query)
	writeCacheField(&builder, strconv.Itoa(req.MaxResults))
	writeCacheField(&builder, strconv.FormatBool(req.IncludeRaw))
	writeCacheField(&builder, req.Language)
	writeCacheField(&builder, strings.Join(req.ExcludeTerms, "\x1f"))
	writeCacheField(&builder, strings.Join(req.IncludeDomain, "\x1f"))
	writeCacheField(&builder, strings.Join(req.ExcludeDomain, "\x1f"))
	writeCacheField(&builder, strconv.FormatInt(req.Since.UnixNano(), 10))
	writeCacheField(&builder, strconv.FormatInt(req.Until.UnixNano(), 10))

	return builder.String()
}

func writeCacheField(builder *strings.Builder, value string) {
	builder.WriteString(value)
	builder.WriteByte(0)
}

func cloneResultSet(set SearchResultSet) SearchResultSet {
	if set.Results == nil {
		return SearchResultSet{Total: set.Total}
	}
	results := make([]SearchResult, len(set.Results))
	copy(results, set.Results)

	return SearchResultSet{Results: results, Total: set.Total}
}
