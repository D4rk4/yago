package searchindex

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const analyzedTokenCacheEntries = 4096

const (
	storedOrderedProximityWeight   = 0.12
	storedUnorderedProximityWeight = 0.05
)

type storedEvidenceTarget struct {
	requirement   int
	raw           string
	analyzed      string
	analyzedRunes []rune
	anchor        []rune
	distance      int
	prefix        int
}

type storedEvidenceMatcher struct {
	analyzer analysis.Analyzer
	targets  []storedEvidenceTarget
	required []string
	lookup   map[string][]int
	fuzzy    bool
	cache    map[string][]int
	queries  int
	name     string
	distance [3][maximumFuzzyTermRunes + 1]int
}

type storedFieldEvidence struct {
	terms      search.TermLocationMap
	latest     map[int]*search.Location
	witnesses  map[int]*search.Location
	bestSpan   int
	queryTerms int
}

type storedFieldScan struct {
	matcher          *storedEvidenceMatcher
	evidence         *storedFieldEvidence
	includePositions bool
	position         uint64
}

type storedCJKValue struct {
	matcher     *storedEvidenceMatcher
	text        string
	arrayIndex  int
	arrayLength int
}

type storedLocationCoordinates struct {
	position    uint64
	start       int
	end         int
	arrayIndex  int
	arrayLength int
}

func searchResultFromStoredEvidence(
	ctx context.Context,
	hit *search.DocumentMatch,
	doc documentstore.Document,
	req SearchRequest,
) (SearchResult, error) {
	analyzerName := indexedAnalyzerName(hit, doc)
	locations, err := storedDocumentLocations(ctx, doc, req, analyzerName)
	if err != nil {
		return SearchResult{}, err
	}
	hit.Locations = locations
	result := searchResultFromDocument(hit, doc, req)
	result.EvidenceReady = true
	result.quotedPhrasePreference = storedQuotedPhrasePreference(
		locations,
		req.Phrases,
		storedEvidenceAnalyzer(analyzerName),
	)
	hit.Locations = nil

	return result, nil
}

func searchResultFromStoredDocument(
	ctx context.Context,
	hit *search.DocumentMatch,
	doc documentstore.Document,
	req SearchRequest,
) (SearchResult, error) {
	if req.CandidateOnly {
		return searchResultFromDocument(hit, doc, req), nil
	}

	return searchResultFromStoredEvidence(ctx, hit, doc, req)
}

func storedDocumentLocations(
	ctx context.Context,
	doc documentstore.Document,
	req SearchRequest,
	analyzerName string,
) (search.FieldTermLocationMap, error) {
	matcher := newStoredEvidenceMatcher(req, analyzerName)
	includePositions := req.IncludePositions || len(req.Phrases) > 0
	locations := search.FieldTermLocationMap{}
	covered := make(map[string]struct{}, matcher.queries)
	fields := []struct {
		name   string
		values []string
	}{
		{name: "title", values: []string{doc.Title}},
		{name: "headings", values: doc.Headings},
		{name: "anchors", values: anchorTexts(doc.Inlinks)},
		{name: "body", values: []string{doc.ExtractedText}},
	}
	for _, field := range fields {
		terms, err := scanStoredField(ctx, matcher, field.values, includePositions)
		if err != nil {
			return nil, err
		}
		if len(terms) > 0 {
			locations[field.name] = terms
			for term := range terms {
				covered[term] = struct{}{}
			}
		}
		if !includePositions && matcher.queries > 0 && len(covered) == matcher.queries {
			break
		}
	}

	return locations, nil
}

