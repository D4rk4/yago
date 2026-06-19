package yacycrawler_test

import (
	"reflect"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

func TestTokenizeLowercasesAndSplits(t *testing.T) {
	got := yacycrawler.Tokenize("Hello, WORLD! a 42 go-lang")
	want := []string{"hello", "world", "42", "go", "lang"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize = %v, want %v", got, want)
	}
}

func TestTokenizeDropsShortTokens(t *testing.T) {
	got := yacycrawler.Tokenize("a b cd e")
	want := []string{"cd"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize = %v, want %v", got, want)
	}
}

func TestNormalizeLanguage(t *testing.T) {
	cases := map[string]string{
		"en-US": "en",
		"DE":    "de",
		"":      "en",
		"x":     "en",
	}
	for in, want := range cases {
		if got := yacycrawler.NormalizeLanguage(in); got != want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}
