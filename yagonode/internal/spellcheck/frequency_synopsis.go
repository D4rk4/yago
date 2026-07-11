package spellcheck

type FrequencySynopsis struct {
	limit   int
	entries map[string]*frequencySynopsisEntry
	queue   frequencySynopsisQueue
}

type frequencySynopsisEntry struct {
	term      string
	frequency int
	error     int
	position  int
}

type frequencySynopsisQueue []*frequencySynopsisEntry

func NewFrequencySynopsis(limit int) *FrequencySynopsis {
	limit = max(0, limit)
	synopsis := &FrequencySynopsis{
		limit:   limit,
		entries: make(map[string]*frequencySynopsisEntry, limit),
		queue:   make(frequencySynopsisQueue, 0, limit),
	}

	return synopsis
}

func (s *FrequencySynopsis) ObserveText(text string) {
	if s.limit == 0 {
		return
	}
	termsInText(text, s.observeTerm)
}

func (s *FrequencySynopsis) Frequencies() map[string]int {
	frequencies := make(map[string]int, len(s.entries))
	for term, entry := range s.entries {
		frequencies[term] = entry.frequency - entry.error
	}

	return frequencies
}

func (s *FrequencySynopsis) observeTerm(term string) {
	if entry, found := s.entries[term]; found {
		entry.frequency++
		s.queue.increased(entry.position)

		return
	}
	if len(s.entries) < s.limit {
		entry := &frequencySynopsisEntry{term: term, frequency: 1}
		s.entries[term] = entry
		s.queue.push(entry)

		return
	}

	entry := s.queue[0]
	delete(s.entries, entry.term)
	entry.error = entry.frequency
	entry.term = term
	entry.frequency++
	s.entries[term] = entry
	s.queue.increased(0)
}

func (q *frequencySynopsisQueue) push(entry *frequencySynopsisEntry) {
	entry.position = len(*q)
	*q = append(*q, entry)
	q.decreased(entry.position)
}

func (q frequencySynopsisQueue) less(left, right int) bool {
	if q[left].frequency != q[right].frequency {
		return q[left].frequency < q[right].frequency
	}

	return q[left].term > q[right].term
}

func (q frequencySynopsisQueue) swap(left, right int) {
	q[left], q[right] = q[right], q[left]
	q[left].position = left
	q[right].position = right
}

func (q frequencySynopsisQueue) decreased(position int) {
	for position > 0 {
		parent := (position - 1) / 2
		if !q.less(position, parent) {
			return
		}
		q.swap(position, parent)
		position = parent
	}
}

func (q frequencySynopsisQueue) increased(position int) {
	for {
		left := position*2 + 1
		if left >= len(q) {
			return
		}
		smallest := left
		right := left + 1
		if right < len(q) && q.less(right, left) {
			smallest = right
		}
		if !q.less(smallest, position) {
			return
		}
		q.swap(position, smallest)
		position = smallest
	}
}
