package contentprior

import (
	"strings"
	"unicode"

	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

const (
	minScoredWords          = 20
	idealFunctionFraction   = 0.20
	maxSymbolFraction       = 0.10
	idealAlphabeticFraction = 0.85
	unsegmentedSkipShare    = 0.30
)

type Evidence struct {
	Known                bool
	Score                float64
	FunctionWordFraction float64
	SymbolFraction       float64
	AlphabeticFraction   float64
	UniqueTokenFraction  float64
	SpamRisk             float64
}

func Analyze(text string) Evidence {
	words := strings.Fields(text)
	if len(words) < minScoredWords || unsegmentedScript(text) {
		return Evidence{}
	}

	functionWords, symbolWords, alphabeticWords := 0, 0, 0
	uniqueTokens := make(map[string]struct{}, len(words))
	for _, word := range words {
		normalized := normalizedWord(word)
		if stopwords.IsStopword(normalized) {
			functionWords++
		}
		if hasSymbol(word) {
			symbolWords++
		}
		if strings.ContainsFunc(word, unicode.IsLetter) {
			alphabeticWords++
		}
		if normalized != "" {
			uniqueTokens[normalized] = struct{}{}
		}
	}

	total := float64(len(words))
	functionWordFraction := float64(functionWords) / total
	symbolFraction := float64(symbolWords) / total
	alphabeticFraction := float64(alphabeticWords) / total
	uniqueTokenFraction := float64(len(uniqueTokens)) / total
	functionScore := clamp01(functionWordFraction / idealFunctionFraction)
	symbolScore := 1 - clamp01(symbolFraction/maxSymbolFraction)
	alphabeticScore := clamp01(alphabeticFraction / idealAlphabeticFraction)
	quality := functionScore * (symbolScore + alphabeticScore) / 2
	spamRisk := 1 - quality

	return Evidence{
		Known:                true,
		Score:                1 - 2*spamRisk,
		FunctionWordFraction: functionWordFraction,
		SymbolFraction:       symbolFraction,
		AlphabeticFraction:   alphabeticFraction,
		UniqueTokenFraction:  uniqueTokenFraction,
		SpamRisk:             spamRisk,
	}
}

func Score(text string) float64 {
	return Analyze(text).Score
}

func normalizedWord(word string) string {
	return strings.ToLower(strings.TrimFunc(word, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}))
}

func hasSymbol(word string) bool {
	return strings.Contains(word, "#") ||
		strings.Contains(word, "…") ||
		strings.Contains(word, "...")
}

func clamp01(value float64) float64 {
	return min(1, max(0, value))
}

func unsegmentedScript(text string) bool {
	letters, unsegmented := 0, 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana,
			unicode.Hangul, unicode.Thai, unicode.Lao, unicode.Khmer, unicode.Myanmar) {
			unsegmented++
		}
	}

	return letters > 0 && float64(unsegmented)/float64(letters) > unsegmentedSkipShare
}
