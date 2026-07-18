package searchindex

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"math"
	"testing"
	"unicode"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis"
	bleveunicode "github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/registry"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/vellum"
)

type fixedCJKTestAnalyzer struct {
	tokens analysis.TokenStream
}

func (a fixedCJKTestAnalyzer) Analyze([]byte) analysis.TokenStream {
	return a.tokens
}

func TestCJKTokenizerConfigurationValidationAndFallbacks(t *testing.T) {
	tests := []map[string]any{
		{},
		{"language": "zh"},
		{"language": "xx", "dictionary": false},
	}
	for _, config := range tests {
		if tokenizer, err := newCJKDictionaryTokenizer(
			config,
			&registry.Cache{},
		); err == nil ||
			tokenizer != nil {
			t.Fatalf("configuration %#v returned tokenizer=%#v err=%v", config, tokenizer, err)
		}
	}
	for _, language := range []string{"zh", "ja", "ko"} {
		tokenizer, err := newCJKDictionaryTokenizer(
			map[string]any{"language": language, "dictionary": true},
			&registry.Cache{},
		)
		if err != nil || len(tokenizer.Tokenize([]byte("検索"))) == 0 {
			t.Fatalf("language %s tokenizer=%#v err=%v", language, tokenizer, err)
		}
	}

	originalChinese := loadCJKChineseDictionary
	sentinel := errors.New("dictionary unavailable")
	loadCJKChineseDictionary = func() (*cjkChineseDictionary, error) {
		return nil, sentinel
	}
	t.Cleanup(func() { loadCJKChineseDictionary = originalChinese })
	configuration, found := cjkTokenizerConfiguration("zh", true)
	if !found {
		t.Fatal("Chinese configuration unavailable")
	}
	if _, err := configuration.dictionary(); !errors.Is(err, sentinel) {
		t.Fatalf("dictionary error = %v", err)
	}
	units := []cjkTextUnit{{term: "尋", start: 0, end: 3}}
	if normalized := canonicalChineseUnits(units); normalized[0].term != "尋" {
		t.Fatalf("failed conversion changed units = %#v", normalized)
	}
	tokenizer := &cjkDictionaryTokenizerInstance{
		base:           bleveunicode.NewUnicodeTokenizer(),
		canonicalize:   true,
		dictionary:     configuration.dictionary,
		widthNormalize: cjkUnstemmedNormalizer,
	}
	if len(tokenizer.Tokenize([]byte("搜尋"))) == 0 {
		t.Fatal("dictionary failure removed recall tokens")
	}
}

func TestCJKTokenizerSequenceAndWidthBoundaries(t *testing.T) {
	tokenizer := &cjkDictionaryTokenizerInstance{splitHangul: true}
	if tokenizer.sequenceToken(&analysis.Token{Start: 0, End: 3}, []byte("abc")) {
		t.Fatal("Latin token treated as Hangul")
	}
	if tokenizer.sequenceToken(&analysis.Token{}, nil) {
		t.Fatal("empty token treated as Hangul")
	}
	tokenizer.widthNormalize = cjkUnstemmedNormalizer
	units := tokenizer.textUnits([]byte("ｶﾞ"), 0, len("ｶﾞ"))
	if len(units) != 1 || units[0].term != "ガ" || units[0].start != 0 || units[0].end != len("ｶﾞ") {
		t.Fatalf("halfwidth voiced unit = %#v", units)
	}
}

func TestCJKConversionRejectsInvalidOutputRanges(t *testing.T) {
	dictionary := &cjkChineseDictionary{conversionOutput: []byte("ok")}
	for _, value := range []uint64{0, 3, math.MaxUint64} {
		if _, _, found := cjkConversionOutput(dictionary, 1, value, 0); found {
			t.Fatalf("invalid value %d returned output", value)
		}
	}
}

