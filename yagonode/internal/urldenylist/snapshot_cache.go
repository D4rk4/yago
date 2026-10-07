package urldenylist

import (
	"sync"
	"sync/atomic"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type snapshotCache struct {
	mutations sync.Mutex
	current   atomic.Pointer[Snapshot]
}

func (c *snapshotCache) storeAdded(kind Kind, value string) {
	next := cloneSnapshot(*c.current.Load())
	switch kind {
	case KindURL:
		next.urls[value] = struct{}{}
	case KindDomain:
		next.domains[value] = struct{}{}
	}
	next = compileSnapshot(next)
	c.current.Store(&next)
}

func (c *snapshotCache) storeRemoved(kind Kind, value string) {
	next := cloneSnapshot(*c.current.Load())
	switch kind {
	case KindURL:
		delete(next.urls, value)
	case KindDomain:
		delete(next.domains, value)
	}
	next = compileSnapshot(next)
	c.current.Store(&next)
}

func cloneSnapshot(current Snapshot) Snapshot {
	next := Snapshot{
		urls:    make(map[string]struct{}, len(current.urls)),
		domains: make(map[string]struct{}, len(current.domains)),
	}
	for value := range current.urls {
		next.urls[value] = struct{}{}
	}
	for value := range current.domains {
		next.domains[value] = struct{}{}
	}

	return next
}

func compileSnapshot(raw Snapshot) Snapshot {
	compiled := Snapshot{
		urls:           raw.urls,
		canonicalURLs:  make(map[string]int, len(raw.urls)),
		domains:        raw.domains,
		canonicalHosts: make(map[string]struct{}, len(raw.domains)),
		wireURLs:       make([]string, 0, len(raw.urls)),
		wireDomains:    make([]string, 0, len(raw.domains)),
	}
	for value := range raw.urls {
		canonical, valid := yagocrawlcontract.CanonicalURL(value)
		if valid {
			compiled.canonicalURLs[canonical]++
			compiled.wireURLs = append(compiled.wireURLs, canonical)
		} else {
			compiled.wireURLs = append(compiled.wireURLs, value)
		}
	}
	for value := range raw.domains {
		canonical := normalize(KindDomain, value)
		if canonical != "" {
			compiled.canonicalHosts[canonical] = struct{}{}
			compiled.wireDomains = append(compiled.wireDomains, canonical)
		} else {
			compiled.wireDomains = append(compiled.wireDomains, value)
		}
	}

	return compiled
}
