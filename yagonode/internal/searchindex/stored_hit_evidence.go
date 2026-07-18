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
	"github.com/D4rk4/yago/yagonode/internal/queryidentifier"
)

const analyzedTokenCacheEntries = 4096

type storedEvidenceTarget struct {
	requirement      int
	rawRequirement   int
	analyzerPosition int
	raw              string
	analyzed         string
	analyzedRunes    []rune
	anchor           []rune
	surfaceAnchor    []rune
	distance         int
	prefix           int
}

type storedEvidenceMatcher struct {
	analyzer                    analysis.Analyzer
	targets                     []storedEvidenceTarget
	required                    []string
	rawRequirements             []string
	rawRequirementOrdinals      []int
	lookup                      map[string][]int
	rawRequirementAnalyzedTerms map[string][]string
	fuzzy                       bool
	cache                       map[string]storedTokenEvidence
	queries                     int
	name                        string
	distance                    [3][maximumFuzzyTermRunes + 1]int
	minimumPassage              int
	relaxedPassageEvidence      bool
	normalizeCandidateAnchors   bool
	exactIdentifierRequirements []int
	quotedPhrases               storedPhraseTokenMatcher
}

type storedTokenEvidence struct {
	targets  []int
	analyzed analysis.TokenStream
}

type storedFieldEvidence struct {
	terms            search.TermLocationMap
	requirementTerms search.TermLocationMap
	targetTerms      map[int]search.Locations
	exactTerms       search.TermLocationMap
	phraseTerms      search.TermLocationMap
	latest           map[int]*search.Location
	latestRaw        map[int]*search.Location
	exactLatest      map[int]*search.Location
	witnesses        map[int]*search.Location
	bestSpan         int
	queryTerms       int
}

