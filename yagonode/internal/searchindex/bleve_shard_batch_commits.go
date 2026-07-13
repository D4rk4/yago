package searchindex

import (
	"fmt"
	"sync"

	"github.com/blevesearch/bleve/v2"
)

func commitBleveShardBatches(shards []bleve.Index, batches []*bleve.Batch) error {
	errorsByShard := make([]error, len(shards))
	var commits sync.WaitGroup
	for shardNumber, batch := range batches {
		if batch == nil {
			continue
		}
		commits.Add(1)
		go func() {
			defer commits.Done()
			errorsByShard[shardNumber] = shards[shardNumber].Batch(batch)
		}()
	}
	commits.Wait()
	for _, err := range errorsByShard {
		if err != nil {
			return fmt.Errorf("index batch: %w", err)
		}
	}

	return nil
}
