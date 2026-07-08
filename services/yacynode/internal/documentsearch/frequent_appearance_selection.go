package documentsearch

import "container/heap"

type leastFrequentFirst []termAppearance

func (h leastFrequentFirst) Len() int           { return len(h) }
func (h leastFrequentFirst) Less(i, j int) bool { return h[i].occurrences < h[j].occurrences }
func (h leastFrequentFirst) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *leastFrequentFirst) Push(x any)        { *h = append(*h, x.(termAppearance)) }

func (h *leastFrequentFirst) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]

	return last
}

type mostFrequentAppearances struct {
	limit int
	kept  leastFrequentFirst
}

func (m *mostFrequentAppearances) consider(appearance termAppearance) {
	if m.limit <= 0 || len(m.kept) < m.limit {
		heap.Push(&m.kept, appearance)

		return
	}
	if m.kept[0].occurrences < appearance.occurrences {
		m.kept[0] = appearance
		heap.Fix(&m.kept, 0)
	}
}

func (m *mostFrequentAppearances) collected() []termAppearance {
	return m.kept
}
