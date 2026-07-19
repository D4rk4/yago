package searchindex

import (
	"fmt"
	"sync"

	"github.com/blevesearch/bleve/v2"
)

const bleveShardCommitLanes = 4

func commitBleveShardBatches(shards []bleve.Index, batches []*bleve.Batch) error {
	errorsByShard := make([]error, len(shards))
	touchedShards := make([]int, 0, len(batches))
	for shardNumber, batch := range batches {
		if batch != nil {
			touchedShards = append(touchedShards, shardNumber)
		}
	}
	if len(touchedShards) == 0 {
		return nil
	}

	shardQueue := make(chan int)
	var commits sync.WaitGroup
	for range min(bleveShardCommitLanes, len(touchedShards)) {
		commits.Go(func() {
			for shardNumber := range shardQueue {
				errorsByShard[shardNumber] = shards[shardNumber].Batch(
					batches[shardNumber],
				)
			}
		})
	}
	for _, shardNumber := range touchedShards {
		shardQueue <- shardNumber
	}
	close(shardQueue)
	commits.Wait()
	for shardNumber, err := range errorsByShard {
		if err != nil {
			return fmt.Errorf("index batch shard %d: %w", shardNumber+1, err)
		}
	}

	return nil
}
