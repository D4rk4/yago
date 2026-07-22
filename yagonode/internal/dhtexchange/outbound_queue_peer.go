package dhtexchange

import "github.com/D4rk4/yago/yagomodel"

func (q *OutboundQueue) DequeuePeer(peer yagomodel.Hash) (OutboundChunk, bool) {
	chunk, known := q.chunks[peer]
	if !known {
		return OutboundChunk{}, false
	}

	detached := cloneChunk(*chunk)
	delete(q.chunks, peer)

	return detached, true
}
