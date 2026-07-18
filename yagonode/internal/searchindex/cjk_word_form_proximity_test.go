package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

var cjkMixedScriptBenchmarkProximity float64

func TestCJKExactTermProximityIgnoresBridgeBigramPositions(t *testing.T) {
	for name, terms := range map[string][]string{
		"simplified":  {"搜索", "引擎"},
		"traditional": {"搜尋", "引擎"},
	} {
		t.Run(name, func(t *testing.T) {
			joined := storedBodyWordFormEvidence(
				t,
				"cjk",
				terms,
				terms[0]+terms[1],
				false,
			)
			spaced := storedBodyWordFormEvidence(
				t,
				"cjk",
				terms,
				terms[0]+" "+terms[1],
				false,
			)
			scattered := storedBodyWordFormEvidence(
				t,
				"cjk",
				terms,
				terms[0]+" 1 2 3 4 5 6 7 8 9 "+terms[1],
				false,
			)
			if joined.proximity != 1 || joined.orderedProximity != 1 {
				t.Fatalf(
					"joined CJK evidence = %v/%v",
					joined.proximity,
					joined.orderedProximity,
				)
			}
			left := joined.exactLocations["body"][terms[0]]
			right := joined.exactLocations["body"][terms[1]]
			if len(left) != 1 || len(right) != 1 ||
				left[0].Pos != 1 || right[0].Pos != 2 {
				t.Fatalf("joined CJK exact positions = %#v", joined.exactLocations)
			}
			if spaced.proximity != 1 || spaced.orderedProximity != 1 {
				t.Fatalf(
					"spaced CJK evidence = %v/%v",
					spaced.proximity,
					spaced.orderedProximity,
				)
			}
			if scattered.proximity != 0 || scattered.orderedProximity != 0 {
				t.Fatalf(
					"scattered CJK evidence = %v/%v",
					scattered.proximity,
					scattered.orderedProximity,
				)
			}
		})
	}
}

func TestCJKUnsegmentedAnalyzerSequenceChangesStoredOrder(t *testing.T) {
	results := []SearchResult{
		{DocumentID: "scattered", Score: 1},
		{
			DocumentID:       "compact",
			Score:            1,
			Proximity:        analyzerVariantPairConfidence,
			OrderedProximity: analyzerVariantPairConfidence,
		},
	}
	rescoreStoredProximity(results, SearchRequest{
		Terms:            []string{"搜索引擎"},
		IncludePositions: true,
	})
	if results[0].DocumentID != "compact" || results[0].Score <= results[1].Score {
		t.Fatalf("unsegmented CJK stored order = %#v", results)
	}
	withoutSequence := []SearchResult{
		{DocumentID: "first", Score: 1},
		{DocumentID: "second", Score: 1},
	}
	rescoreStoredProximity(withoutSequence, SearchRequest{
		Terms:            []string{"搜索引擎"},
		IncludePositions: true,
	})
	if withoutSequence[0].DocumentID != "first" || withoutSequence[0].Score != 1 {
		t.Fatalf("single raw term without analyzer sequence = %#v", withoutSequence)
	}
}

func TestCJKUnsegmentedAnalyzerSequenceRanksCompactDocument(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://alpha.example/scattered",
			ExtractedText: "搜索 甲乙 索引 丙丁 引擎",
			Language:      "zh-Hans",
		},
		{
			NormalizedURL: "https://zebra.example/compact",
			ExtractedText: "搜索引擎 甲乙 丙丁",
			Language:      "zh-Hans",
		},
	}
	for _, document := range documents {
		if err := index.Index(t.Context(), document); err != nil {
			t.Fatalf("Index(%s): %v", document.NormalizedURL, err)
		}
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query:            "搜索引擎",
		Terms:            []string{"搜索引擎"},
		MaxResults:       len(documents),
		IncludePositions: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != len(documents) ||
		result.Results[0].URL != documents[1].NormalizedURL ||
		result.Results[1].URL != documents[0].NormalizedURL {
		t.Fatalf("unsegmented CJK result order = %#v", result.Results)
	}
	if result.Results[0].Score <= result.Results[1].Score ||
		result.Results[0].OrderedProximity != analyzerVariantPairConfidence ||
		result.Results[1].OrderedProximity != 0 {
		t.Fatalf("unsegmented CJK ranking evidence = %#v", result.Results)
	}
}

