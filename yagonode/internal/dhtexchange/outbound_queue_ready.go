package dhtexchange

import "github.com/D4rk4/yago/yagomodel"

type OutboundPeerReady func(yagomodel.Hash) bool

func (q *OutboundQueue) DequeueLargestReady(ready OutboundPeerReady) (OutboundChunk, bool) {
	if ready == nil {
		return q.DequeueLargest()
	}

	var selected yagomodel.Hash
	selectedCount := -1
	for hash, chunk := range q.chunks {
		if !ready(hash) {
			continue
		}
		count := len(chunk.Postings)
		if count > selectedCount || count == selectedCount && hash.String() < selected.String() {
			selected = hash
			selectedCount = count
		}
	}
	if selectedCount < 0 {
		return OutboundChunk{}, false
	}

	chunk := cloneChunk(*q.chunks[selected])
	delete(q.chunks, selected)

	return chunk, true
}
