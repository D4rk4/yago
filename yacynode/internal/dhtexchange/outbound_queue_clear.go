package dhtexchange

func (q *OutboundQueue) Clear() {
	q.chunks = nil
}
