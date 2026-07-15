package ingest

import "github.com/D4rk4/yago/yagocrawlcontract"

func fitExtractedText(batch IngestBatch) (IngestBatch, []byte, bool) {
	text := batch.Document.ExtractedText
	lower, upper := 0, len(text)
	var bestBatch IngestBatch
	var best []byte
	for lower <= upper {
		middle := lower + (upper-lower)/2
		candidate := batch
		candidate.Document.ExtractedText = boundedIngestText(text, middle)
		data := encodedValidatedIngestBatch(candidate)
		if len(data) <= yagocrawlcontract.MaximumIngestBatchBytes {
			bestBatch = candidate
			best = data
			lower = middle + 1
		} else {
			upper = middle - 1
		}
	}
	if best == nil {
		batch.Document.ExtractedText = ""

		return batch, encodedValidatedIngestBatch(batch), false
	}

	return bestBatch, best, true
}
