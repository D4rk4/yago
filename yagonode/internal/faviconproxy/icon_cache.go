package faviconproxy

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

const (
	// maxCacheBytes bounds the memory the cache spends on unique icon bodies.
	maxCacheBytes = 256 << 20
	// maxCacheEntries backstops the per-host bookkeeping: negative entries carry
	// no body bytes, so a byte budget alone would let host records grow without
	// bound under a scan of icon-less hosts.
	maxCacheEntries = 65536
)

// iconCache is an LRU keyed by host with the icon bodies deduplicated by
// content digest: hosts sharing one icon (CDN defaults, subdomain families)
// hold references to a single stored body, and eviction pops least-recently
// used hosts until both the unique-body byte budget and the entry backstop
// hold. Negative (placeholder) entries reference no body and cost no bytes.
type iconCache struct {
	mu         sync.Mutex
	hosts      map[string]*hostEntry
	bodies     map[string]*iconBody
	order      *list.List
	totalBytes int
	maxBytes   int
	maxEntries int
}

type hostEntry struct {
	host        string
	digest      string
	contentType string
	expires     time.Time
	element     *list.Element
}

type iconBody struct {
	data []byte
	refs int
}

func newIconCache(maxBytes, maxEntries int) *iconCache {
	return &iconCache{
		hosts:      map[string]*hostEntry{},
		bodies:     map[string]*iconBody{},
		order:      list.New(),
		maxBytes:   maxBytes,
		maxEntries: maxEntries,
	}
}

// get returns the cached body (nil for a negative entry) and content type, and
// refreshes the host's recency. Expired entries are dropped on sight.
func (c *iconCache) get(host string) (body []byte, contentType string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, found := c.hosts[host]
	if !found {
		return nil, "", false
	}
	if clock().After(entry.expires) {
		c.removeLocked(entry)

		return nil, "", false
	}
	c.order.MoveToFront(entry.element)
	if entry.digest == "" {
		return nil, entry.contentType, true
	}

	return c.bodies[entry.digest].data, entry.contentType, true
}

// put stores a fetched icon (or a negative body=nil entry) for the host,
// deduplicating the body by digest and evicting least-recently-used hosts
// until the byte and entry budgets hold.
func (c *iconCache) put(host string, body []byte, contentType string, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, found := c.hosts[host]; found {
		c.removeLocked(existing)
	}

	entry := &hostEntry{host: host, contentType: contentType, expires: expires}
	if len(body) > 0 {
		entry.digest = bodyDigest(body)
		if stored, found := c.bodies[entry.digest]; found {
			stored.refs++
		} else {
			c.bodies[entry.digest] = &iconBody{data: body, refs: 1}
			c.totalBytes += len(body)
		}
	}
	entry.element = c.order.PushFront(entry)
	c.hosts[host] = entry

	for (c.totalBytes > c.maxBytes || c.order.Len() > c.maxEntries) && c.order.Len() > 1 {
		// The list is owned by this file and holds host entries only.
		oldest, _ := c.order.Back().Value.(*hostEntry)
		c.removeLocked(oldest)
	}
}

func (c *iconCache) removeLocked(entry *hostEntry) {
	c.order.Remove(entry.element)
	delete(c.hosts, entry.host)
	if entry.digest == "" {
		return
	}
	body := c.bodies[entry.digest]
	body.refs--
	if body.refs == 0 {
		c.totalBytes -= len(body.data)
		delete(c.bodies, entry.digest)
	}
}

func bodyDigest(body []byte) string {
	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}
