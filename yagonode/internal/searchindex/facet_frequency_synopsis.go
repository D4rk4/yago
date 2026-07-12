package searchindex

import (
	"container/heap"
	"strings"
)

type facetFrequencySynopsis struct {
	limit   int
	entries map[string]*facetFrequencyEntry
	queue   facetFrequencyQueue
}

type facetFrequencyEntry struct {
	term      string
	frequency int
	error     int
	position  int
}

type facetFrequencyQueue []*facetFrequencyEntry

func newFacetFrequencySynopsis(limit int) *facetFrequencySynopsis {
	return &facetFrequencySynopsis{
		limit:   max(0, limit),
		entries: make(map[string]*facetFrequencyEntry, max(0, limit)),
		queue:   make(facetFrequencyQueue, 0, max(0, limit)),
	}
}

func (s *facetFrequencySynopsis) observe(term string) {
	if s.limit == 0 {
		return
	}
	if entry, found := s.entries[term]; found {
		entry.frequency++
		heap.Fix(&s.queue, entry.position)

		return
	}
	term = strings.Clone(term)
	if len(s.entries) < s.limit {
		entry := &facetFrequencyEntry{term: term, frequency: 1}
		s.entries[term] = entry
		heap.Push(&s.queue, entry)

		return
	}
	entry := heap.Pop(&s.queue).(*facetFrequencyEntry)
	delete(s.entries, entry.term)
	entry.error = entry.frequency
	entry.term = term
	entry.frequency++
	s.entries[term] = entry
	heap.Push(&s.queue, entry)
}

func (s *facetFrequencySynopsis) terms() []FacetTerm {
	terms := make([]FacetTerm, 0, len(s.entries))
	for term, entry := range s.entries {
		terms = append(terms, FacetTerm{
			Term:  term,
			Count: entry.frequency - entry.error,
		})
	}

	return terms
}

func (q facetFrequencyQueue) Len() int {
	return len(q)
}

func (q facetFrequencyQueue) Less(left, right int) bool {
	if q[left].frequency != q[right].frequency {
		return q[left].frequency < q[right].frequency
	}

	return q[left].term > q[right].term
}

func (q facetFrequencyQueue) Swap(left, right int) {
	q[left], q[right] = q[right], q[left]
	q[left].position = left
	q[right].position = right
}

func (q *facetFrequencyQueue) Push(value any) {
	entry := value.(*facetFrequencyEntry)
	entry.position = len(*q)
	*q = append(*q, entry)
}

func (q *facetFrequencyQueue) Pop() any {
	previous := *q
	last := len(previous) - 1
	entry := previous[last]
	previous[last] = nil
	*q = previous[:last]

	return entry
}
