package dhtexchange

func (q *OutboundQueue) Requeue(chunk OutboundChunk) int {
	return q.add(chunk.Peer, chunk.Postings)
}
