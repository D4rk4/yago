package faviconproxy

import (
	"container/list"
	"crypto/sha256"
	"strings"
	"sync"
	"time"
)

const (
	maxCacheBytes   = 32 << 20
	maxCacheEntries = 65536
)

type iconCache struct {
	mu         sync.Mutex
	hosts      map[string]*hostEntry
	bodies     map[[sha256.Size]byte]*iconBody
	order      *list.List
	totalBytes int
	maxBytes   int
	maxEntries int
}

type hostEntry struct {
	host          string
	digest        [sha256.Size]byte
	contentType   string
	expires       time.Time
	element       *list.Element
	retainedBytes int
	hasBody       bool
}

type iconBody struct {
	data          []byte
	refs          int
	retainedBytes int
}

func newIconCache(maxBytes, maxEntries int) *iconCache {
	return &iconCache{
		hosts:      map[string]*hostEntry{},
		bodies:     map[[sha256.Size]byte]*iconBody{},
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
	if !entry.hasBody {
		return nil, entry.contentType, true
	}

	return c.bodies[entry.digest].data, entry.contentType, true
}

// put stores a fetched icon (or a negative body=nil entry) for the host,
// deduplicating the body by digest and evicting least-recently-used hosts
// until the byte and entry budgets hold.
func (c *iconCache) put(host string, body []byte, contentType string, expires time.Time) {
	entryBytes := retainedIconHostBytes(host, contentType)
	hasBody := len(body) > 0
	var digest [sha256.Size]byte
	if hasBody {
		digest = bodyDigest(body)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	minimumBodyBytes := 0
	additionalBodyBytes := 0
	storedBody, bodyFound := c.bodies[digest]
	if hasBody {
		minimumBodyBytes = retainedIconBodyBytes(len(body))
		additionalBodyBytes = minimumBodyBytes
		if bodyFound {
			minimumBodyBytes = storedBody.retainedBytes
			additionalBodyBytes = 0
		}
	}
	if c.maxBytes <= 0 || c.maxEntries <= 0 ||
		entryBytes+minimumBodyBytes > c.maxBytes {
		return
	}
	if hasBody {
		if bodyFound {
			storedBody.refs++
		} else {
			ownedBody := make([]byte, len(body))
			copy(ownedBody, body)
			storedBody = &iconBody{
				data:          ownedBody,
				refs:          1,
				retainedBytes: additionalBodyBytes,
			}
			c.bodies[digest] = storedBody
			c.totalBytes += additionalBodyBytes
		}
	}
	if existing, found := c.hosts[host]; found {
		c.removeLocked(existing)
	}
	for (c.totalBytes+entryBytes > c.maxBytes || c.order.Len() >= c.maxEntries) &&
		c.order.Len() > 0 {
		oldest, _ := c.order.Back().Value.(*hostEntry)
		c.removeLocked(oldest)
	}
	entry := &hostEntry{
		host:          strings.Clone(host),
		digest:        digest,
		contentType:   strings.Clone(contentType),
		expires:       expires,
		retainedBytes: entryBytes,
		hasBody:       hasBody,
	}
	entry.element = c.order.PushFront(entry)
	c.hosts[entry.host] = entry
	c.totalBytes += entry.retainedBytes
}

func (c *iconCache) removeLocked(entry *hostEntry) {
	c.order.Remove(entry.element)
	delete(c.hosts, entry.host)
	c.totalBytes -= entry.retainedBytes
	if !entry.hasBody {
		return
	}
	body := c.bodies[entry.digest]
	body.refs--
	if body.refs == 0 {
		c.totalBytes -= body.retainedBytes
		delete(c.bodies, entry.digest)
	}
}

func bodyDigest(body []byte) [sha256.Size]byte {
	return sha256.Sum256(body)
}
