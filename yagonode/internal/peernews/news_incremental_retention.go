package peernews

import (
	"bytes"
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type newsStoredState struct {
	queueRecords int
	queueBytes   int
	knownRecords int
}

func (p *Pool) loadStoredState(ctx context.Context) error {
	var state newsStoredState
	knownNews := newBoundedNewestNews(p.retention.knownRecords, -1)
	queuedNews := newBoundedNewestNews(p.retention.queueRecords, p.retention.queueBytes)
	if err := p.vault.View(ctx, func(tx *vault.Txn) error {
		return p.readStoredNewsState(ctx, tx, &state, knownNews, queuedNews)
	}); err != nil {
		return fmt.Errorf("load stored news state: %w", err)
	}
	p.stored = state
	p.knownNewsRetention = knownNews
	p.queuedNewsRetention = queuedNews

	return nil
}

var newsQueues = []Queue{Incoming, Processed, Outgoing, Published}

func parseQueueKey(key vault.Key) (Queue, bool) {
	for _, queue := range newsQueues {
		prefix := queuePrefix(queue)
		if len(key) == len(prefix)+8 && bytes.HasPrefix(key, prefix) {
			return queue, true
		}
	}

	return "", false
}
