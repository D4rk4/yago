package peernews

import (
	"bytes"
	"container/heap"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type boundedNewsOrder []*retainedNewsRecord

func (o boundedNewsOrder) Len() int { return len(o) }

func (o boundedNewsOrder) Less(left, right int) bool {
	return retainedNewsBefore(*o[left], *o[right])
}

func retainedNewsBefore(left, right retainedNewsRecord) bool {
	comparison := left.created.Compare(right.created)
	if comparison != 0 {
		return comparison < 0
	}
	if comparison = bytes.Compare(left.tie, right.tie); comparison != 0 {
		return comparison < 0
	}

	return bytes.Compare(left.key, right.key) < 0
}

func (o boundedNewsOrder) Swap(left, right int) {
	o[left], o[right] = o[right], o[left]
	o[left].index = left
	o[right].index = right
}

func (o *boundedNewsOrder) Push(value any) {
	record := value.(*retainedNewsRecord)
	record.index = len(*o)
	*o = append(*o, record)
}

func (o *boundedNewsOrder) Pop() any {
	old := *o
	last := len(old) - 1
	record := old[last]
	old[last] = nil
	*o = old[:last]
	record.index = -1

	return record
}

type boundedNewestNews struct {
	records     boundedNewsOrder
	keys        map[string]*retainedNewsRecord
	bytes       int
	recordLimit int
	byteLimit   int
}

func newBoundedNewestNews(recordLimit, byteLimit int) *boundedNewestNews {
	capacity := max(recordLimit, 0)

	return &boundedNewestNews{
		records:     make(boundedNewsOrder, 0, capacity),
		keys:        make(map[string]*retainedNewsRecord, capacity),
		recordLimit: recordLimit,
		byteLimit:   byteLimit,
	}
}

func (n *boundedNewestNews) Add(record retainedNewsRecord) []retainedNewsRecord {
	if n.recordLimit <= 0 || n.byteLimit == 0 {
		return []retainedNewsRecord{record}
	}
	if existing, found := n.keys[string(record.key)]; found {
		n.remove(existing)
	}
	removed := make([]retainedNewsRecord, 0, 2)
	for n.records.Len() >= n.recordLimit {
		if !retainedNewsBefore(*n.records[0], record) {
			return append(removed, record)
		}
		removed = append(removed, n.removeOldest())
	}
	stored := record
	heap.Push(&n.records, &stored)
	n.keys[string(stored.key)] = &stored
	n.bytes += stored.bytes
	for n.byteLimit > 0 && n.bytes > n.byteLimit {
		removed = append(removed, n.removeOldest())
	}

	return removed
}

func (n *boundedNewestNews) Remove(key vault.Key) (retainedNewsRecord, bool) {
	record, found := n.keys[string(key)]
	if !found {
		return retainedNewsRecord{}, false
	}

	return n.remove(record), true
}

func (n *boundedNewestNews) Contains(key vault.Key) bool {
	_, found := n.keys[string(key)]

	return found
}

func (n *boundedNewestNews) removeOldest() retainedNewsRecord {
	return n.remove(heap.Pop(&n.records).(*retainedNewsRecord))
}

func (n *boundedNewestNews) remove(record *retainedNewsRecord) retainedNewsRecord {
	if record.index >= 0 {
		heap.Remove(&n.records, record.index)
	}
	delete(n.keys, string(record.key))
	n.bytes -= record.bytes

	return *record
}