func newStoredEvidenceMatcher(req SearchRequest, analyzerName string) *storedEvidenceMatcher {
	query := make([]string, 0, len(queryTermWords(req)))
	seen := map[string]struct{}{}
	for _, term := range queryTermWords(req) {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if _, exists := seen[term]; exists {
			continue
		}
		seen[term] = struct{}{}
		query = append(query, term)
	}
	matcher := &storedEvidenceMatcher{
		fuzzy:  req.Fuzzy,
		cache:  make(map[string][]int, min(analyzedTokenCacheEntries, len(query)*64)),
		name:   analyzerName,
		lookup: make(map[string][]int),
	}
	indexMapping := loadStemmingMapping()
	if indexMapping == nil {
		for _, term := range query {
			matcher.addTarget(term, term)
		}
		matcher.queries = len(matcher.required)

		return matcher
	}
	matcher.analyzer = storedEvidenceAnalyzer(analyzerName)
	for _, term := range query {
		analyzed := matcher.analyzer.Analyze([]byte(term))
		for _, token := range analyzed {
			matcher.addTarget(term, string(token.Term))
		}
	}
	if len(matcher.targets) == 0 && len(query) > 0 {
		matcher.analyzer = indexMapping.AnalyzerNamed(standardTextAnalyzer)
		matcher.name = standardTextAnalyzer
		for _, term := range query {
			for _, token := range matcher.analyzer.Analyze([]byte(term)) {
				matcher.addTarget(term, string(token.Term))
			}
		}
	}
	matcher.queries = len(matcher.required)

	return matcher
}

func (m *storedEvidenceMatcher) addTarget(raw string, analyzed string) {
	if analyzed == "" {
		return
	}
	if _, exists := m.lookup[analyzed]; exists {
		return
	}
	requirement := len(m.required)
	m.required = append(m.required, analyzed)
	m.targets = append(
		m.targets,
		newStoredEvidenceTarget(requirement, raw, analyzed),
	)
	m.lookup[analyzed] = append(m.lookup[analyzed], len(m.targets)-1)
}

func newStoredEvidenceTarget(requirement int, raw string, analyzed string) storedEvidenceTarget {
	distance := fuzzyEditDistance(raw)
	prefix := fuzzyPrefixLength(raw)
	analyzedRunes := []rune(analyzed)
	anchorRunes := 3
	if distance > 0 {
		anchorRunes = 0
		if prefix > 0 {
			anchorRunes = utf8.RuneCountInString(raw[:min(prefix, len(raw))])
		}
	}
	anchorRunes = min(anchorRunes, len(analyzedRunes))

	return storedEvidenceTarget{
		requirement:   requirement,
		raw:           raw,
		analyzed:      analyzed,
		analyzedRunes: analyzedRunes,
		anchor:        append([]rune(nil), analyzedRunes[:anchorRunes]...),
		distance:      distance,
		prefix:        prefix,
	}
}

func indexedAnalyzerName(hit *search.DocumentMatch, doc documentstore.Document) string {
	if hit != nil {
		if value, ok := hit.Fields[documentAnalyzerField].(string); ok && value != "" {
			return value
		}
	}

	return detectDocumentAnalyzer(analyzerDetectionText(doc), doc.Language)
}

func scanStoredField(
	ctx context.Context,
	matcher *storedEvidenceMatcher,
	values []string,
	includePositions bool,
) (search.TermLocationMap, error) {
	if matcher.name == "cjk" {
		return scanStoredCJKField(ctx, matcher, values, includePositions)
	}
	field := storedFieldEvidence{
		terms:      search.TermLocationMap{},
		bestSpan:   int(^uint(0) >> 1),
		queryTerms: matcher.queries,
	}
	scan := storedFieldScan{
		matcher:          matcher,
		evidence:         &field,
		includePositions: includePositions,
	}
	for arrayIndex, value := range values {
		field.latest = map[int]*search.Location{}
		if err := scan.scanValue(ctx, value, arrayIndex, len(values)); err != nil {
			return nil, err
		}
		scan.position++
	}
	field.preserveWitnesses(matcher)

	return field.terms, nil
}

func (s *storedFieldScan) scanValue(
	ctx context.Context,
	value string,
	arrayIndex int,
	arrayLength int,
) error {
	for start, end := range rangeStoredTokens(value) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stored search evidence: %w", err)
		}
		s.position++
		matched := s.matcher.match(value[start:end])
		if len(matched) == 0 {
			continue
		}
		for _, targetIndex := range matched {
			target := s.matcher.targets[targetIndex]
			location := newStoredLocation(storedLocationCoordinates{
				position:    s.position,
				start:       start,
				end:         end,
				arrayIndex:  arrayIndex,
				arrayLength: arrayLength,
			})
			s.evidence.add(target, location)
		}
		s.evidence.observeWindow()
		if !s.includePositions && len(s.evidence.latest) == s.evidence.queryTerms {
			break
		}
	}

	return nil
}

