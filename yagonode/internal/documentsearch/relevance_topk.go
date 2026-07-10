package documentsearch

import (
	"container/heap"
	"slices"

	"github.com/D4rk4/yago/yagomodel"
)

// mostRelevantDocuments returns the identifiers of the up-to-limit most relevant
// documents in full ranking order, selecting them with a bounded heap so a large
// matching set is never fully ordered. It is result-identical to ranking every
// document and truncating: documentRelevanceOrder is a strict total order, so the
// top-limit prefix is unique. A non-positive limit (unbounded) or a limit that
// spans the whole set falls back to ordering everything, reusing the full sort.
func mostRelevantDocuments(
	documents map[yagomodel.Hash]matchedDocument,
	limit int,
) []yagomodel.Hash {
	if limit <= 0 || limit >= len(documents) {
		return takeMostRelevant(documentsOrderedByRelevance(documents), limit)
	}

	leastRelevantFirst := make(documentHeap, 0, limit+1)
	for _, document := range documents {
		heap.Push(&leastRelevantFirst, document)
		if leastRelevantFirst.Len() > limit {
			heap.Pop(&leastRelevantFirst)
		}
	}

	kept := []matchedDocument(leastRelevantFirst)
	slices.SortFunc(kept, documentRelevanceOrder)
	identifiers := make([]yagomodel.Hash, 0, len(kept))
	for _, document := range kept {
		identifiers = append(identifiers, document.identifier)
	}

	return identifiers
}

// documentHeap orders matched documents least-relevant-first, so a bounded top-k
// selection evicts the least relevant kept document whenever the heap overflows
// the requested limit.
type documentHeap []matchedDocument

func (h documentHeap) Len() int      { return len(h) }
func (h documentHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h documentHeap) Less(i, j int) bool {
	return documentRelevanceOrder(h[i], h[j]) > 0
}

func (h *documentHeap) Push(x any) {
	//nolint:forcetypeassert // a documentHeap only ever holds matchedDocument.
	*h = append(*h, x.(matchedDocument))
}

func (h *documentHeap) Pop() any {
	old := *h
	last := len(old) - 1
	document := old[last]
	*h = old[:last]

	return document
}