func TestCJKDictionaryAssetErrorBoundaries(t *testing.T) {
	validFST := encodedCJKTestAsset(cjkTestFST(t, map[string]uint64{"word": 1}))
	validOutput := encodedCJKTestAsset([]byte("output"))
	invalidFST := encodedCJKTestAsset([]byte("not an FST"))
	if _, err := loadCJKFST("%"); err == nil {
		t.Fatal("invalid base64 accepted")
	}
	if _, err := decodeCJKDictionaryAsset(
		base64.StdEncoding.EncodeToString([]byte("plain")),
	); err == nil {
		t.Fatal("plain bytes accepted as gzip")
	}
	corrupt := encodedCJKTestAsset([]byte("checksum"))
	compressed, err := base64.StdEncoding.DecodeString(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	compressed[len(compressed)-1] ^= 0xff
	if _, err := decodeCJKDictionaryAsset(
		base64.StdEncoding.EncodeToString(compressed),
	); err == nil {
		t.Fatal("corrupt gzip accepted")
	}
	if _, err := loadCJKFST(invalidFST); err == nil {
		t.Fatal("invalid FST accepted")
	}
	if _, err := loadCJKChineseDictionaryAssets("%", validFST, validOutput); err == nil {
		t.Fatal("invalid Chinese lexicon accepted")
	}
	if _, err := loadCJKChineseDictionaryAssets(validFST, invalidFST, validOutput); err == nil {
		t.Fatal("invalid Chinese conversion accepted")
	}
	if _, err := loadCJKChineseDictionaryAssets(validFST, validFST, "%"); err == nil {
		t.Fatal("invalid Chinese output accepted")
	}
	if _, err := loadCJKJapaneseDictionaryAsset(invalidFST); err == nil {
		t.Fatal("invalid Japanese lexicon accepted")
	}
}

func TestCJKAnalyzerRegistrationAndMappingBoundaries(t *testing.T) {
	originalRegistrationError := errCJKTokenizerRegistration
	sentinel := errors.New("tokenizer type unavailable")
	errCJKTokenizerRegistration = sentinel
	if err := registerCJKDictionaryAnalyzers(bleve.NewIndexMapping()); !errors.Is(err, sentinel) {
		t.Fatalf("registration error = %v", err)
	}
	errCJKTokenizerRegistration = originalRegistrationError
	t.Cleanup(func() { errCJKTokenizerRegistration = originalRegistrationError })

	duplicateTokenizer := bleve.NewIndexMapping()
	if err := duplicateTokenizer.AddCustomTokenizer(cjkChineseComponent, map[string]any{
		"type": cjkDictionaryComponent, "language": "zh", "dictionary": true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := registerCJKDictionaryAnalyzers(duplicateTokenizer); err == nil {
		t.Fatal("duplicate tokenizer accepted")
	}

	duplicateAnalyzer := bleve.NewIndexMapping()
	if err := registerUnicodeNormalizer(duplicateAnalyzer); err != nil {
		t.Fatal(err)
	}
	if err := duplicateAnalyzer.AddCustomAnalyzer(cjkChineseTextAnalyzer, map[string]any{
		"type": "custom", "tokenizer": "unicode", "token_filters": []string{lowercaseFilter},
	}); err != nil {
		t.Fatal(err)
	}
	if err := registerCJKDictionaryAnalyzers(duplicateAnalyzer); err == nil {
		t.Fatal("duplicate analyzer accepted")
	}

	if supportsCJKDictionaryAnalyzers(nil) ||
		supportsCJKDictionaryAnalyzers(bleve.NewIndexMapping()) {
		t.Fatal("legacy mapping supports CJK dictionary analyzers")
	}
	current, err := newSearchIndexMapping()
	if err != nil || !supportsCJKDictionaryAnalyzers(current) {
		t.Fatalf(
			"current mapping supported=%v err=%v",
			supportsCJKDictionaryAnalyzers(current),
			err,
		)
	}
	currentIndex, err := newBleveMemory(current)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := currentIndex.Close(); err != nil {
			t.Errorf("close current index: %v", err)
		}
	})
	legacyIndex, err := newBleveMemory(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := legacyIndex.Close(); err != nil {
			t.Errorf("close legacy index: %v", err)
		}
	})
	if !shardMappingIsCurrent(currentIndex) || shardMappingIsCurrent(legacyIndex) {
		t.Fatalf(
			"current=%v legacy=%v",
			shardMappingIsCurrent(currentIndex),
			shardMappingIsCurrent(legacyIndex),
		)
	}
	originalRegistration := registerCJKDictionaryAnalyzers
	registerCJKDictionaryAnalyzers = func(*mapping.IndexMappingImpl) error { return sentinel }
	t.Cleanup(func() { registerCJKDictionaryAnalyzers = originalRegistration })
	if _, err := newSearchIndexMapping(); !errors.Is(err, sentinel) {
		t.Fatalf("mapping registration error = %v", err)
	}
}