func scanStoredCJKField(
	ctx context.Context,
	matcher *storedEvidenceMatcher,
	values []string,
	includePositions bool,
) (search.TermLocationMap, error) {
	field := storedFieldEvidence{
		terms:      search.TermLocationMap{},
		bestSpan:   int(^uint(0) >> 1),
		queryTerms: matcher.queries,
	}
	position := uint64(0)
	for arrayIndex, value := range values {
		field.latest = map[int]*search.Location{}
		cjkValue := storedCJKValue{
			matcher:     matcher,
			text:        value,
			arrayIndex:  arrayIndex,
			arrayLength: len(values),
		}
		for start, end := range rangeStoredTokens(value) {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("stored search evidence: %w", err)
			}
			token := value[start:end]
			if !containsStoredCJK(token) {
				position++
				field.addCJKMatches(
					matcher,
					strings.ToLower(token),
					storedLocationCoordinates{
						position:    position,
						start:       start,
						end:         end,
						arrayIndex:  arrayIndex,
						arrayLength: len(values),
					},
				)
			} else {
				position = field.addCJKSequence(
					cjkValue,
					start,
					end,
					position,
				)
			}
			field.observeWindow()
			if !includePositions && len(field.latest) == field.queryTerms {
				break
			}
		}
		position++
	}
	field.preserveWitnesses(matcher)

	return field.terms, nil
}

func containsStoredCJK(text string) bool {
	for _, character := range text {
		if unicode.In(
			character,
			unicode.Han,
			unicode.Hiragana,
			unicode.Katakana,
			unicode.Hangul,
		) {
			return true
		}
	}

	return false
}

func (f *storedFieldEvidence) addCJKSequence(
	value storedCJKValue,
	start int,
	end int,
	position uint64,
) uint64 {
	previousStart := -1
	previousEnd := -1
	sequence := 0
	for offset, character := range value.text[start:end] {
		absoluteStart := start + offset
		absoluteEnd := absoluteStart + utf8.RuneLen(character)
		if !unicode.In(
			character,
			unicode.Han,
			unicode.Hiragana,
			unicode.Katakana,
			unicode.Hangul,
		) {
			if sequence == 1 {
				position++
				f.addCJKMatches(
					value.matcher,
					value.text[previousStart:previousEnd],
					storedLocationCoordinates{
						position:    position,
						start:       previousStart,
						end:         previousEnd,
						arrayIndex:  value.arrayIndex,
						arrayLength: value.arrayLength,
					},
				)
			}
			sequence = 0
			previousStart = -1
			continue
		}
		if sequence > 0 {
			position++
			f.addCJKMatches(
				value.matcher,
				value.text[previousStart:absoluteEnd],
				storedLocationCoordinates{
					position:    position,
					start:       previousStart,
					end:         absoluteEnd,
					arrayIndex:  value.arrayIndex,
					arrayLength: value.arrayLength,
				},
			)
		}
		sequence++
		previousStart = absoluteStart
		previousEnd = absoluteEnd
	}
	if sequence == 1 {
		position++
		f.addCJKMatches(
			value.matcher,
			value.text[previousStart:previousEnd],
			storedLocationCoordinates{
				position:    position,
				start:       previousStart,
				end:         previousEnd,
				arrayIndex:  value.arrayIndex,
				arrayLength: value.arrayLength,
			},
		)
	}

	return position
}

func (f *storedFieldEvidence) addCJKMatches(
	matcher *storedEvidenceMatcher,
	term string,
	coordinates storedLocationCoordinates,
) {
	for _, targetIndex := range matcher.lookup[term] {
		location := newStoredLocation(coordinates)
		f.add(matcher.targets[targetIndex], location)
	}
}

func newStoredLocation(coordinates storedLocationCoordinates) *search.Location {
	location := &search.Location{
		Pos:   coordinates.position,
		Start: storedLocationCoordinate(coordinates.start),
		End:   storedLocationCoordinate(coordinates.end),
	}
	if coordinates.arrayLength > 1 {
		location.ArrayPositions = search.ArrayPositions{
			storedLocationCoordinate(coordinates.arrayIndex),
		}
	}

	return location
}

func storedLocationCoordinate(coordinate int) uint64 {
	if coordinate < 0 {
		return 0
	}

	return uint64(coordinate)
}

