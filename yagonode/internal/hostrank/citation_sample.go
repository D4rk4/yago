package hostrank

import (
	"container/heap"
	"encoding/binary"
	"hash/fnv"
	"math"
	"sort"
)

const (
	maximumCitationURLBytes      = 2 << 10
	maximumCitationSampleBytes   = 16 << 20
	maximumCitationRetainedBytes = 5 << 10
	maximumDomainCitations       = maximumCitationSampleBytes / maximumCitationRetainedBytes
)

type CitationSample struct {
	limit         int
	retainedBytes int
	citations     citationPriorityQueue
	keys          map[string]struct{}
}

type scoredCitation struct {
	citation Citation
	key      string
	priority uint64
}

type citationPriorityQueue []scoredCitation

func NewCitationSample() *CitationSample {
	return newCitationSample(maximumDomainCitations)
}

func newCitationSample(limit int) *CitationSample {
	limit = min(max(0, limit), maximumDomainCitations)

	return &CitationSample{
		limit: limit,
		keys:  make(map[string]struct{}, limit),
	}
}

func (s *CitationSample) Add(citations ...Citation) {
	if s == nil || s.limit <= 0 {
		return
	}
	for _, citation := range citations {
		if len(citation.SourceURL) > maximumCitationURLBytes ||
			len(citation.TargetURL) > maximumCitationURLBytes {
			continue
		}
		candidate := scoredCitationFor(citation)
		if _, exists := s.keys[candidate.key]; exists {
			continue
		}
		if len(s.citations) < s.limit {
			heap.Push(&s.citations, candidate)
			s.keys[candidate.key] = struct{}{}
			s.retainedBytes += maximumCitationRetainedBytes
			continue
		}
		if !citationPrecedes(candidate, s.citations[0]) {
			continue
		}
		removed := heap.Pop(&s.citations).(scoredCitation)
		delete(s.keys, removed.key)
		heap.Push(&s.citations, candidate)
		s.keys[candidate.key] = struct{}{}
	}
}

func (s *CitationSample) Citations() []Citation {
	if s == nil {
		return nil
	}
	ordered := append(citationPriorityQueue(nil), s.citations...)
	sort.Slice(ordered, func(left, right int) bool {
		return citationPrecedes(ordered[left], ordered[right])
	})
	citations := make([]Citation, len(ordered))
	for index, citation := range ordered {
		citations[index] = citation.citation
	}

	return citations
}

func scoredCitationFor(citation Citation) scoredCitation {
	var confidence [8]byte
	binary.BigEndian.PutUint64(confidence[:], math.Float64bits(citation.Confidence))
	key := citation.SourceURL + "\x00" + citation.TargetURL + "\x00" + string(confidence[:])
	sourceEnd := len(citation.SourceURL)
	targetStart := sourceEnd + 1
	targetEnd := targetStart + len(citation.TargetURL)
	citation.SourceURL = key[:sourceEnd]
	citation.TargetURL = key[targetStart:targetEnd]
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(key))

	return scoredCitation{citation: citation, key: key, priority: digest.Sum64()}
}

func citationPrecedes(left, right scoredCitation) bool {
	if left.priority != right.priority {
		return left.priority < right.priority
	}

	return left.key < right.key
}

func (q citationPriorityQueue) Len() int {
	return len(q)
}

func (q citationPriorityQueue) Less(left, right int) bool {
	return citationPrecedes(q[right], q[left])
}

func (q citationPriorityQueue) Swap(left, right int) {
	q[left], q[right] = q[right], q[left]
}

func (q *citationPriorityQueue) Push(value any) {
	*q = append(*q, value.(scoredCitation))
}

func (q *citationPriorityQueue) Pop() any {
	old := *q
	last := len(old) - 1
	value := old[last]
	*q = old[:last]

	return value
}
