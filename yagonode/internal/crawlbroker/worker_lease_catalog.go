package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type workerLeaseSession struct {
	workerID  string
	sessionID string
}

type workerLeaseCatalog struct {
	active map[workerLeaseSession]int
}

func loadWorkerLeaseCatalog(
	ctx context.Context,
	storage *vault.Vault,
	leases *vault.Collection[leaseRecord],
) (*workerLeaseCatalog, error) {
	catalog := &workerLeaseCatalog{active: make(map[workerLeaseSession]int)}
	err := storage.View(ctx, func(tx *vault.Txn) error {
		return leases.Scan(tx, nil, func(_ vault.Key, record leaseRecord) (bool, error) {
			catalog.add(record)

			return true, nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("load worker crawl lease catalog: %w", err)
	}

	return catalog, nil
}

func (c *workerLeaseCatalog) add(record leaseRecord) {
	if record.Deferred || record.WorkerID == "" || record.WorkerSessionID == "" {
		return
	}
	key := workerLeaseSession{workerID: record.WorkerID, sessionID: record.WorkerSessionID}
	c.active[key]++
}

func (c *workerLeaseCatalog) remove(record leaseRecord) bool {
	if record.Deferred || record.WorkerID == "" || record.WorkerSessionID == "" {
		return false
	}
	key := workerLeaseSession{workerID: record.WorkerID, sessionID: record.WorkerSessionID}
	assigned, found := c.active[key]
	if !found || assigned <= 0 {
		return false
	}
	remaining := assigned - 1
	if remaining <= 0 {
		delete(c.active, key)

		return true
	}
	c.active[key] = remaining

	return true
}

func (c *workerLeaseCatalog) reassignWorker(workerID string, sessionID string, active int) {
	for key := range c.active {
		if key.workerID == workerID {
			delete(c.active, key)
		}
	}
	if active > 0 {
		c.active[workerLeaseSession{workerID: workerID, sessionID: sessionID}] = active
	}
}

func (c *workerLeaseCatalog) reached(workerID string, sessionID string, capacity int) bool {
	return c.active[workerLeaseSession{workerID: workerID, sessionID: sessionID}] >= capacity
}
