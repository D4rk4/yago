package urldenylist

import (
	"sync"
	"sync/atomic"
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
