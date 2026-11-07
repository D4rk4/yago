package frontier

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (frontier *Frontier) Depth(ctx context.Context) (QueueDepth, error) {
	if err := checkContext(ctx); err != nil {
		return QueueDepth{}, err
	}
	var depth QueueDepth
	err := frontier.database.View(func(transaction *bolt.Tx) error {
		var err error
		depth, err = readQueueDepth(transaction.Bucket([]byte(metadataBucketName)))
		return err
	})
	if err != nil {
		return QueueDepth{}, fmt.Errorf("read frontier depth: %w", err)
	}
	return depth, nil
}

func (frontier *Frontier) DepthForRun(ctx context.Context, runID string) (QueueDepth, error) {
	if err := checkContext(ctx); err != nil {
		return QueueDepth{}, err
	}
	if runID == "" {
		return QueueDepth{}, fmt.Errorf("crawl run identity is required")
	}
	var depth QueueDepth
	err := frontier.database.View(func(transaction *bolt.Tx) error {
		var err error
		depth, err = readRunDepth(transaction.Bucket([]byte(runsBucketName)), runID)
		return err
	})
	if err != nil {
		return QueueDepth{}, fmt.Errorf("read crawl run depth: %w", err)
	}
	return depth, nil
}

func readRunDepth(runs *bolt.Bucket, runID string) (QueueDepth, error) {
	raw := runs.Get([]byte(runID))
	if raw == nil {
		return QueueDepth{}, nil
	}
	var depth QueueDepth
	if err := json.Unmarshal(raw, &depth); err != nil {
		return QueueDepth{}, fmt.Errorf("decode crawl run depth: %w", err)
	}
	return depth, nil
}

func writeRunDepth(runs *bolt.Bucket, runID string, depth QueueDepth) error {
	encoded, err := json.Marshal(depth)
	if err != nil {
		return fmt.Errorf("encode crawl run depth: %w", err)
	}
	return runs.Put([]byte(runID), encoded)
}

func readQueueDepth(metadata *bolt.Bucket) (QueueDepth, error) {
	var depth QueueDepth
	for state, target := range map[State]*uint64{
		StateQueued: &depth.Queued, StateFetching: &depth.Fetching,
		StateFetched: &depth.Fetched, StateIndexed: &depth.Indexed, StateFailed: &depth.Failed,
		StateDeferred: &depth.Deferred, StateBlocked: &depth.Blocked,
		StateCancelled: &depth.Cancelled,
	} {
		raw := metadata.Get(queueStateKey(state))
		if raw == nil {
			continue
		}
		if len(raw) != 8 {
			return QueueDepth{}, fmt.Errorf("invalid %s page count", state)
		}
		*target = binary.BigEndian.Uint64(raw)
	}
	return depth, nil
}

func writeQueueState(metadata *bolt.Bucket, state State, value uint64) error {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, value)
	return metadata.Put(queueStateKey(state), encoded)
}

func queueStateKey(state State) []byte {
	return []byte("pages." + string(state))
}

func (depth QueueDepth) For(state State) uint64 {
	switch state {
	case StateQueued:
		return depth.Queued
	case StateFetching:
		return depth.Fetching
	case StateFetched:
		return depth.Fetched
	case StateIndexed:
		return depth.Indexed
	case StateFailed:
		return depth.Failed
	case StateDeferred:
		return depth.Deferred
	case StateBlocked:
		return depth.Blocked
	case StateCancelled:
		return depth.Cancelled
	default:
		return 0
	}
}

func (depth QueueDepth) Pending() uint64 {
	return depth.Queued + depth.Fetching + depth.Deferred
}

func (depth QueueDepth) Total() uint64 {
	return depth.Pending() + depth.Fetched + depth.Indexed + depth.Failed + depth.Blocked + depth.Cancelled
}

func (depth *QueueDepth) Add(state State) {
	switch state {
	case StateQueued:
		depth.Queued++
	case StateFetching:
		depth.Fetching++
	case StateFetched:
		depth.Fetched++
	case StateIndexed:
		depth.Indexed++
	case StateFailed:
		depth.Failed++
	case StateDeferred:
		depth.Deferred++
	case StateBlocked:
		depth.Blocked++
	case StateCancelled:
		depth.Cancelled++
	}
}
