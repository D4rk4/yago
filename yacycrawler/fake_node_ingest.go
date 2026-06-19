package yacycrawler

import (
	"context"
	"sync"
)

type FakeNodeIngest struct {
	source  Receiver[IngestBatch]
	mu      sync.Mutex
	batches []IngestBatch
}

func NewFakeNodeIngest(source Receiver[IngestBatch]) *FakeNodeIngest {
	return &FakeNodeIngest{source: source}
}

func (n *FakeNodeIngest) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case batch, ok := <-n.source.Receive():
			if !ok {
				return
			}
			n.mu.Lock()
			n.batches = append(n.batches, batch)
			n.mu.Unlock()
		}
	}
}

func (n *FakeNodeIngest) Batches() []IngestBatch {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]IngestBatch, len(n.batches))
	copy(out, n.batches)
	return out
}