func rangeStoredTokens(text string) func(func(int, int) bool) {
	return func(yield func(int, int) bool) {
		start := -1
		for index, character := range text {
			wordCharacter := unicode.IsLetter(character) || unicode.IsNumber(character) ||
				unicode.IsMark(character)
			if wordCharacter {
				if start < 0 {
					start = index
				}
				continue
			}
			if start >= 0 && !yield(start, index) {
				return
			}
			start = -1
		}
		if start >= 0 {
			yield(start, len(text))
		}
	}
}

func (m *storedEvidenceMatcher) match(token string) []int {
	if cached, ok := m.cache[token]; ok {
		return cached
	}
	if !m.mightMatch(token) {
		return nil
	}
	analyzedTerms := []string{strings.ToLower(token)}
	if m.analyzer != nil {
		analyzedTerms = analyzedTerms[:0]
		for _, analyzed := range m.analyzer.Analyze([]byte(token)) {
			analyzedTerms = append(analyzedTerms, string(analyzed.Term))
		}
	}
	matched := make([]int, 0, 1)
	for index, target := range m.targets {
		for _, analyzed := range analyzedTerms {
			if m.analyzedTermMatches(analyzed, target) {
				matched = append(matched, index)
				break
			}
		}
	}
	if len(m.cache) < analyzedTokenCacheEntries {
		m.cache[token] = matched
	}

	return matched
}

func (m *storedEvidenceMatcher) mightMatch(token string) bool {
	for _, target := range m.targets {
		if len(target.anchor) == 0 || containsFoldedRunes(token, target.anchor) {
			return true
		}
	}

	return false
}

