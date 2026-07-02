package pageindex_test

import (
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/pageindex"
)

func TestNormalizeLanguage(t *testing.T) {
	cases := map[string]string{
		"en-US": "en",
		"DE":    "de",
		"":      "en",
		"x":     "en",
	}
	for in, want := range cases {
		if got := pageindex.NormalizeLanguage(in); got != want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}