func TestCJKDictionaryQueryBoundaries(t *testing.T) {
	if cjkDictionaryQueryTerm(standardTextAnalyzer, "搜索引擎") ||
		cjkDictionaryQueryTerm("cjk", "搜索引擎") ||
		!cjkDictionaryQueryTerm(cjkChineseTextAnalyzer, "搜索引擎") ||
		cjkDictionaryQueryTerm(cjkChineseTextAnalyzer, "未知") {
		t.Fatal("dictionary query classification mismatch")
	}
	original := loadStemmingMapping
	loadStemmingMapping = func() *mapping.IndexMappingImpl { return nil }
	t.Cleanup(func() { loadStemmingMapping = original })
	if cjkDictionaryQueryTerm(cjkChineseTextAnalyzer, "搜索引擎") {
		t.Fatal("unavailable analyzer reported dictionary term")
	}
}

func TestCJKAnalyzerHelperBoundaries(t *testing.T) {
	if cjkQueryAnalyzer(cjkKoreanTextAnalyzer) != cjkKoreanQueryAnalyzer ||
		cjkQueryAnalyzer(standardTextAnalyzer) != standardTextAnalyzer {
		t.Fatal("query analyzer mapping mismatch")
	}
	if _, found := cjkTokenizerConfiguration("unsupported", false); found {
		t.Fatal("unsupported tokenizer configuration found")
	}
	if normalizedUnstemmedWord("カタ", cjkJapaneseTextAnalyzer) != "カタ" ||
		normalizedUnstemmedWord("검색", cjkKoreanTextAnalyzer) != "검색" {
		t.Fatal("language-specific CJK normalization mismatch")
	}
	if queryAnalyzerScript("漢かな") != unicode.Hiragana ||
		queryAnalyzerScript("漢カナ") != unicode.Katakana ||
		queryAnalyzerScript("漢한글") != unicode.Hangul {
		t.Fatal("mixed CJK script routing mismatch")
	}
	if analyzer, found := scriptQualifiedLanguageAnalyzer(
		"ko",
		"한글",
	); !found ||
		analyzer != cjkKoreanTextAnalyzer {
		t.Fatalf("Korean analyzer = %q found=%v", analyzer, found)
	}
	if containsAnyScript("123", unicode.Han, unicode.Hangul) {
		t.Fatal("scriptless text matched CJK")
	}
	query := crossFieldTermClause(
		"搜索引擎",
		[]string{cjkChineseTextAnalyzer},
		RankingWeights{}.orDefault(),
		1,
	)
	if query == nil {
		t.Fatal("dictionary cross-field query missing")
	}
}

