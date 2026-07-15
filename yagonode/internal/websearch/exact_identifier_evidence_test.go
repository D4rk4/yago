package websearch

import "testing"

func TestWebVerificationRequiresMixedAlphanumericIdentifier(t *testing.T) {
	results := []Result{
		{
			Title:   "ZX900Q wall mounted backup power supply",
			URL:     "https://reference.example/power-unit",
			Snippet: "Product specifications",
		},
		{
			Title:   "Maritime history archive",
			URL:     "https://archive.example/maritime-history",
			Snippet: "Wall mounted backup power supply historical notes",
		},
	}

	got := resultsMentioningTerms(
		[]string{"ZX900Q", "wall", "mounted", "backup", "power", "supply"},
		results,
	)
	if len(got) != 1 || got[0].URL != results[0].URL {
		t.Fatalf("results = %#v", got)
	}
}
