package pageparse_test

import (
	"reflect"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageparse"
)

func TestTokenizeLowercasesAndSplits(t *testing.T) {
	got := pageparse.Tokenize("Hello WORLD a 42 golang")
	want := []string{"hello", "world", "42", "golang"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize = %v, want %v", got, want)
	}
}

func TestTokenizeDropsShortTokens(t *testing.T) {
	got := pageparse.Tokenize("a b cd e")
	want := []string{"cd"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize = %v, want %v", got, want)
	}
}

func TestTokenizeKeepsHyphenAndDigitSeparators(t *testing.T) {
	got := pageparse.Tokenize("go-lang 1,234 4.7Ohm")
	want := []string{"go-lang", "1,234", "4.7", "ohm"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize = %v, want %v", got, want)
	}
}

func TestTokenizeMatchesYaCyWordSplitting(t *testing.T) {
	got := pageparse.Tokenize(
		"word-word word . word.word@word.word ....word... word,word word word",
	)
	wordCount := 0
	for _, tok := range got {
		switch tok {
		case "word", "word-word", "word,word":
			wordCount++
		default:
			t.Errorf("unexpected token %q", tok)
		}
	}
	if wordCount != 10 {
		t.Errorf("word token count = %d, want 10", wordCount)
	}
}
