package searchindex

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

var storedPhraseBenchmarkPreference float64

func TestStoredPhraseEvidenceMatchesInternalApostrophePositions(t *testing.T) {
	for _, apostrophe := range []string{"'", "’", "＇"} {
		phrase := "tool" + apostrophe + "s archive"
		candidates := []SearchResult{
			{DocumentID: "scattered", Score: 1, Analyzer: searchTextAnalyzer},
			{DocumentID: "exact", Score: 1, Analyzer: searchTextAnalyzer},
		}
		documents := []documentstore.Document{
			{ExtractedText: "tool" + apostrophe + "s remote archive", Language: "en"},
			{ExtractedText: phrase, Language: "en"},
		}
		results, err := searchEvidenceResults(
			t.Context(),
			SearchRequest{
				Query: phrase, Terms: strings.Fields(phrase),
				Phrases: []string{phrase},
			},
			candidates,
			func(index int) (documentstore.Document, bool, error) {
				return documents[index], true, nil
			},
		)
		if err != nil {
			t.Fatalf("apostrophe %q evidence: %v", apostrophe, err)
		}
		if results[0].DocumentID != "exact" || results[0].Score <= results[1].Score {
			t.Fatalf("apostrophe %q results = %#v", apostrophe, results)
		}
	}
}

func TestStoredPhraseEvidenceKeepsExactPositionsPrivate(t *testing.T) {
	phrase := "tool’s archive"
	req := SearchRequest{
		Query: phrase, Terms: strings.Fields(phrase),
		Phrases: []string{phrase}, IncludePositions: true,
	}
	evidence, err := storedDocumentLocations(
		t.Context(),
		documentstore.Document{ExtractedText: phrase},
		req,
		searchTextAnalyzer,
	)
	if err != nil {
		t.Fatalf("stored evidence: %v", err)
	}
	positions := exactSurfaceFieldTermPositions(req, evidence.exactLocations)["body"]
	if got := positions["tool’s"]; len(got) != 1 || got[0] != 1 {
		t.Fatalf("tool exact positions = %v", got)
	}
	if got := positions["archive"]; len(got) != 1 || got[0] != 2 {
		t.Fatalf("archive exact positions = %v", got)
	}
}

func TestStoredPhraseEvidenceBoundsAnalyzerPositions(t *testing.T) {
	phrase := "tool’s archive"
	req := SearchRequest{
		Query: phrase, Terms: strings.Fields(phrase),
		Phrases: []string{phrase},
	}
	evidence, err := storedDocumentLocations(
		t.Context(),
		documentstore.Document{
			ExtractedText: strings.Repeat(
				phrase+" ",
				maximumTermPositionsPerField+8,
			),
		},
		req,
		searchTextAnalyzer,
	)
	if err != nil {
		t.Fatalf("stored evidence: %v", err)
	}
	if got := len(evidence.phraseLocations["body"]["tool"]); got != maximumTermPositionsPerField {
		t.Fatalf("bounded tool positions = %d", got)
	}
	if preference := storedQuotedPhrasePreference(
		evidence.phraseLocations,
		req.Phrases,
		storedEvidenceAnalyzer(searchTextAnalyzer),
	); preference != 1 {
		t.Fatalf("bounded phrase preference = %v", preference)
	}
}

func TestStoredPhraseEvidenceRejectsAnalyzerAnchorFalsePositive(t *testing.T) {
	phrase := "running archive"
	req := SearchRequest{
		Query: phrase, Terms: strings.Fields(phrase),
		Phrases: []string{phrase},
	}
	evidence, err := storedDocumentLocations(
		t.Context(),
		documentstore.Document{ExtractedText: "runner archive"},
		req,
		searchTextAnalyzer,
	)
	if err != nil {
		t.Fatalf("stored evidence: %v", err)
	}
	if preference := storedQuotedPhrasePreference(
		evidence.phraseLocations,
		req.Phrases,
		storedEvidenceAnalyzer(searchTextAnalyzer),
	); preference != 0 {
		t.Fatalf("anchor-only phrase preference = %v", preference)
	}
}

