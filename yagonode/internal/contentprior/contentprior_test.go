package contentprior

import (
	"reflect"
	"strings"
	"testing"
)

func TestAnalyzeReturnsUnknownNeutralEvidence(t *testing.T) {
	for _, text := range []string{
		"only a handful of words here",
		strings.Repeat("字 ", minScoredWords),
	} {
		if got := Analyze(text); !reflect.DeepEqual(got, Evidence{}) {
			t.Fatalf("Analyze(%q) = %#v, want zero evidence", text, got)
		}
		if got := Score(text); got != 0 {
			t.Fatalf("Score(%q) = %v, want 0", text, got)
		}
	}
}

func TestAnalyzeExposesCleanProseFractions(t *testing.T) {
	text := "the cat and dog are in the house and sun bright alpha beta gamma delta epsilon zeta eta theta iota"
	got := Analyze(text)
	want := Evidence{
		Known:                true,
		Score:                1,
		FunctionWordFraction: 0.3,
		SymbolFraction:       0,
		AlphabeticFraction:   1,
		UniqueTokenFraction:  0.9,
		SpamRisk:             0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Analyze(clean prose) = %#v, want %#v", got, want)
	}
}

func TestAnalyzeDemotesKeywordStuffing(t *testing.T) {
	text := "linux kernel driver module compile firmware bootloader chipset router firewall gateway payload buffer register opcode syscall mmap vector matrix tensor"
	got := Analyze(text)
	if !got.Known || got.Score != -1 || got.SpamRisk != 1 ||
		got.FunctionWordFraction != 0 || got.SymbolFraction != 0 ||
		got.AlphabeticFraction != 1 || got.UniqueTokenFraction != 1 {
		t.Fatalf("Analyze(keyword stuffing) = %#v", got)
	}
}

func TestAnalyzeMeasuresSymbolsNumbersAndNormalizedTokens(t *testing.T) {
	text := "the and in of Alpha, alpha! beta gamma delta epsilon zeta eta theta iota kappa lambda #tag ## … ..."
	got := Analyze(text)
	want := Evidence{
		Known:                true,
		Score:                0,
		FunctionWordFraction: 0.2,
		SymbolFraction:       0.2,
		AlphabeticFraction:   0.85,
		UniqueTokenFraction:  0.8,
		SpamRisk:             0.5,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Analyze(mixed text) = %#v, want %#v", got, want)
	}

	numbers := Analyze(strings.Repeat("12345 ", minScoredWords))
	if !numbers.Known || numbers.Score != -1 || numbers.SpamRisk != 1 ||
		numbers.AlphabeticFraction != 0 || numbers.UniqueTokenFraction != 0.05 {
		t.Fatalf("Analyze(numbers) = %#v", numbers)
	}
}

func TestNormalizedWord(t *testing.T) {
	for input, want := range map[string]string{
		"«HELLO!»": "hello",
		"42":       "42",
		"...":      "",
		"can't":    "can't",
	} {
		if got := normalizedWord(input); got != want {
			t.Fatalf("normalizedWord(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHasSymbol(t *testing.T) {
	for input, want := range map[string]bool{
		"#tag": true,
		"…":    true,
		"...":  true,
		"word": false,
	} {
		if got := hasSymbol(input); got != want {
			t.Fatalf("hasSymbol(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestClamp01(t *testing.T) {
	for input, want := range map[float64]float64{-1: 0, 0.5: 0.5, 2: 1} {
		if got := clamp01(input); got != want {
			t.Fatalf("clamp01(%v) = %v, want %v", input, got, want)
		}
	}
}

func TestUnsegmentedScript(t *testing.T) {
	for input, want := range map[string]bool{
		"123":   false,
		"latin": false,
		"字abc":  false,
		"字字ab":  true,
		"東京の案内": true,
	} {
		if got := unsegmentedScript(input); got != want {
			t.Fatalf("unsegmentedScript(%q) = %v, want %v", input, got, want)
		}
	}
}
