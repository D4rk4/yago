package contentprior

import (
	"strings"
	"testing"
)

func TestScoreNeutralWhenUnmeasurable(t *testing.T) {
	// Too short to grade.
	if got := Score("only a handful of words here"); got != neutralScore {
		t.Errorf("short text score = %v, want neutral %v", got, neutralScore)
	}
	// Unsegmented script: word statistics are meaningless.
	cjk := strings.Repeat("字 ", 30)
	if got := Score(cjk); got != neutralScore {
		t.Errorf("CJK score = %v, want neutral %v", got, neutralScore)
	}
}

func TestScoreRewardsCleanProse(t *testing.T) {
	prose := "the cat and the dog are in the house and the sun is bright " +
		"over the green hills of the quiet valley where the river runs to the sea"
	got := Score(prose)
	if got < 0.75 {
		t.Fatalf("clean prose score = %v, want a high grade", got)
	}
}

func TestScoreDemotesKeywordStuffing(t *testing.T) {
	// Twenty-plus content words with no function words: classic keyword spam.
	spam := "linux kernel driver module compile firmware bootloader chipset " +
		"router firewall gateway payload buffer register opcode syscall mmap " +
		"vector matrix tensor gradient descent"
	got := Score(spam)
	if got > 0.2 {
		t.Fatalf("keyword-stuffed score = %v, want a low grade", got)
	}
	if got >= Score("the cat and the dog are in the house and the sun is bright over the hills") {
		t.Fatalf("keyword spam %v must score below clean prose", got)
	}
}

func TestScorePenalizesSymbolsAndNonAlphabetic(t *testing.T) {
	clean := "the cat and the dog are in the house and the sun is bright over the hills of the sea"
	symbols := clean + " #tag #tag #tag #tag #tag #tag #tag #tag"
	if Score(symbols) >= Score(clean) {
		t.Errorf("symbol-heavy text %v must score below clean text %v",
			Score(symbols), Score(clean))
	}
	// Pure numbers: no letters (so no unsegmented letters either) and no function
	// words — the alphabetic and function features both bottom out.
	numbers := strings.Repeat("12345 ", 25)
	if got := Score(numbers); got != 0 {
		t.Errorf("all-numeric score = %v, want 0", got)
	}
}
