package searchindex

import (
	"strings"
	"unicode"

	"github.com/abadojack/whatlanggo"
	// Per-language analyzers bleve resolves by name in the field mappings. Only
	// the languages whose package registers a complete analyzer (not merely a
	// stop-word filter) are imported; the rest fall through to the standard
	// analyzer.
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ar"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/cjk"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ckb"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/da"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/de"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/es"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fa"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fi"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fr"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hi"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hr"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hu"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/it"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/nl"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/no"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/pl"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/pt"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ro"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ru"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/sv"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/tr"
	"github.com/blevesearch/bleve/v2/mapping"
)

const standardTextAnalyzer = "standard_text"

// stemmingAnalyzers lists the bleve language analyzers a document may be routed
// to, keyed by the analyzer name used both in the field mappings and as the
// document's TypeField value. "cjk" bigram-segments the uninflected CJK
// languages; the rest are Snowball or custom stemmers.
func stemmingAnalyzers() []string {
	return []string{
		"ar", "cjk", "ckb", "da", "de", "es", "fa", "fi", "fr", "hi", "hr",
		"hu", "it", "nl", "no", "pl", "pt", "ro", "ru", "sv", "tr",
		searchTextAnalyzer,
	}
}

// documentAnalyzers is every analyzer a document mapping is registered for: the
// stemming analyzers plus the no-stemming fallback.
func documentAnalyzers() []string {
	return append(stemmingAnalyzers(), standardTextAnalyzer)
}

// languageToAnalyzer maps an ISO 639-1 language code to the analyzer that best
// normalizes it. Serbian and Bosnian share Croatian's Serbo-Croatian stemmer;
// Chinese, Japanese, and Korean share the CJK bigram analyzer; a language with
// no bleve analyzer (Hebrew, Thai, ...) falls through to the standard analyzer.
func languageToAnalyzer(code string) string {
	code = normalizedLanguageCode(code)
	switch code {
	case "sr", "bs":
		return "hr"
	case "zh", "ja", "ko":
		return "cjk"
	case "en":
		return searchTextAnalyzer
	}
	for _, analyzer := range stemmingAnalyzers() {
		if analyzer == code {
			return analyzer
		}
	}

	return standardTextAnalyzer
}

// detectDocumentAnalyzer chooses a document's analyzer from its extracted text,
// using the crawl-time HTML lang attribute as a prior: a reliable content
// detection wins, otherwise a usable lang hint decides, and otherwise the
// dominant Unicode script's primary analyzer is used (so short or ambiguous
// text still stems with its script's most common language rather than a wrong
// guess). Detection runs on document bodies, where language identification is
// accurate, never on queries.
func detectDocumentAnalyzer(text string, htmlLang string) string {
	if analyzer, ok := scriptQualifiedLanguageAnalyzer(htmlLang, text); ok {
		return analyzer
	}
	if info := whatlanggo.Detect(text); info.IsReliable() && info.Lang.Iso6391() != "" {
		return reliableLanguageAnalyzer(info.Lang.Iso6391(), text)
	}
	if analyzer, ok := analyzerFromLangHint(htmlLang); ok {
		return analyzer
	}
	if candidates := scriptAnalyzers(dominantScript(text)); len(candidates) > 0 {
		return candidates[0]
	}

	return standardTextAnalyzer
}

// analyzerFromLangHint resolves the HTML lang attribute to an analyzer, if it
// names a language we route.
func analyzerFromLangHint(htmlLang string) (string, bool) {
	htmlLang = strings.TrimSpace(htmlLang)
	if htmlLang == "" {
		return "", false
	}
	analyzer := languageToAnalyzer(htmlLang)
	if analyzer == standardTextAnalyzer {
		return "", false
	}

	return analyzer, true
}

// queryAnalyzers picks the analyzers to interpret a query with, from its
// dominant Unicode script rather than by identifying the query language (which
// is unreliable on short strings). The standard analyzer is always included so
// an exact word or proper noun matches a document in any language.
func queryAnalyzers(query string) []string {
	candidates := scriptAnalyzers(queryAnalyzerScript(query))
	analyzers := make([]string, 0, len(candidates)+1)
	seen := map[string]bool{}
	for _, analyzer := range append(candidates, standardTextAnalyzer) {
		if !seen[analyzer] {
			seen[analyzer] = true
			analyzers = append(analyzers, analyzer)
		}
	}

	return analyzers
}

// scriptAnalyzers lists the stemming analyzers that serve a Unicode script.
// Latin is bounded to the most common web languages to keep the query fan-out
// small; other scripts resolve to a short candidate set or none.
func scriptAnalyzers(script *unicode.RangeTable) []string {
	switch script {
	case unicode.Cyrillic:
		return []string{"ru"}
	case unicode.Latin:
		return []string{
			searchTextAnalyzer, "da", "de", "es", "fi", "fr", "hr", "hu", "it",
			"nl", "no", "pl", "pt", "ro", "sv", "tr",
		}
	case unicode.Arabic:
		return []string{"ar", "fa", "ckb"}
	case unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul:
		return []string{"cjk"}
	case unicode.Devanagari:
		return []string{"hi"}
	}

	return nil
}

// dominantScript reports the Unicode script most of the query's letters belong
// to, so a query is interpreted with the analyzers of its own writing system.
func dominantScript(text string) *unicode.RangeTable {
	counts := map[*unicode.RangeTable]int{}
	scripts := []*unicode.RangeTable{
		unicode.Cyrillic, unicode.Latin, unicode.Arabic, unicode.Han,
		unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Greek,
		unicode.Devanagari, unicode.Armenian, unicode.Hebrew,
	}
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		for _, script := range scripts {
			if unicode.Is(script, r) {
				counts[script]++

				break
			}
		}
	}
	var best *unicode.RangeTable
	bestCount := 0
	for _, script := range scripts {
		if counts[script] > bestCount {
			best, bestCount = script, counts[script]
		}
	}

	return best
}

// registerStandardTextAnalyzer wires the no-stemming fallback analyzer. It is a
// package var so a test can force a registration failure.
var registerStandardTextAnalyzer = func(indexMapping *mapping.IndexMappingImpl) error {
	return indexMapping.AddCustomAnalyzer(standardTextAnalyzer, map[string]any{
		"type":          "custom",
		"tokenizer":     "unicode",
		"token_filters": []string{lowercaseFilter, unicodeNormFilter},
	})
}
