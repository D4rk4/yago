package yagonode

import (
	"sync"

	"github.com/D4rk4/yago/yagomodel"
)

const maximumPendingDHTInboundURLs = 65536

type dhtInboundReconciliation struct {
	mu             sync.Mutex
	pending        map[yagomodel.Hash]uint64
	arrival        []dhtInboundURLArrival
	firstArrival   int
	nextGeneration uint64
	capacity       int
}

type dhtInboundURLArrival struct {
	hash       yagomodel.Hash
	generation uint64
}

type dhtInboundURLResolution struct {
	arrivals int
	rejected int
	existing int
}

func newDHTInboundReconciliation(capacity int) *dhtInboundReconciliation {
	capacity = max(capacity, 0)
	return &dhtInboundReconciliation{
		pending:  make(map[yagomodel.Hash]uint64, min(capacity, 4096)),
		arrival:  make([]dhtInboundURLArrival, 0, min(capacity, 4096)),
		capacity: capacity,
	}
}

func (r *dhtInboundReconciliation) note(hashes []yagomodel.Hash) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, hash := range hashes {
		if _, exists := r.pending[hash]; exists {
			continue
		}
		if r.capacity == 0 {
			return
		}
		if len(r.pending) >= r.capacity {
			r.evictOldest()
		}
		r.nextGeneration++
		r.pending[hash] = r.nextGeneration
		r.arrival = append(r.arrival, dhtInboundURLArrival{
			hash:       hash,
			generation: r.nextGeneration,
		})
		r.compactArrivals()
	}
}

func (r *dhtInboundReconciliation) resolve(
	rows []yagomodel.URIMetadataRow,
	rejected []yagomodel.Hash,
	existing []yagomodel.Hash,
) int {
	if r == nil {
		return 0
	}
	resolutions := make(map[yagomodel.Hash]dhtInboundURLResolution, len(rows))
	for _, row := range rows {
		hash, err := row.URLHash()
		if err == nil {
			plain := hash.Hash()
			resolution := resolutions[plain]
			resolution.arrivals++
			resolutions[plain] = resolution
		}
	}
	for _, hash := range rejected {
		resolution := resolutions[hash]
		resolution.rejected++
		resolutions[hash] = resolution
	}
	for _, hash := range existing {
		resolution := resolutions[hash]
		resolution.existing++
		resolutions[hash] = resolution
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	reconciled := 0
	for hash, resolution := range resolutions {
		accepted := resolution.arrivals - resolution.rejected - resolution.existing
		if accepted <= 0 && resolution.existing == 0 {
			continue
		}
		if _, waiting := r.pending[hash]; !waiting {
			continue
		}
		delete(r.pending, hash)
		if accepted > 0 {
			reconciled++
		}
	}

	return reconciled
}

func (r *dhtInboundReconciliation) evictOldest() {
	for r.firstArrival < len(r.arrival) {
		arrival := r.arrival[r.firstArrival]
		r.firstArrival++
		if generation, exists := r.pending[arrival.hash]; exists &&
			generation == arrival.generation {
			delete(r.pending, arrival.hash)

			return
		}
	}
}

func (r *dhtInboundReconciliation) compactArrivals() {
	if len(r.arrival) <= r.capacity*2 {
		return
	}
	retained := r.arrival[:0]
	for _, arrival := range r.arrival[r.firstArrival:] {
		if generation, exists := r.pending[arrival.hash]; exists &&
			generation == arrival.generation {
			retained = append(retained, arrival)
		}
	}
	r.arrival = retained
	r.firstArrival = 0
}
