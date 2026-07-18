package searchsession

import (
	"container/list"
	"reflect"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	retainedMapBytes      = 256
	retainedMapEntryBytes = 64
)

var (
	retainedMaximumInt         = int(^uint(0) >> 1)
	retainedSessionWidth       = reflect.TypeOf(session{}).Size()
	retainedListElementWidth   = reflect.TypeOf(list.Element{}).Size()
	retainedResultWidth        = reflect.TypeOf(searchcore.Result{}).Size()
	retainedFailureWidth       = reflect.TypeOf(searchcore.PartialFailure{}).Size()
	retainedFacetGroupWidth    = reflect.TypeOf(searchcore.FacetGroup{}).Size()
	retainedFacetTermWidth     = reflect.TypeOf(searchcore.FacetTerm{}).Size()
	retainedResultImageWidth   = reflect.TypeOf(searchcore.ResultImage{}).Size()
	retainedQueryMatchWidth    = reflect.TypeOf(searchcore.QueryMatch{}).Size()
	retainedPositionValueWidth = reflect.TypeOf(int(0)).Size()
)

func (s *stableSearcher) purgeExpiredLocked(now time.Time) {
	for element := s.order.Back(); element != nil; {
		previous := element.Prev()
		entry := element.Value.(*session)
		if now.After(entry.expires) {
			s.removeLocked(entry)
		}
		element = previous
	}
}

func (s *stableSearcher) enforceRetentionLocked() {
	for len(s.sessions) > maxSessions || s.retained > s.limit {
		s.removeLocked(s.order.Back().Value.(*session))
	}
}

func (s *stableSearcher) refreshRetention(entry *session, retained int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, found := s.sessions[entry.key]
	if !found || current != entry {
		return
	}
	s.retained -= entry.retained
	entry.retained = retained
	s.retained += entry.retained
	s.enforceRetentionLocked()
}

func retainedSessionBytes(entry *session) int {
	retained := retainedProduct(1, retainedSessionWidth)
	retained = retainedAdd(retained, retainedProduct(1, retainedListElementWidth))
	retained = retainedStrings(retained, entry.key, entry.recovered, entry.didYouMean)
	retained = retainedAdd(
		retained,
		retainedProduct(cap(entry.results), retainedResultWidth),
	)
	for _, result := range entry.results {
		retained = retainedResultBytes(retained, result)
	}
	retained = retainedAdd(
		retained,
		retainedProduct(cap(entry.failures), retainedFailureWidth),
	)
	for _, failure := range entry.failures {
		retained = retainedStrings(retained, failure.Source, failure.Reason)
	}
	retained = retainedAdd(
		retained,
		retainedProduct(cap(entry.facets), retainedFacetGroupWidth),
	)
	for _, facet := range entry.facets {
		retained = retainedStrings(retained, facet.Name)
		retained = retainedAdd(
			retained,
			retainedProduct(cap(facet.Terms), retainedFacetTermWidth),
		)
		for _, term := range facet.Terms {
			retained = retainedStrings(retained, term.Term)
		}
	}

	return retained
}

func retainedResultBytes(retained int, result searchcore.Result) int {
	retained = retainedStrings(
		retained,
		result.DocumentID,
		result.Analyzer,
		result.Title,
		result.URL,
		result.ClusterID,
		result.RepresentativeURL,
		result.DisplayURL,
		result.Snippet,
		string(result.Source),
		result.Host,
		result.Path,
		result.File,
		result.ContentType,
		result.URLHash,
		result.Date,
		string(result.ContentDomain),
		result.Language,
		result.Author,
		result.Keywords,
		result.Publisher,
		result.Explanation,
	)
	retained = retainedAdd(
		retained,
		retainedProduct(cap(result.Images), retainedResultImageWidth),
	)
	for _, image := range result.Images {
		retained = retainedStrings(retained, image.URL, image.Alt)
	}
	retained = retainedAdd(
		retained,
		retainedProduct(cap(result.QueryMatches), retainedQueryMatchWidth),
	)
	retained = retainedAdd(
		retained,
		retainedProduct(cap(result.BodyQueryMatches), retainedQueryMatchWidth),
	)
	retained = retainedStringScores(retained, result.FieldScores)
	retained = retainedTermPositions(retained, result.FieldTermPositions)

	return retained
}

func retainedStringScores(retained int, values map[string]float64) int {
	if values == nil {
		return retained
	}
	retained = retainedAdd(retained, retainedMapBytes)
	for key := range values {
		retained = retainedAdd(retained, retainedMapEntryBytes)
		retained = retainedStrings(retained, key)
	}

	return retained
}

func retainedTermPositions(
	retained int,
	fields map[string]map[string][]int,
) int {
	if fields == nil {
		return retained
	}
	retained = retainedAdd(retained, retainedMapBytes)
	for field, terms := range fields {
		retained = retainedAdd(retained, retainedMapEntryBytes)
		retained = retainedStrings(retained, field)
		retained = retainedAdd(retained, retainedMapBytes)
		for term, positions := range terms {
			retained = retainedAdd(retained, retainedMapEntryBytes)
			retained = retainedStrings(retained, term)
			retained = retainedAdd(
				retained,
				retainedProduct(cap(positions), retainedPositionValueWidth),
			)
		}
	}

	return retained
}

func retainedStrings(retained int, values ...string) int {
	for _, value := range values {
		retained = retainedAdd(retained, len(value))
	}

	return retained
}

func retainedProduct(length int, width uintptr) int {
	if width != 0 && uintptr(length) > uintptr(retainedMaximumInt)/width {
		return retainedMaximumInt
	}

	return length * int(width)
}

func retainedAdd(retained int, added int) int {
	if added > retainedMaximumInt-retained {
		return retainedMaximumInt
	}

	return retained + added
}