func TestStoredPhraseEvidenceAnalyzesTermsOutsideSearchTargets(t *testing.T) {
	phrase := "tool’s archive"
	req := SearchRequest{
		Query: phrase, Terms: []string{"archive"},
		Phrases: []string{phrase},
	}
	evidence, err := storedDocumentLocations(
		t.Context(),
		documentstore.Document{ExtractedText: phrase},
		req,
		searchTextAnalyzer,
	)
	if err != nil {
		t.Fatalf("stored evidence: %v", err)
	}
	if preference := storedQuotedPhrasePreference(
		evidence.phraseLocations,
		req.Phrases,
		storedEvidenceAnalyzer(searchTextAnalyzer),
	); preference != 1 {
		t.Fatalf("independent phrase preference = %v", preference)
	}
}

func TestStoredPhraseTokenMatcherHandlesDegeneratePhrases(t *testing.T) {
	analyzer := storedEvidenceAnalyzer(searchTextAnalyzer)
	if matcher := newStoredPhraseTokenMatcher([]string{"single"}, analyzer); matcher.enabled() {
		t.Fatalf("single-term matcher = %#v", matcher.terms)
	}
	matcher := newStoredPhraseTokenMatcher([]string{"archive archive"}, analyzer)
	if len(matcher.terms) != 1 {
		t.Fatalf("repeated-term matcher = %#v", matcher.terms)
	}
}

func TestStoredPhraseEvidenceUsesCJKAnalyzerPositions(t *testing.T) {
	phrase := "游戏鼠标"
	req := SearchRequest{
		Query: phrase, Terms: []string{phrase},
		Phrases: []string{phrase},
	}
	matched, err := storedDocumentLocations(
		t.Context(),
		documentstore.Document{ExtractedText: phrase},
		req,
		"cjk",
	)
	if err != nil {
		t.Fatalf("matched CJK evidence: %v", err)
	}
	scattered, err := storedDocumentLocations(
		t.Context(),
		documentstore.Document{ExtractedText: "游戏设备鼠标"},
		req,
		"cjk",
	)
	if err != nil {
		t.Fatalf("scattered CJK evidence: %v", err)
	}
	analyzer := storedEvidenceAnalyzer("cjk")
	if preference := storedQuotedPhrasePreference(
		matched.phraseLocations,
		req.Phrases,
		analyzer,
	); preference != 1 {
		t.Fatalf("matched CJK preference = %v", preference)
	}
	if preference := storedQuotedPhrasePreference(
		scattered.phraseLocations,
		req.Phrases,
		analyzer,
	); preference != 0 {
		t.Fatalf("scattered CJK preference = %v", preference)
	}
}

func BenchmarkStoredApostrophePhraseEvidence(b *testing.B) {
	phrase := "tool’s archive"
	document := documentstore.Document{
		ExtractedText: strings.Repeat("background material ", 128) +
			phrase + strings.Repeat(" appendix material", 128),
	}
	analyzer := storedEvidenceAnalyzer(searchTextAnalyzer)
	for _, benchmark := range []struct {
		name              string
		requestPhrases    []string
		analyzerPositions bool
	}{
		{name: "prior-position-control", requestPhrases: []string{"single"}},
		{name: "analyzer-positions", requestPhrases: []string{phrase}, analyzerPositions: true},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			req := SearchRequest{
				Query: phrase, Terms: strings.Fields(phrase),
				Phrases: benchmark.requestPhrases,
			}
			b.ReportAllocs()
			for range b.N {
				evidence, err := storedDocumentLocations(
					b.Context(),
					document,
					req,
					searchTextAnalyzer,
				)
				if err != nil {
					b.Fatal(err)
				}
				locations := evidence.locations
				if benchmark.analyzerPositions {
					locations = evidence.phraseLocations
				}
				storedPhraseBenchmarkPreference = storedQuotedPhrasePreference(
					locations,
					[]string{phrase},
					analyzer,
				)
			}
		})
	}
}
