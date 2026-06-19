package yacycrawlcontract

import (
	"encoding/json"
	"fmt"
)

func MarshalIngestBatch(batch IngestBatch) ([]byte, error) {
	data, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("marshal ingest batch: %w", err)
	}
	return data, nil
}

func UnmarshalIngestBatch(data []byte) (IngestBatch, error) {
	var batch IngestBatch
	if err := json.Unmarshal(data, &batch); err != nil {
		return IngestBatch{}, fmt.Errorf("unmarshal ingest batch: %w", err)
	}
	return batch, nil
}