func TestStoredCJKSequenceBoundaries(t *testing.T) {
	if locations := storedAnalyzerRequirementLocations(
		&storedEvidenceMatcher{},
		nil,
		0,
	); locations != nil {
		t.Fatalf("empty requirement locations = %#v", locations)
	}
	left := &search.Location{Pos: 3, Start: 3, End: 6}
	right := &search.Location{Pos: 2, Start: 0, End: 3}
	if !storedAnalyzerLocationsAdjacent(left, right, 1, false) ||
		storedAnalyzerLocationsAdjacent(nil, right, 1, false) ||
		storedAnalyzerLocationsAdjacent(
			&search.Location{Pos: 1, ArrayPositions: search.ArrayPositions{0}},
			&search.Location{Pos: 2, ArrayPositions: search.ArrayPositions{1}},
			1,
			true,
		) {
		t.Fatal("sequence adjacency mismatch")
	}
	if !sameStoredLocationSpan(
		&search.Location{Pos: 1, Start: 0, End: 3},
		&search.Location{Pos: 1, Start: 0, End: 3},
	) {
		t.Fatal("identical spans differ")
	}
	values := []string{"one"}
	if _, found := storedLocationValue(values, &search.Location{
		ArrayPositions: search.ArrayPositions{math.MaxUint64},
	}); found {
		t.Fatal("invalid array position found")
	}
	targets := make([]storedEvidenceTarget, maximumTermPositionsPerField+1)
	for index := range targets {
		targets[index] = storedEvidenceTarget{rawRequirement: 0, analyzerPosition: index + 1}
	}
	if groups := storedAnalyzerTargetGroups(
		targets,
		0,
	); len(
		groups,
	) != maximumTermPositionsPerField {
		t.Fatalf("bounded groups = %d", len(groups))
	}
	if groups := storedAnalyzerTargetGroups(
		[]storedEvidenceTarget{{rawRequirement: 0}},
		0,
	); len(groups) != 1 ||
		groups[0].position != 1 {
		t.Fatalf("default analyzer position groups = %#v", groups)
	}
	samePosition := &storedEvidenceMatcher{
		rawRequirements: []string{"x"},
		targets: []storedEvidenceTarget{
			{rawRequirement: 0, analyzerPosition: 1},
			{rawRequirement: 0, analyzerPosition: 1},
		},
	}
	if unordered, ordered := storedSingleRequirementAnalyzerProximity(
		samePosition,
		nil,
		true,
	); unordered != 0 ||
		ordered != 0 {
		t.Fatalf("single group proximity = %v/%v", unordered, ordered)
	}
}

func TestStoredCJKSurfaceGapBoundaries(t *testing.T) {
	if units := storedCJKSeparatorUnits("甲乙 abc 123"); units != 4 {
		t.Fatalf("separator units = %d", units)
	}
	values := []string{"左 甲乙 right"}
	left := &search.Location{Start: 0, End: uint64(len("左"))}
	right := &search.Location{End: uint64(len(values[0]))}
	const rightLength = uint64(len("right"))
	right.Start = right.End - rightLength
	if gap, found := storedCJKLocationGap(values, right, left); !found || gap != 3 {
		t.Fatalf("reverse gap = %d found=%v", gap, found)
	}
	if _, found := storedCJKOrderedLocationGap(values, right, left); found {
		t.Fatal("overlapping order accepted")
	}
	if _, found := storedCJKOrderedLocationGap(
		values,
		left,
		&search.Location{Start: uint64(len(values[0]) + 1)},
	); found {
		t.Fatal("out-of-range location accepted")
	}
}

