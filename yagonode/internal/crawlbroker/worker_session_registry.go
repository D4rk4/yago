package crawlbroker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const maximumWorkerSessions = 4096

type workerSession struct {
	id             string
	cancel         context.CancelFunc
	connected      bool
	lastSeen       time.Time
	generation     uint64
	deliveryCredit *workerSessionDeliveryCredit
}

type workerSessionEntry struct {
	mutex      sync.Mutex
	current    workerSession
	references int
}

type workerSessionRegistry struct {
	mutex          sync.Mutex
	sessions       map[string]*workerSessionEntry
	capacity       int
	retention      time.Duration
	now            func() time.Time
	nextGeneration atomic.Uint64
}

func newWorkerSessionRegistry(
	capacity int,
	retentionValues ...time.Duration,
) *workerSessionRegistry {
	retention := DefaultLeaseTTL
	if len(retentionValues) > 0 && retentionValues[0] > 0 {
		retention = retentionValues[0]
	}
	return &workerSessionRegistry{
		sessions:  make(map[string]*workerSessionEntry, capacity),
		capacity:  max(1, capacity),
		retention: retention,
		now:       time.Now,
	}
}

func (r *workerSessionRegistry) activate(
	workerID string,
	workerSessionID string,
	cancel context.CancelFunc,
	adopt func() error,
) (uint64, error) {
	if !validCrawlerLeaseIdentity(workerID, workerSessionID) {
		return 0, fmt.Errorf("invalid worker session identity")
	}
	entry, err := r.retain(workerID, true)
	if err != nil {
		return 0, err
	}
	defer r.releaseRetention(entry)
	entry.mutex.Lock()
	defer entry.mutex.Unlock()
	if entry.current.connected {
		return 0, errWorkerSessionActive
	}
	if err := adopt(); err != nil {
		return 0, err
	}
	generation := r.nextGeneration.Add(1)
	entry.current = workerSession{
		id: workerSessionID, cancel: cancel, connected: true, lastSeen: r.now(),
		generation: generation, deliveryCredit: newWorkerSessionDeliveryCredit(),
	}

	return generation, nil
}

func (r *workerSessionRegistry) deactivate(
	workerID string,
	workerSessionID string,
	generation uint64,
) {
	entry, err := r.retain(workerID, false)
	if err != nil {
		return
	}
	defer r.releaseRetention(entry)
	entry.mutex.Lock()
	current := entry.current
	if current.id != workerSessionID || current.generation != generation {
		entry.mutex.Unlock()

		return
	}
	deliveryCredit := current.deliveryCredit
	current.cancel = nil
	current.connected = false
	current.lastSeen = r.now()
	entry.current = current
	entry.mutex.Unlock()
	if deliveryCredit != nil {
		deliveryCredit.stop()
	}
}

func (r *workerSessionRegistry) whileCurrentRegistration(
	workerID string,
	workerSessionID string,
	generation uint64,
	operation func() error,
) error {
	entry, err := r.retain(workerID, false)
	if err != nil {
		return errLeaseLost
	}
	defer r.releaseRetention(entry)
	entry.mutex.Lock()
	defer entry.mutex.Unlock()
	current := entry.current
	if current.id != workerSessionID || current.generation != generation {
		return errLeaseLost
	}

	return operation()
}

func (r *workerSessionRegistry) current(workerID string, workerSessionID string) bool {
	entry, err := r.retain(workerID, false)
	if err != nil {
		return false
	}
	defer r.releaseRetention(entry)
	entry.mutex.Lock()
	defer entry.mutex.Unlock()
	current := entry.current
	if current.id == workerSessionID {
		if !current.connected {
			current.lastSeen = r.now()
			entry.current = current
		}

		return true
	}

	return false
}

func (r *workerSessionRegistry) whileCurrent(
	workerID string,
	workerSessionID string,
	operation func() error,
) error {
	entry, err := r.retain(workerID, false)
	if err != nil {
		return errLeaseLost
	}
	defer r.releaseRetention(entry)
	entry.mutex.Lock()
	defer entry.mutex.Unlock()
	current := entry.current
	if current.id != workerSessionID {
		return errLeaseLost
	}
	if !current.connected {
		current.lastSeen = r.now()
		entry.current = current
	}

	return operation()
}

func (r *workerSessionRegistry) registration(workerID string) workerSession {
	entry, err := r.retain(workerID, false)
	if err != nil {
		return workerSession{}
	}
	defer r.releaseRetention(entry)
	entry.mutex.Lock()
	defer entry.mutex.Unlock()

	return entry.current
}

func (r *workerSessionRegistry) retain(
	workerID string,
	create bool,
) (*workerSessionEntry, error) {
	r.mutex.Lock()
	entry, exists := r.sessions[workerID]
	if !exists && create {
		if len(r.sessions) >= r.capacity {
			r.removeExpiredInactiveLocked(r.now())
		}
		if len(r.sessions) >= r.capacity {
			r.mutex.Unlock()

			return nil, fmt.Errorf("worker session capacity %d reached", r.capacity)
		}
		entry = &workerSessionEntry{}
		r.sessions[workerID] = entry
		exists = true
	}
	if !exists {
		r.mutex.Unlock()

		return nil, errLeaseLost
	}
	entry.references++
	r.mutex.Unlock()

	return entry, nil
}

func (r *workerSessionRegistry) releaseRetention(entry *workerSessionEntry) {
	r.mutex.Lock()
	entry.references--
	r.mutex.Unlock()
}

func (r *workerSessionRegistry) removeExpiredInactiveLocked(now time.Time) {
	for workerID, entry := range r.sessions {
		if entry.references == 0 && !entry.current.connected &&
			!entry.current.lastSeen.Add(r.retention).After(now) {
			delete(r.sessions, workerID)
		}
	}
}
