package yacycrawler_test

import (
	"context"
	"sync"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/yacycrawler"
)

type fakeNodeIngest struct {
	source  yacycrawler.Receiver[yacycrawler.IngestBatch]
	mu      sync.Mutex
	batches []yacycrawler.IngestBatch
}

func newFakeNodeIngest(source yacycrawler.Receiver[yacycrawler.IngestBatch]) *fakeNodeIngest {
	return &fakeNodeIngest{source: source}
}

func (n *fakeNodeIngest) Run(ctx context.Context) {
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

func (n *fakeNodeIngest) Batches() []yacycrawler.IngestBatch {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]yacycrawler.IngestBatch, len(n.batches))
	copy(out, n.batches)
	return out
}
