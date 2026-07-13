package hostrank

import (
	"container/heap"
	"hash/fnv"
	"math"
	"net/url"
	"sort"
	"strings"
)

const (
	maximumCitationURLBytes      = 2 << 10
	maximumCitationDomainBytes   = 253
	maximumCitationSampleBytes   = 16 << 20
	maximumCitationRetainedBytes = 5 << 10
	maximumDomainCitations       = maximumCitationSampleBytes / maximumCitationRetainedBytes
)

type CitationSample struct {
	limit         int
	retainedBytes int
	citations     citationPriorityQueue
	pages         map[string]*scoredCitation
	domainEdges   map[string]map[string]*scoredCitation
}

type scoredCitation struct {
	citation    Citation
	key         string
	domainEdge  string
	priority    uint64
	queueOffset int
}

type citationPriorityQueue []*scoredCitation

func NewCitationSample() *CitationSample {
	return newCitationSample(maximumDomainCitations)
}

func newCitationSample(limit int) *CitationSample {
	limit = min(max(0, limit), maximumDomainCitations)

	return &CitationSample{
		limit:       limit,
		pages:       make(map[string]*scoredCitation, limit),
		domainEdges: make(map[string]map[string]*scoredCitation),
	}
}

func (s *CitationSample) Add(citations ...Citation) {
	if s == nil || s.limit <= 0 {
		return
	}
	for _, citation := range citations {
		candidate, valid := scoredCitationFor(citation)
		if !valid {
			continue
		}
		if retained := s.pages[candidate.key]; retained != nil {
			retained.citation.Confidence = max(
				retained.citation.Confidence,
				candidate.citation.Confidence,
			)
			continue
		}
		edgePages := s.domainEdges[candidate.domainEdge]
		if len(edgePages) >= maximumSourcePagesPerDomain {
			worst := worstCitation(edgePages)
			if !citationPrecedes(candidate, worst) {
				continue
			}
			s.remove(worst)
		}
		if len(s.citations) >= s.limit {
			if !citationPrecedes(candidate, s.citations[0]) {
				continue
			}
			s.remove(s.citations[0])
		}
		s.retain(candidate)
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

func (s *CitationSample) retain(candidate *scoredCitation) {
	heap.Push(&s.citations, candidate)
	s.pages[candidate.key] = candidate
	edgePages := s.domainEdges[candidate.domainEdge]
	if edgePages == nil {
		edgePages = make(map[string]*scoredCitation, maximumSourcePagesPerDomain)
		s.domainEdges[candidate.domainEdge] = edgePages
	}
	edgePages[candidate.key] = candidate
	s.retainedBytes += maximumCitationRetainedBytes
}

func (s *CitationSample) remove(candidate *scoredCitation) {
	heap.Remove(&s.citations, candidate.queueOffset)
	delete(s.pages, candidate.key)
	edgePages := s.domainEdges[candidate.domainEdge]
	delete(edgePages, candidate.key)
	if len(edgePages) == 0 {
		delete(s.domainEdges, candidate.domainEdge)
	}
	s.retainedBytes -= maximumCitationRetainedBytes
}

func scoredCitationFor(citation Citation) (*scoredCitation, bool) {
	if len(citation.SourceURL) > maximumCitationURLBytes ||
		len(citation.TargetURL) > maximumCitationURLBytes ||
		math.IsNaN(citation.Confidence) || math.IsInf(citation.Confidence, 0) ||
		citation.Confidence <= 0 {
		return nil, false
	}
	sourcePage := strings.TrimSpace(citation.SourceURL)
	sourceDomain := RegistrableDomain(sourcePage)
	targetDomain := RegistrableDomain(citation.TargetURL)
	if sourceDomain == "" || targetDomain == "" || sourceDomain == targetDomain ||
		len(sourceDomain) > maximumCitationDomainBytes ||
		len(targetDomain) > maximumCitationDomainBytes {
		return nil, false
	}
	targetHost := targetDomain
	if strings.Contains(targetHost, ":") {
		targetHost = "[" + targetHost + "]"
	}
	canonicalTargetURL := url.URL{Scheme: "https", Host: targetHost, Path: "/"}
	targetURL := canonicalTargetURL.String()
	key := sourcePage + "\x00" + targetURL
	sourceEnd := len(sourcePage)
	targetStart := sourceEnd + 1
	targetEnd := targetStart + len(targetURL)
	citation.SourceURL = key[:sourceEnd]
	citation.TargetURL = key[targetStart:targetEnd]
	citation.Confidence = min(1, citation.Confidence)
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(key))

	return &scoredCitation{
		citation: citation, key: key, domainEdge: sourceDomain + "\x00" + targetDomain,
		priority:    digest.Sum64(),
		queueOffset: -1,
	}, true
}

func worstCitation(citations map[string]*scoredCitation) *scoredCitation {
	var worst *scoredCitation
	for _, citation := range citations {
		if worst == nil || citationPrecedes(worst, citation) {
			worst = citation
		}
	}

	return worst
}

func citationPrecedes(left, right *scoredCitation) bool {
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
	q[left].queueOffset = left
	q[right].queueOffset = right
}

func (q *citationPriorityQueue) Push(value any) {
	citation := value.(*scoredCitation)
	citation.queueOffset = len(*q)
	*q = append(*q, citation)
}

func (q *citationPriorityQueue) Pop() any {
	old := *q
	last := len(old) - 1
	citation := old[last]
	old[last] = nil
	citation.queueOffset = -1
	*q = old[:last]

	return citation
}