func TestCJKMixedScriptWithoutSeparatorKeepsStoredEvidence(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	if err := index.Index(t.Context(), documentstore.Document{
		NormalizedURL: "https://example.test/mixed-cjk",
		ExtractedText: "AI模型",
		Language:      "zh-Hans",
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query:            "AI 模型",
		Terms:            []string{"AI", "模型"},
		MaxResults:       1,
		IncludePositions: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].Proximity != 1 ||
		result.Results[0].OrderedProximity != 1 {
		t.Fatalf("mixed CJK result = %#v", result.Results)
	}
	positions := result.Results[0].FieldTermPositions["body"]
	if len(positions["ai"]) != 1 || positions["ai"][0] != 1 ||
		len(positions["模型"]) != 1 || positions["模型"][0] != 2 {
		t.Fatalf("mixed CJK positions = %#v", positions)
	}
}

func TestCJKStoredEvidenceNormalizesWidthVariants(t *testing.T) {
	for name, fixture := range map[string]struct {
		surface string
		term    string
	}{
		"fullwidth Latin": {surface: "ＡＩ", term: "ai"},
		"halfwidth Kana":  {surface: "ｶﾀ", term: "カタ"},
	} {
		t.Run(name, func(t *testing.T) {
			evidence, err := storedDocumentLocations(
				t.Context(),
				documentstore.Document{ExtractedText: fixture.surface},
				SearchRequest{Terms: []string{fixture.term}},
				"cjk",
			)
			if err != nil {
				t.Fatalf("storedDocumentLocations: %v", err)
			}
			if len(evidence.locations["body"][fixture.term]) != 1 {
				t.Fatalf("normalized CJK evidence = %#v", evidence.locations)
			}
			if len(evidence.exactLocations["body"][fixture.term]) != 0 {
				t.Fatalf("width variant became exact = %#v", evidence.exactLocations)
			}
		})
	}
}

func TestCJKWidthVariantsRetrieveWithStoredEvidence(t *testing.T) {
	for name, fixture := range map[string]struct {
		document string
		query    string
		terms    []string
	}{
		"fullwidth Latin": {
			document: "ＡＩ 模型",
			query:    "AI 模型",
			terms:    []string{"AI", "模型"},
		},
		"halfwidth Kana": {
			document: "ｶﾀ 模型",
			query:    "カタ 模型",
			terms:    []string{"カタ", "模型"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			index, err := NewBleveMemoryIndex(t.Context(), nil)
			if err != nil {
				t.Fatalf("NewBleveMemoryIndex: %v", err)
			}
			if err := index.Index(t.Context(), documentstore.Document{
				NormalizedURL: "https://example.test/width-cjk",
				ExtractedText: fixture.document,
				Language:      "zh-Hans",
			}); err != nil {
				t.Fatalf("Index: %v", err)
			}
			result, err := index.Search(t.Context(), SearchRequest{
				Query:            fixture.query,
				Terms:            fixture.terms,
				MaxResults:       1,
				IncludePositions: true,
			})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(result.Results) != 1 ||
				result.Results[0].Analyzer != cjkChineseTextAnalyzer ||
				result.Results[0].Proximity != analyzerVariantPairConfidence ||
				result.Results[0].OrderedProximity != analyzerVariantPairConfidence {
				t.Fatalf("width CJK result = %#v", result.Results)
			}
		})
	}
}

func BenchmarkCJKMixedScriptStoredEvidence(b *testing.B) {
	req := SearchRequest{
		Query: "AI 模型", Terms: []string{"AI", "模型"},
		IncludePositions: true,
	}
	for _, benchmark := range []struct {
		name string
		text string
	}{
		{name: "separated", text: "AI 模型"},
		{name: "adjacent", text: "AI模型"},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				evidence, err := storedDocumentLocations(
					b.Context(),
					documentstore.Document{ExtractedText: benchmark.text},
					req,
					"cjk",
				)
				if err != nil {
					b.Fatal(err)
				}
				cjkMixedScriptBenchmarkProximity = evidence.proximity
			}
		})
	}
}

func TestCJKUnsegmentedQueryUsesAnalyzerSequenceProximity(t *testing.T) {
	for name, phrase := range map[string]string{
		"simplified":  "搜索引擎",
		"traditional": "搜尋引擎",
	} {
		t.Run(name, func(t *testing.T) {
			evidence := storedBodyWordFormEvidence(
				t,
				"cjk",
				[]string{phrase},
				phrase,
				false,
			)
			if evidence.proximity != analyzerVariantPairConfidence ||
				evidence.orderedProximity != analyzerVariantPairConfidence {
				t.Fatalf(
					"unsegmented CJK evidence = %v/%v",
					evidence.proximity,
					evidence.orderedProximity,
				)
			}
			if len(evidence.exactLocations["body"][phrase]) != 0 {
				t.Fatalf("analyzer sequence became exact: %#v", evidence.exactLocations)
			}
		})
	}
}

func TestCJKExactRecallWithAndWithoutQueryWhitespace(t *testing.T) {
	for name, fixture := range map[string]struct {
		language string
		terms    []string
	}{
		"simplified":  {language: "zh-Hans", terms: []string{"搜索", "引擎"}},
		"traditional": {language: "zh-Hant", terms: []string{"搜尋", "引擎"}},
	} {
		t.Run(name, func(t *testing.T) {
			assertCJKExactRecall(t, name, fixture.language, fixture.terms)
		})
	}
}

func assertCJKExactRecall(
	t *testing.T,
	name string,
	language string,
	terms []string,
) {
	t.Helper()
	phrase := terms[0] + terms[1]
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	if err := index.Index(t.Context(), documentstore.Document{
		NormalizedURL: "https://example.test/" + name,
		ExtractedText: phrase,
		Language:      language,
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	for queryName, request := range map[string]SearchRequest{
		"spaced": {
			Query:            terms[0] + " " + terms[1],
			Terms:            terms,
			MaxResults:       1,
			IncludePositions: true,
		},
		"unsegmented": {
			Query:            phrase,
			Terms:            []string{phrase},
			MaxResults:       1,
			IncludePositions: true,
		},
	} {
		t.Run(queryName, func(t *testing.T) {
			result, err := index.Search(t.Context(), request)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if result.Total != 1 || len(result.Results) != 1 {
				t.Fatalf("CJK recall = %#v", result)
			}
			if result.Results[0].Analyzer != cjkChineseTextAnalyzer ||
				result.Results[0].Proximity == 0 ||
				result.Results[0].OrderedProximity == 0 {
				t.Fatalf("CJK result evidence = %#v", result.Results[0])
			}
		})
	}
}