func TestStoredCJKScannerBoundaries(t *testing.T) {
	request := SearchRequest{Terms: []string{"搜索"}}
	matcher := newStoredEvidenceMatcher(request, cjkChineseTextAnalyzer)
	fallback := *matcher
	fallback.analyzer = nil
	if _, err := scanStoredDictionaryCJKFieldEvidence(
		t.Context(),
		&fallback,
		[]string{"搜索"},
		true,
	); err != nil {
		t.Fatalf("fallback scan: %v", err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := scanStoredDictionaryCJKFieldEvidence(
		canceled,
		matcher,
		[]string{"搜索"},
		true,
	); err == nil {
		t.Fatal("canceled scan succeeded")
	}

	invalid := *matcher
	invalid.analyzer = fixedCJKTestAnalyzer{tokens: analysis.TokenStream{
		&analysis.Token{Term: []byte("搜索"), Start: -1, End: 1, Position: 1},
	}}
	if evidence, err := scanStoredDictionaryCJKFieldEvidence(
		t.Context(),
		&invalid,
		[]string{"搜索"},
		true,
	); err != nil || len(evidence.requirementTerms["搜索"]) != 0 {
		t.Fatalf("invalid offsets evidence=%#v err=%v", evidence, err)
	}

	if evidence, err := scanStoredDictionaryCJKFieldEvidence(
		t.Context(),
		matcher,
		[]string{"搜索引擎"},
		false,
	); err != nil || len(evidence.requirementTerms["搜索"]) != 1 {
		t.Fatalf("bounded scan evidence=%#v err=%v", evidence, err)
	}

	phraseMatcher := newStoredEvidenceMatcher(SearchRequest{
		Terms:   []string{"搜索", "引擎"},
		Phrases: []string{"搜索引擎"},
	}, cjkChineseTextAnalyzer)
	if evidence, err := scanStoredDictionaryCJKFieldEvidence(
		t.Context(),
		phraseMatcher,
		[]string{"搜索引擎"},
		true,
	); err != nil || len(evidence.phraseTerms) == 0 {
		t.Fatalf("phrase evidence=%#v err=%v", evidence, err)
	}

	values := make([]string, maximumTermPositionsPerField+8)
	for index := range values {
		values[index] = "搜索"
	}
	if evidence, err := scanStoredDictionaryCJKFieldEvidence(
		t.Context(),
		matcher,
		values,
		true,
	); err != nil || len(evidence.requirementTerms["搜索"]) != maximumTermPositionsPerField {
		t.Fatalf("bounded locations=%d err=%v", len(evidence.requirementTerms["搜索"]), err)
	}
	assertStoredCJKLegacyScannerBoundaries(t, matcher)
}

func assertStoredCJKLegacyScannerBoundaries(
	t *testing.T,
	matcher *storedEvidenceMatcher,
) {
	t.Helper()
	legacy := newStoredEvidenceMatcher(
		SearchRequest{Terms: []string{"AI", "模型"}},
		"cjk",
	)
	if _, err := scanStoredCJKFieldEvidence(
		t.Context(),
		legacy,
		[]string{"AI模型"},
		true,
	); err != nil {
		t.Fatalf("legacy mixed scan: %v", err)
	}
	canceledVisible, cancelVisible := context.WithCancel(t.Context())
	cancelVisible()
	if _, _, err := scanVisibleCJKFieldEvidence(
		canceledVisible,
		matcher,
		"搜索",
		true,
	); err == nil {
		t.Fatal("canceled visible scan succeeded")
	}
}

func TestStoredCJKRequirementCollapseRejectsInvalidSpans(t *testing.T) {
	matcher := &storedEvidenceMatcher{
		rawRequirements: []string{"x"},
		targets: []storedEvidenceTarget{
			{rawRequirement: 0, analyzerPosition: 1},
		},
	}
	field := newStoredFieldEvidence(matcher)
	field.targetTerms[0] = search.Locations{
		&search.Location{Pos: 1, Start: 2, End: 1},
		&search.Location{Pos: 2, Start: 0, End: 2},
	}
	collapseStoredCJKRequirements(matcher, []string{"x"}, &field)
	if len(field.exactTerms["x"]) != 0 {
		t.Fatalf("invalid exact spans = %#v", field.exactTerms)
	}
}

func cjkTestFST(t *testing.T, entries map[string]uint64) []byte {
	t.Helper()
	buffer := bytes.Buffer{}
	builder, err := vellum.New(&buffer, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"word"} {
		if err := builder.Insert([]byte(key), entries[key]); err != nil {
			t.Fatal(err)
		}
	}
	if err := builder.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

func encodedCJKTestAsset(data []byte) string {
	buffer := bytes.Buffer{}
	writer := gzip.NewWriter(&buffer)
	_, _ = writer.Write(data)
	_ = writer.Close()

	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}