func containsFoldedRunes(text string, sought []rune) bool {
	if len(sought) == 0 {
		return true
	}
	var window [4]rune
	count := 0
	for _, character := range text {
		character = unicode.ToLower(character)
		if count < len(sought) {
			window[count] = character
			count++
		} else {
			copy(window[:], window[1:len(sought)])
			window[len(sought)-1] = character
		}
		if count < len(sought) {
			continue
		}
		matched := true
		for index, expected := range sought {
			if window[index] != expected {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}

	return false
}

func (m *storedEvidenceMatcher) analyzedTermMatches(
	candidate string,
	target storedEvidenceTarget,
) bool {
	if candidate == target.analyzed {
		return true
	}
	if !m.fuzzy || target.distance == 0 ||
		runeLengthDifference(candidate, target.analyzed) >
			target.distance {
		return false
	}
	prefix := min(target.prefix, len(target.analyzed))
	if len(candidate) < prefix || candidate[:prefix] != target.analyzed[:prefix] {
		return false
	}

	return m.storedEvidenceWithinDistance(candidate, target)
}

func runeLengthDifference(left string, right string) int {
	difference := utf8.RuneCountInString(left) - utf8.RuneCountInString(right)
	if difference < 0 {
		return -difference
	}

	return difference
}

func (m *storedEvidenceMatcher) storedEvidenceWithinDistance(
	candidate string,
	target storedEvidenceTarget,
) bool {
	var candidateRunes [maximumFuzzyTermRunes]rune
	candidateLength := 0
	for _, character := range candidate {
		if candidateLength == len(candidateRunes) {
			return false
		}
		candidateRunes[candidateLength] = character
		candidateLength++
	}
	if len(target.analyzedRunes) > maximumFuzzyTermRunes {
		return false
	}
	limit := target.distance
	beforePrevious := m.distance[0][:len(target.analyzedRunes)+1]
	previous := m.distance[1][:len(target.analyzedRunes)+1]
	current := m.distance[2][:len(target.analyzedRunes)+1]
	for index := range previous {
		previous[index] = index
		beforePrevious[index] = limit + 1
	}
	previousCandidateRune := rune(0)
	for left := 1; left <= candidateLength; left++ {
		candidateRune := candidateRunes[left-1]
		for index := range current {
			current[index] = limit + 1
		}
		current[0] = left
		first := max(1, left-limit)
		last := min(len(target.analyzedRunes), left+limit)
		rowMinimum := limit + 1
		for right := first; right <= last; right++ {
			cost := 0
			if candidateRune != target.analyzedRunes[right-1] {
				cost = 1
			}
			current[right] = min(
				current[right-1]+1,
				previous[right]+1,
				previous[right-1]+cost,
			)
			if left > 1 && right > 1 &&
				candidateRune == target.analyzedRunes[right-2] &&
				previousCandidateRune == target.analyzedRunes[right-1] {
				current[right] = min(current[right], beforePrevious[right-2]+1)
			}
			rowMinimum = min(rowMinimum, current[right])
		}
		if rowMinimum > limit {
			return false
		}
		previousCandidateRune = candidateRune
		beforePrevious, previous, current = previous, current, beforePrevious
	}

	return previous[len(target.analyzedRunes)] <= limit
}

func hitUnorderedProximity(hit *search.DocumentMatch, terms []string) float64 {
	words := distinctWords(terms)
	if len(words) < 2 {
		return 0
	}
	positions := hitBodyPositions(hit, words)
	satisfied := 0
	for index := 0; index+1 < len(words); index++ {
		if withinWindow(
			positions[words[index]],
			positions[words[index+1]],
			sdmUnorderedWindow,
		) {
			satisfied++
		}
	}

	return float64(satisfied) / float64(len(words)-1)
}

func hitOrderedProximity(hit *search.DocumentMatch, terms []string) float64 {
	words := distinctWords(terms)
	if len(words) < 2 {
		return 0
	}
	positions := hitBodyPositions(hit, words)
	satisfied := 0
	for index := 0; index+1 < len(words); index++ {
		if orderedAdjacent(positions[words[index]], positions[words[index+1]]) {
			satisfied++
		}
	}

	return float64(satisfied) / float64(len(words)-1)
}

func hitBodyPositions(hit *search.DocumentMatch, terms []string) map[string][]int {
	positions := make(map[string][]int, len(terms))
	if hit == nil {
		return positions
	}
	for _, term := range terms {
		for _, location := range hit.Locations["body"][term] {
			positions[term] = append(
				positions[term],
				int(location.Pos), //nolint:gosec // bounded stored-document position
			)
		}
		sort.Ints(positions[term])
	}

	return positions
}

func rescoreStoredProximity(results []SearchResult, req SearchRequest) {
	if !req.IncludePositions || len(distinctWords(queryTermWords(req))) < 2 {
		return
	}
	for index := range results {
		results[index].Score *= 1 +
			storedOrderedProximityWeight*results[index].OrderedProximity +
			storedUnorderedProximityWeight*results[index].Proximity
	}
	sort.SliceStable(results, func(left int, right int) bool {
		if results[left].Score != results[right].Score {
			return results[left].Score > results[right].Score
		}

		return results[left].DocumentID < results[right].DocumentID
	})
}

func (f *storedFieldEvidence) add(
	target storedEvidenceTarget,
	location *search.Location,
) {
	locations := f.terms[target.analyzed]
	if len(locations) < maximumTermPositionsPerField {
		locations = append(locations, location)
	} else {
		locations[len(locations)-1] = location
	}
	f.terms[target.analyzed] = locations
	f.latest[target.requirement] = location
}

func (f *storedFieldEvidence) observeWindow() {
	if len(f.latest) != f.queryTerms || f.queryTerms == 0 {
		return
	}
	minimum := ^uint64(0)
	maximum := uint64(0)
	for _, location := range f.latest {
		minimum = min(minimum, location.Pos)
		maximum = max(maximum, location.Pos)
	}
	span := int(maximum - minimum)
	if span >= f.bestSpan {
		return
	}
	f.bestSpan = span
	f.witnesses = make(map[int]*search.Location, len(f.latest))
	for index, location := range f.latest {
		copy := *location
		copy.ArrayPositions = append(search.ArrayPositions(nil), location.ArrayPositions...)
		f.witnesses[index] = &copy
	}
}

func (f *storedFieldEvidence) preserveWitnesses(matcher *storedEvidenceMatcher) {
	for requirement, witness := range f.witnesses {
		term := matcher.required[requirement]
		locations := f.terms[term]
		found := false
		for _, location := range locations {
			if location.Pos == witness.Pos &&
				location.ArrayPositions.Equals(witness.ArrayPositions) {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if len(locations) < maximumTermPositionsPerField {
			locations = append(locations, witness)
		} else {
			locations[len(locations)/2] = witness
		}
		f.terms[term] = locations
	}
}