type storedFieldScan struct {
	matcher          *storedEvidenceMatcher
	evidence         *storedFieldEvidence
	includePositions bool
	position         uint64
	phrasePosition   uint64
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

type storedDocumentEvidence struct {
	locations              search.FieldTermLocationMap
	exactLocations         search.FieldTermLocationMap
	phraseLocations        search.FieldTermLocationMap
	bodyQueryMatches       []TextQueryMatch
	rawRequirements        []string
	rawRequirementOrdinals []int
	relaxedPassage         bool
	proximity              float64
	orderedProximity       float64
}

type storedDocumentField struct {
	name   string
	values []string
}

func searchResultFromStoredEvidence(
	ctx context.Context,
	hit *search.DocumentMatch,
	doc documentstore.Document,
	req SearchRequest,
) (SearchResult, error) {
	analyzerName := indexedAnalyzerName(hit, doc)
	evidence, err := storedDocumentLocations(
		ctx,
		doc,
		req,
		analyzerName,
	)
	if err != nil {
		return SearchResult{}, err
	}
	hit.Locations = evidence.locations
	result := searchResultFromDocument(hit, doc, req)
	if req.IncludePositions {
		result.FieldTermPositions = exactSurfaceFieldTermPositions(
			req,
			evidence.exactLocations,
		)
		result.Proximity = evidence.proximity
		result.OrderedProximity = evidence.orderedProximity
	}
	result.EvidenceReady = true
	result.EvidenceRequirementOrdinals = append(
		[]int{},
		evidence.rawRequirementOrdinals...,
	)
	result.BodyQueryMatches = evidence.bodyQueryMatches
	result.relaxedPassageEvidence = evidence.relaxedPassage
	result.quotedPhrasePreference = storedQuotedPhrasePreference(
		evidence.phraseLocations,
		req.Phrases,
		storedRequirementAnalyzer(analyzerName),
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
) (storedDocumentEvidence, error) {
	matcher := newStoredEvidenceMatcher(req, analyzerName)
	includePositions := req.IncludePositions || len(req.Phrases) > 0
	locations := search.FieldTermLocationMap{}
	exactLocations := search.FieldTermLocationMap{}
	phraseLocations := search.FieldTermLocationMap{}
	var bodyQueryMatches []TextQueryMatch
	proximity := 0.0
	orderedProximity := 0.0
	covered := make(map[string]struct{}, matcher.queries)
	fields := []storedDocumentField{
		{name: "title", values: []string{doc.Title}},
		{name: "headings", values: doc.Headings},
		{name: "anchors", values: anchorTexts(doc.Inlinks)},
		{name: "body", values: []string{doc.ExtractedText}},
	}
	for _, field := range fields {
		evidence, err := scanStoredFieldEvidence(ctx, matcher, field.values, includePositions)
		if err != nil {
			return storedDocumentEvidence{}, err
		}
		locations[field.name] = evidence.terms
		exactLocations[field.name] = evidence.exactTerms
		phraseLocations[field.name] = evidence.phraseTerms
		bodyQueryMatches = storedBodyMatches(
			bodyQueryMatches,
			field,
			evidence,
			doc.ExtractedText,
			analyzerName,
		)
		if req.IncludePositions {
			fieldProximity, fieldOrderedProximity := storedDocumentFieldProximity(
				field,
				matcher,
				evidence,
				req,
				analyzerName,
			)
			proximity = max(proximity, fieldProximity)
			orderedProximity = max(orderedProximity, fieldOrderedProximity)
		}
		recordStoredCoverage(covered, evidence.terms)
		if !includePositions && matcher.queries > 0 && len(covered) == matcher.queries &&
			(matcher.minimumPassage == 0 || matcher.relaxedPassageEvidence) {
			break
		}
	}

	return storedDocumentEvidence{
		locations:        locations,
		exactLocations:   exactLocations,
		phraseLocations:  phraseLocations,
		bodyQueryMatches: bodyQueryMatches,
		rawRequirements:  append([]string(nil), matcher.rawRequirements...),
		rawRequirementOrdinals: append(
			[]int(nil),
			matcher.rawRequirementOrdinals...,
		),
		relaxedPassage:   matcher.relaxedPassageEvidence,
		proximity:        proximity,
		orderedProximity: orderedProximity,
	}, nil
}

func storedBodyMatches(
	current []TextQueryMatch,
	field storedDocumentField,
	evidence storedFieldEvidence,
	body string,
	analyzerName string,
) []TextQueryMatch {
	if field.name != "body" {
		return current
	}
	matches := evidence.terms
	if isCJKAnalyzer(analyzerName) {
		matches = evidence.requirementTerms
	}

	return boundedBodyQueryMatches(body, matches)
}

func storedDocumentFieldProximity(
	field storedDocumentField,
	matcher *storedEvidenceMatcher,
	evidence storedFieldEvidence,
	req SearchRequest,
	analyzerName string,
) (float64, float64) {
	var proximity float64
	var ordered float64
	if isCJKAnalyzer(analyzerName) && analyzerName != "cjk" {
		proximity, ordered = storedCJKRequirementProximity(storedCJKProximityEvidence{
			values:         field.values,
			exact:          evidence.exactTerms,
			wordForms:      evidence.requirementTerms,
			requirements:   matcher.rawRequirements,
			ordinals:       matcher.rawRequirementOrdinals,
			allowWordForms: !req.Fuzzy,
		})
	} else {
		proximity, ordered = storedWordFormProximity(
			evidence.exactTerms,
			evidence.requirementTerms,
			matcher.rawRequirements,
			matcher.rawRequirementOrdinals,
			!req.Fuzzy,
		)
	}
	analyzerProximity, analyzerOrdered := storedSingleRequirementAnalyzerProximity(
		matcher,
		evidence.targetTerms,
		!req.Fuzzy,
	)

	return max(proximity, analyzerProximity), max(ordered, analyzerOrdered)
}

func recordStoredCoverage(
	covered map[string]struct{},
	terms search.TermLocationMap,
) {
	for term, locations := range terms {
		if len(locations) > 0 {
			covered[term] = struct{}{}
		}
	}
}

func newStoredEvidenceMatcher(req SearchRequest, analyzerName string) *storedEvidenceMatcher {
	query := make([]storedRawRequirement, 0, len(queryTermWords(req)))
	seen := map[string]struct{}{}
	for ordinal, term := range queryTermWords(req) {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if _, exists := seen[term]; exists {
			continue
		}
		seen[term] = struct{}{}
		query = append(query, storedRawRequirement{term: term, ordinal: ordinal})
	}
	matcher := &storedEvidenceMatcher{
		fuzzy:  req.Fuzzy,
		cache:  make(map[string]storedTokenEvidence, min(analyzedTokenCacheEntries, len(query)*64)),
		name:   analyzerName,
		lookup: make(map[string][]int),
	}
	indexMapping := loadStemmingMapping()
	if indexMapping == nil {
		for _, requirement := range query {
			matcher.addTargetAtOrdinal(
				requirement.term,
				requirement.term,
				requirement.ordinal,
				1,
			)
		}
		matcher.queries = len(matcher.required)
		matcher.minimumPassage = matcher.relaxedPassageMinimum(req)
		matcher.quotedPhrases = newStoredPhraseTokenMatcher(req.Phrases, nil)
		matcher.quotedPhrases.bindSearchTargets(matcher.lookup)

		return matcher
	}
	matcher.analyzer = storedEvidenceAnalyzer(analyzerName)
	requirementAnalyzer := storedRequirementAnalyzer(analyzerName)
	for _, requirement := range query {
		analyzed := requirementAnalyzer.Analyze([]byte(requirement.term))
		for _, token := range analyzed {
			matcher.addTargetAtOrdinal(
				requirement.term,
				string(token.Term),
				requirement.ordinal,
				token.Position,
			)
		}
	}
	if len(matcher.targets) == 0 && len(query) > 0 {
		matcher.analyzer = indexMapping.AnalyzerNamed(standardTextAnalyzer)
		requirementAnalyzer = matcher.analyzer
		matcher.name = standardTextAnalyzer
		for _, requirement := range query {
			for _, token := range matcher.analyzer.Analyze([]byte(requirement.term)) {
				matcher.addTargetAtOrdinal(
					requirement.term,
					string(token.Term),
					requirement.ordinal,
					token.Position,
				)
			}
		}
	}
	matcher.queries = len(matcher.required)
	matcher.minimumPassage = matcher.relaxedPassageMinimum(req)
	matcher.quotedPhrases = newStoredPhraseTokenMatcher(req.Phrases, requirementAnalyzer)
	matcher.quotedPhrases.bindSearchTargets(matcher.lookup)

	return matcher
}

func (m *storedEvidenceMatcher) addTarget(raw string, analyzed string) {
	m.addTargetAtOrdinal(
		raw,
		analyzed,
		len(m.rawRequirements),
		len(m.targets)+1,
	)
}

func (m *storedEvidenceMatcher) addTargetAtOrdinal(
	raw string,
	analyzed string,
	ordinal int,
	analyzerPosition int,
) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	analyzed = strings.ToLower(strings.TrimSpace(analyzed))
	if analyzed == "" {
		return
	}
	if m.rawRequirementAnalyzedTerms == nil {
		m.rawRequirementAnalyzedTerms = make(map[string][]string)
	}
	published := m.rawRequirementAnalyzedTerms[raw]
	registered := false
	for _, existing := range published {
		if existing == analyzed {
			registered = true
			break
		}
	}
	if !registered {
		m.rawRequirementAnalyzedTerms[raw] = append(published, analyzed)
	}
	rawRequirement := -1
	for index, requirement := range m.rawRequirements {
		if requirement == raw {
			rawRequirement = index
			break
		}
	}
	if rawRequirement < 0 {
		rawRequirement = len(m.rawRequirements)
		m.rawRequirements = append(m.rawRequirements, raw)
		m.rawRequirementOrdinals = append(m.rawRequirementOrdinals, ordinal)
		if queryidentifier.MixedAlphanumeric(raw) {
			m.exactIdentifierRequirements = append(
				m.exactIdentifierRequirements,
				rawRequirement,
			)
		}
	}
	requirement := -1
	for index, required := range m.required {
		if required == analyzed {
			requirement = index
			break
		}
	}
	if requirement < 0 {
		requirement = len(m.required)
		m.required = append(m.required, analyzed)
	}
	for _, targetIndex := range m.lookup[analyzed] {
		if m.targets[targetIndex].rawRequirement == rawRequirement {
			return
		}
	}
	m.targets = append(
		m.targets,
		newStoredEvidenceTargetAtPosition(
			requirement,
			rawRequirement,
			analyzerPosition,
			raw,
			analyzed,
		),
	)
	m.lookup[analyzed] = append(m.lookup[analyzed], len(m.targets)-1)
}

func newStoredEvidenceTarget(
	requirement int,
	rawRequirement int,
	raw string,
	analyzed string,
) storedEvidenceTarget {
	return newStoredEvidenceTargetAtPosition(
		requirement,
		rawRequirement,
		0,
		raw,
		analyzed,
	)
}

func newStoredEvidenceTargetAtPosition(
	requirement int,
	rawRequirement int,
	analyzerPosition int,
	raw string,
	analyzed string,
) storedEvidenceTarget {
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
	surfaceRunes := []rune(raw)
	surfaceAnchor := surfaceRunes[:min(anchorRunes, len(surfaceRunes))]
	analyzedAnchor := analyzedRunes[:anchorRunes]
	if string(surfaceAnchor) == string(analyzedAnchor) {
		surfaceAnchor = nil
	}

	return storedEvidenceTarget{
		requirement:      requirement,
		rawRequirement:   rawRequirement,
		analyzerPosition: analyzerPosition,
		raw:              raw,
		analyzed:         analyzed,
		analyzedRunes:    analyzedRunes,
		anchor:           append([]rune(nil), analyzedAnchor...),
		surfaceAnchor:    append([]rune(nil), surfaceAnchor...),
		distance:         distance,
		prefix:           prefix,
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

func scanStoredFieldEvidence(
	ctx context.Context,
	matcher *storedEvidenceMatcher,
	values []string,
	includePositions bool,
) (storedFieldEvidence, error) {
	if isCJKAnalyzer(matcher.name) {
		return scanStoredCJKFieldEvidence(ctx, matcher, values, includePositions)
	}
	field := newStoredFieldEvidence(matcher)
	scan := storedFieldScan{
		matcher:          matcher,
		evidence:         &field,
		includePositions: includePositions,
	}
	for arrayIndex, value := range values {
		field.latest = map[int]*search.Location{}
		field.latestRaw = map[int]*search.Location{}
		field.exactLatest = map[int]*search.Location{}
		scan.phrasePosition = 0
		if err := scan.scanValue(ctx, value, arrayIndex, len(values)); err != nil {
			return storedFieldEvidence{}, err
		}
		scan.position++
	}
	field.preserveWitnesses(matcher)

	return field, nil
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
		token := value[start:end]
		evidence := s.matcher.match(token)
		s.observePhraseToken(evidence.analyzed, start, end, arrayIndex, arrayLength)
		s.position++
		matched := evidence.targets
		if len(matched) == 0 {
			continue
		}
		location := newStoredLocation(storedLocationCoordinates{
			position:    s.position,
			start:       start,
			end:         end,
			arrayIndex:  arrayIndex,
			arrayLength: arrayLength,
		})
		s.evidence.addMatches(s.matcher, matched, location, token)
		s.evidence.observeWindow()
		s.matcher.observeRelaxedPassage(s.evidence.exactLatest)
		if !s.includePositions && len(s.evidence.latest) == s.evidence.queryTerms &&
			(s.matcher.minimumPassage == 0 || s.matcher.relaxedPassageEvidence) {
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
	evidence, err := scanStoredCJKFieldEvidence(ctx, matcher, values, includePositions)

	return evidence.terms, err
}

func scanStoredCJKFieldEvidence(
	ctx context.Context,
	matcher *storedEvidenceMatcher,
	values []string,
	includePositions bool,
) (storedFieldEvidence, error) {
	if matcher.name != "cjk" {
		return scanStoredDictionaryCJKFieldEvidence(
			ctx,
			matcher,
			values,
			includePositions,
		)
	}
	field := newStoredFieldEvidence(matcher)
	position := uint64(0)
	for arrayIndex, value := range values {
		field.latest = map[int]*search.Location{}
		field.latestRaw = map[int]*search.Location{}
		field.exactLatest = map[int]*search.Location{}
		cjkValue := storedCJKValue{
			matcher:     matcher,
			text:        value,
			arrayIndex:  arrayIndex,
			arrayLength: len(values),
		}
		for start, end := range rangeStoredTokens(value) {
			if err := ctx.Err(); err != nil {
				return storedFieldEvidence{}, fmt.Errorf("stored search evidence: %w", err)
			}
			token := value[start:end]
			if !containsStoredCJK(token) {
				position++
				field.addCJKMatches(
					matcher,
					normalizedUnstemmedWord(token, "cjk"),
					token,
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
			matcher.observeRelaxedPassage(field.exactLatest)
			if !includePositions && len(field.latest) == field.queryTerms &&
				(matcher.minimumPassage == 0 || matcher.relaxedPassageEvidence) {
				break
			}
		}
		position++
	}
	field.preserveWitnesses(matcher)
	if matcher.quotedPhrases.enabled() {
		field.phraseTerms = field.terms
	}

	return field, nil
}

func containsStoredCJK(text string) bool {
	for _, character := range text {
		if storedCJKCharacter(character) {
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
	literalStart := -1
	for offset, character := range value.text[start:end] {
		absoluteStart := start + offset
		absoluteEnd := absoluteStart + utf8.RuneLen(character)
		if !storedCJKCharacter(character) {
			if sequence == 1 {
				position = f.addCJKSurfaceTerm(
					value,
					previousStart,
					previousEnd,
					position,
				)
			}
			sequence = 0
			previousStart = -1
			if literalStart < 0 {
				literalStart = absoluteStart
			}
			continue
		}
		if literalStart >= 0 {
			position = f.addCJKSurfaceTerm(value, literalStart, absoluteStart, position)
			literalStart = -1
		}
		if sequence > 0 {
			position = f.addCJKSurfaceTerm(
				value,
				previousStart,
				absoluteEnd,
				position,
			)
		}
		sequence++
		previousStart = absoluteStart
		previousEnd = absoluteEnd
	}
	if literalStart >= 0 {
		return f.addCJKSurfaceTerm(value, literalStart, end, position)
	}
	if sequence == 1 {
		position = f.addCJKSurfaceTerm(
			value,
			previousStart,
			previousEnd,
			position,
		)
	}

	return position
}

func (f *storedFieldEvidence) addCJKMatches(
	matcher *storedEvidenceMatcher,
	term string,
	surface string,
	coordinates storedLocationCoordinates,
) {
	location := newStoredLocation(coordinates)
	f.addMatches(matcher, matcher.lookup[term], location, surface)
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
				unicode.IsMark(character) || storedApostropheContinuesWord(
				text,
				index,
				character,
				start,
			)
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

func storedApostropheContinuesWord(
	text string,
	index int,
	character rune,
	start int,
) bool {
	if start < 0 || (character != '\'' && character != '’' && character != '＇') {
		return false
	}
	_, width := utf8.DecodeRuneInString(text[index:])
	next, _ := utf8.DecodeRuneInString(text[index+width:])

	return unicode.IsLetter(next) || unicode.IsNumber(next) || unicode.IsMark(next)
}

func (m *storedEvidenceMatcher) match(token string) storedTokenEvidence {
	if cached, ok := m.cache[token]; ok {
		return cached
	}
	mightMatchTarget := m.mightMatch(token)
	if !mightMatchTarget && !m.quotedPhrases.independentlyMightMatch(token) {
		return storedTokenEvidence{}
	}
	analyzed := analysis.TokenStream{
		&analysis.Token{Term: []byte(strings.ToLower(token)), Position: 1},
	}
	if m.analyzer != nil {
		analyzed = m.analyzer.Analyze([]byte(token))
	}
	matched := make([]int, 0, 1)
	if mightMatchTarget {
		for index, target := range m.targets {
			for _, analyzedToken := range analyzed {
				if m.analyzedTermMatches(string(analyzedToken.Term), target) {
					matched = append(matched, index)
					break
				}
			}
		}
	}
	evidence := storedTokenEvidence{targets: matched}
	if m.quotedPhrases.enabled() {
		evidence.analyzed = analyzed
	}
	if len(m.cache) < analyzedTokenCacheEntries {
		m.cache[token] = evidence
	}

	return evidence
}

func (m *storedEvidenceMatcher) mightMatch(token string) bool {
	normalized := ""
	for _, target := range m.targets {
		if len(target.anchor) == 0 || containsFoldedRunes(token, target.anchor) ||
			(len(target.surfaceAnchor) > 0 && containsFoldedRunes(token, target.surfaceAnchor)) {
			return true
		}
		if !m.normalizeCandidateAnchors {
			continue
		}
		if normalized == "" {
			normalized = normalizedUnstemmedWord(token, m.name)
		}
		if containsFoldedRunes(normalized, target.anchor) ||
			(len(target.surfaceAnchor) > 0 &&
				containsFoldedRunes(normalized, target.surfaceAnchor)) {
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
	if !storedProximityEligible(results, req) {
		return
	}
	weights := req.Weights.orDefault()
	for index := range results {
		results[index].Score *= 1 +
			weights.OrderedProximity*results[index].OrderedProximity +
			weights.Proximity*results[index].Proximity
	}
	sort.SliceStable(results, func(left int, right int) bool {
		if results[left].Score != results[right].Score {
			return results[left].Score > results[right].Score
		}

		return results[left].DocumentID < results[right].DocumentID
	})
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
